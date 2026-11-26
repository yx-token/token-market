/**
 * 闲猫助手主进程：注册 xianmao://，换票后打开下载页并唤起客户端导入协议。
 * 浏览器无法静默安装；本进程可代用户打开安装包链接与深度链接。
 */
const { app, BrowserWindow, shell, ipcMain } = require('electron')
const fs = require('fs')
const http = require('http')
const os = require('os')
const path = require('path')
const { pathToFileURL } = require('url')
const { applyClientConfig, openUrls } = require('./apply-config.cjs')

const PROTOCOL = 'xianmao'
/** 网页用 localhost 探测/投递任务，比自定义协议可靠（浏览器里 xianmao:// 常被静默吞掉）。 */
const LOCAL_PORT = 39217
/** 始终用源码根目录，不要用 process.argv[1]（后面常跟 --user-data-dir，注册协议会坏掉）。 */
const APP_ROOT = path.join(__dirname, '..')
const APP_ICON = (() => {
  const icns = path.join(APP_ROOT, 'build', 'icon.icns')
  const png = path.join(APP_ROOT, 'build', 'icon.png')
  const uiPng = path.join(APP_ROOT, 'ui', 'assets', 'xianmao-logo.png')
  if (process.platform === 'darwin' && fs.existsSync(icns)) return icns
  if (fs.existsSync(png)) return png
  if (fs.existsSync(uiPng)) return uiPng
  return null
})()
let mainWindow = null
let pendingDeepLink = null
let localServer = null
/** 一键任务串行：点完 A 再点 B 时，B 等 A 装完再跑，避免并发抢安装/写配置。 */
let oneClickJobChain = Promise.resolve()
let oneClickJobSeq = 0
/** 每次点「停止」递增；排队中的旧任务看到 epoch 变了就跳过。 */
let oneClickCancelEpoch = 0
/**
 * 进度代际：停止或开新任务时递增。
 * 旧任务收尾时仍可能 send(done:true)，用 token 丢掉，避免「已停止」被「完成」盖住。
 */
let progressToken = 0
/** 单任务上限：避免上一次安装卡住后，后面全部永远排队、界面永远待机。 */
const ONE_CLICK_JOB_TIMEOUT_MS = 12 * 60 * 1000
/** 窗口未就绪时缓冲进度，避免 createWindow 后立刻 send 被 renderer 丢掉。 */
let pendingProgress = []
let lastProgress = null
let uiReady = false

function bindProgress(send) {
  const token = ++progressToken
  return (payload) => {
    if (token !== progressToken) return
    send(payload)
  }
}

function accountStorePath() {
  return path.join(app.getPath('userData'), 'bound-account.json')
}

function credentialsStorePath() {
  return path.join(app.getPath('userData'), 'bound-credentials.json')
}

function preferredSitePath() {
  return path.join(app.getPath('userData'), 'preferred-site.json')
}

/**
 * 正式包固定连生产站，不再让用户填局域网 IP。
 * 本地联调：启动前设 XIANMAO_SITE / XIANMAO_API_BASE（例如 http://127.0.0.1:8080）。
 */
const PRODUCTION_SITE = 'https://idlicat.com'

function envSiteOverride() {
  return String(process.env.XIANMAO_SITE || process.env.XIANMAO_API_BASE || '')
    .trim()
    .replace(/\/$/, '')
}

function canonicalSiteAPI() {
  return envSiteOverride() || PRODUCTION_SITE
}

/** @deprecated 保留调用点兼容；正式包忽略网页推送的局域网地址。 */
function rememberPreferredSite(api) {
  const next = String(api || '').trim().replace(/\/$/, '')
  if (!next || !/^https?:\/\//i.test(next)) return null
  // 有开发覆盖时才记住非生产地址，方便本机联调
  if (envSiteOverride()) {
    try {
      fs.mkdirSync(path.dirname(preferredSitePath()), { recursive: true })
      fs.writeFileSync(
        preferredSitePath(),
        JSON.stringify({ api: next, saved_at: new Date().toISOString() }, null, 2) + '\n',
        'utf8',
      )
    } catch {
      /* ignore */
    }
    return next
  }
  return canonicalSiteAPI()
}

/** 清掉历史缓存的局域网地址，避免继续连 172.16 / 127.0.0.1。 */
function migrateSiteToProduction() {
  if (envSiteOverride()) return
  const prod = PRODUCTION_SITE
  try {
    fs.unlinkSync(preferredSitePath())
  } catch {
    /* ignore */
  }
  try {
    const bound = readBoundAccount()
    if (bound?.api && normalizeSiteHost(bound.api) !== normalizeSiteHost(prod)) {
      writeBoundAccount({ ...bound, api: prod })
    }
  } catch {
    /* ignore */
  }
  try {
    const creds = readBoundCredentials()
    if (!creds?.key) return
    const base = String(creds.base_url || creds.api || '')
    if (base && normalizeSiteHost(base) !== normalizeSiteHost(prod)) {
      writeBoundCredentials({ ...creds, base_url: prod }, prod)
    }
  } catch {
    /* ignore */
  }
}

function normalizeSiteHost(raw) {
  try {
    return String(new URL(String(raw || '').trim()).hostname || '').toLowerCase().replace(/^www\./, '')
  } catch {
    return ''
  }
}

function readPreferredSite() {
  try {
    const raw = fs.readFileSync(preferredSitePath(), 'utf8')
    const data = JSON.parse(raw)
    const api = String(data?.api || '').trim().replace(/\/$/, '')
    return api || null
  } catch {
    return null
  }
}

function readBoundAccount() {
  try {
    const raw = fs.readFileSync(accountStorePath(), 'utf8')
    const data = JSON.parse(raw)
    if (!data || typeof data !== 'object') return null
    if (!data.account && !data.user_id) return null
    return data
  } catch {
    return null
  }
}

function readBoundCredentials(opts = {}) {
  try {
    const raw = fs.readFileSync(credentialsStorePath(), 'utf8')
    const data = JSON.parse(raw)
    if (!data || typeof data !== 'object') return null
    if (!data.key || !data.base_url) return null
    return data
  } catch {
    /* 缺文件时默认不迁移：网站会话同步也会读这里，自动迁回会导致「退出又已登录」 */
  }
  if (opts.allowMigrate) return migrateCredentialsFromLocalCodex()
  return null
}

/** 旧版助手只记了账号、没存密钥；若 Codex 已指向闲猫则回收一份供本助手复用。 */
function migrateCredentialsFromLocalCodex() {
  try {
    // 必须已有 bound-account；否则退出登录后会从 ~/.codex 把密钥又写回来，UI 仍显示已登录
    const bound = readBoundAccount()
    if (!bound) return null
    const api = String(bound.api || '').replace(/\/$/, '')
    if (!api) return null
    const authPath = path.join(os.homedir(), '.codex', 'auth.json')
    const cfgPath = path.join(os.homedir(), '.codex', 'config.toml')
    if (!fs.existsSync(authPath) || !fs.existsSync(cfgPath)) return null
    const auth = JSON.parse(fs.readFileSync(authPath, 'utf8'))
    const key = String(auth.OPENAI_API_KEY || '').trim()
    if (!key) return null
    const cfg = fs.readFileSync(cfgPath, 'utf8')
    const m = cfg.match(/base_url\s*=\s*"([^"]+)"/)
    const base = String(m?.[1] || '').replace(/\/$/, '')
    if (!base) return null
    // 只接受指向本机关联市场 / 闲猫本地网关的配置，防止误用其他上游密钥
    const apiHost = api.replace(/^https?:\/\//, '').split('/')[0]
    const baseHost = base.replace(/^https?:\/\//, '').split('/')[0]
    const looksLocal = /127\.0\.0\.1|localhost/i.test(baseHost)
    if (apiHost && baseHost && apiHost !== baseHost && !(looksLocal && /127\.0\.0\.1|localhost/i.test(apiHost))) {
      return null
    }
    return writeBoundCredentials(
      {
        key,
        key_id: 0,
        key_name: bound.key_name || '闲猫',
        base_url: base.replace(/\/v1$/i, ''),
        provider: '闲猫',
      },
      api || `http://${baseHost}`,
    )
  } catch {
    return null
  }
}

function isLoopbackHTTPURL(raw) {
  try {
    const u = new URL(String(raw || '').trim())
    const host = String(u.hostname || '').toLowerCase()
    return host === '127.0.0.1' || host === 'localhost' || host === '::1'
  } catch {
    return false
  }
}

/** 客户端要连的地址：优先实际可达的站点，不要把 127.0.0.1 写进异机配置。 */
function pickClientBaseURL(payloadBase, siteApi) {
  const fromPayload = String(payloadBase || '').replace(/\/$/, '')
  const fromSite = String(siteApi || '').replace(/\/$/, '')
  if (fromSite && (!fromPayload || isLoopbackHTTPURL(fromPayload)) && !isLoopbackHTTPURL(fromSite)) {
    return fromSite
  }
  if (fromPayload && !isLoopbackHTTPURL(fromPayload)) return fromPayload
  if (fromSite) return fromSite
  return fromPayload
}

function writeBoundCredentials(payload, api) {
  // 只存闲猫换票结果；禁止自定义上游
  const siteApi = String(api || '').replace(/\/$/, '')
  let prev = null
  try {
    prev = JSON.parse(fs.readFileSync(credentialsStorePath(), 'utf8'))
  } catch {
    /* ignore */
  }
  const session = String(payload.helper_session || payload.helperSession || prev?.helper_session || '').trim()
  const next = {
    key: String(payload.key || prev?.key || ''),
    key_id: Number(payload.key_id || prev?.key_id) || 0,
    key_name: String(payload.key_name || prev?.key_name || ''),
    platform: String(payload.platform || prev?.platform || '').trim().toLowerCase(),
    base_url: pickClientBaseURL(payload.base_url || prev?.base_url, siteApi),
    provider: '闲猫',
    helper_session: session,
    api: siteApi,
    saved_at: new Date().toISOString(),
  }
  // 有长会话或 API Key 之一即可；base_url 必须有
  if ((!next.key && !next.helper_session) || !next.base_url) return null
  fs.mkdirSync(path.dirname(credentialsStorePath()), { recursive: true })
  fs.writeFileSync(credentialsStorePath(), JSON.stringify(next, null, 2) + '\n', { encoding: 'utf8', mode: 0o600 })
  try {
    fs.chmodSync(credentialsStorePath(), 0o600)
  } catch {
    /* ignore */
  }
  return next
}

/** 助手调市场接口：优先用与 API Key 解耦的长会话 */
function helperAuthBearer(creds) {
  const session = String(creds?.helper_session || '').trim()
  if (session) return session
  return String(creds?.key || '').trim()
}

function hasHelperAuth(creds) {
  return !!(creds && helperAuthBearer(creds) && creds.base_url)
}

function writeBoundAccount(info) {
  const next = {
    account: String(info.account || '').trim(),
    user_id: Number(info.user_id) || 0,
    api: String(info.api || '').replace(/\/$/, ''),
    key_name: String(info.key_name || ''),
    has_credentials: !!readBoundCredentials(),
    bound_at: new Date().toISOString(),
  }
  if (!next.account && !next.user_id) return null
  fs.mkdirSync(path.dirname(accountStorePath()), { recursive: true })
  fs.writeFileSync(accountStorePath(), JSON.stringify(next, null, 2) + '\n', 'utf8')
  return { ...next, has_credentials: !!readBoundCredentials() }
}

function clearBoundAccount() {
  try {
    fs.unlinkSync(accountStorePath())
  } catch {
    /* ignore */
  }
  try {
    fs.unlinkSync(credentialsStorePath())
  } catch {
    /* ignore */
  }
}

/** 网站登录态推到本机：只同步账号身份；换用户时清掉旧密钥。 */
function syncBoundSession(info) {
  const userId = Number(info?.user_id) || 0
  const account = String(info?.account || '').trim()
  const api = String(info?.api || '').replace(/\/$/, '')
  if (!userId && !account) return null
  const prev = readBoundAccount()
  const prevId = Number(prev?.user_id) || 0
  if (prevId && userId && prevId !== userId) {
    try {
      fs.unlinkSync(credentialsStorePath())
    } catch {
      /* ignore */
    }
  }
  const site = canonicalSiteAPI()
  if (api) rememberPreferredSite(api)
  return writeBoundAccount({
    account,
    user_id: userId,
    api: site,
    key_name: prev?.key_name || '',
  })
}

function notifySessionUI(account) {
  sendToUI({
    step: 'session',
    status: account ? 'done' : 'idle',
    done: false,
    message: account ? `已与网站同步 · ${account.account || account.user_id || ''}` : '已与网站同步：未登录',
    account: account || null,
    sessionSync: true,
  })
}

function withTimeout(promise, ms, label) {
  let timer
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`${label || '任务'}超时，请回网页再点一次「一键使用」`)), ms)
  })
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer))
}

function enqueueOneClick(parsed, send) {
  const seq = ++oneClickJobSeq
  const epoch = oneClickCancelEpoch
  const jobSend = bindProgress(send)
  const { resetCancel, isCancelledError } = require('./auto-install.cjs')
  // 接单立刻刷 UI（不等队列），否则上一个任务卡住时用户只看见待机页
  jobSend({
    step: 'download',
    status: 'active',
    message: '已接到任务，准备处理…',
    cancellable: true,
    client: parsed.client || '',
  })
  const run = async () => {
    if (epoch !== oneClickCancelEpoch) {
      return
    }
    resetCancel()
    jobSend({
      step: 'download',
      status: 'active',
      message: oneClickJobSeq > seq ? `排队中（前面还有任务）…` : '开始处理…',
      cancellable: true,
      client: parsed.client || '',
    })
    try {
      await withTimeout(runOneClick(parsed, jobSend), ONE_CLICK_JOB_TIMEOUT_MS, '一键使用')
    } catch (err) {
      if (isCancelledError(err) || epoch !== oneClickCancelEpoch) {
        jobSend({ step: 'error', status: 'error', message: '已停止', cancelled: true })
        return
      }
      jobSend({
        step: 'error',
        status: 'error',
        message: err instanceof Error ? err.message : '一键使用失败',
      })
    }
  }
  // 单链路串行；若上游 Promise 永久挂起，用超时兜底，避免永远排不上
  const prev = oneClickJobChain
  oneClickJobChain = Promise.race([
    prev.catch(() => {}),
    new Promise((resolve) => setTimeout(resolve, 15_000)),
  ]).then(run, run)
  return oneClickJobChain
}

function cancelOneClick(send) {
  oneClickCancelEpoch += 1
  progressToken += 1 // 作废进行中任务的后续进度（含可能迟到的 done:true）
  try {
    require('./auto-install.cjs').requestCancel()
  } catch {
    /* ignore */
  }
  // 断开卡住的 Promise 链，否则后面任务永远停在「已接到任务，准备处理」
  oneClickJobChain = Promise.resolve()
  pendingProgress = []
  send({
    step: 'error',
    status: 'error',
    message: '已停止。可点「一键使用」重新开始。',
    cancelled: true,
  })
}

// 开发态直接跑 Electron 二进制时，菜单/程序坞默认叫 Electron；改成闲猫助手避免和空白壳混淆
try {
  app.setName('闲猫助手')
  // 各平台用官方约定目录，避免 Windows 误写到 macOS 的 Library 路径
  const userData =
    process.platform === 'darwin'
      ? path.join(os.homedir(), 'Library', 'Application Support', '闲猫助手')
      : process.platform === 'win32'
        ? path.join(process.env.APPDATA || path.join(os.homedir(), 'AppData', 'Roaming'), '闲猫助手')
        : path.join(os.homedir(), '.config', '闲猫助手')
  app.setPath('userData', userData)
} catch {
  /* ignore */
}

/**
 * 注册 xianmao://。
 * macOS 上若对「裸 Electron」调用 setAsDefaultProtocolClient(execPath, [APP_ROOT])，
 * Launch Services 只会记下 com.github.electron、丢掉 APP_ROOT 参数 → 一点协议就弹出 Electron 欢迎页。
 * 因此开发态优先把 scheme 绑到 dist/闲猫助手.app（com.xianmao.helper.dev）。
 */
/** Windows：把 xianmao:// 的显示名写成「闲猫」，避免浏览器弹出「要打开 Electron 吗？」 */
function registerWindowsProtocolDisplayName() {
  if (process.platform !== 'win32') return
  try {
    const { spawnSync } = require('child_process')
    const exe = process.execPath
    const key = `HKCU\\Software\\Classes\\${PROTOCOL}`
    const run = (args) => spawnSync('reg', args, { encoding: 'utf8', windowsHide: true })
    run(['add', key, '/ve', '/d', 'URL:闲猫', '/f'])
    run(['add', key, '/v', 'URL Protocol', '/d', '', '/f'])
    run(['add', `${key}\\DefaultIcon`, '/ve', '/d', `${exe},0`, '/f'])
    run(['add', `${key}\\shell\\open\\command`, '/ve', '/d', `"${exe}" "%1"`, '/f'])
    run(['add', `${key}\\Application`, '/v', 'ApplicationName', '/d', '闲猫', '/f'])
    run(['add', `${key}\\Application`, '/v', 'ApplicationDescription', '/d', '闲猫', '/f'])
    run(['add', `${key}\\Application`, '/v', 'ApplicationIcon', '/d', `${exe},0`, '/f'])
  } catch (err) {
    console.error('[xianmao-helper] win protocol display name failed', err)
  }
}

function registerProtocolClient() {
  try {
    if (process.platform === 'darwin') {
      const helperApp = path.join(APP_ROOT, 'dist', '闲猫助手.app')
      const setter = path.join(APP_ROOT, 'scripts', 'set-url-handler')
      const lsregister =
        '/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister'
      if (fs.existsSync(helperApp)) {
        try {
          const { spawnSync } = require('child_process')
          if (fs.existsSync(lsregister)) spawnSync(lsregister, ['-f', helperApp], { encoding: 'utf8' })
          if (fs.existsSync(setter)) {
            spawnSync(setter, [PROTOCOL, 'com.xianmao.helper.dev'], { encoding: 'utf8' })
            return
          }
        } catch (err) {
          console.error('[xianmao-helper] mac protocol bind via helper.app failed', err)
        }
      }
    }
    if (process.defaultApp) {
      // 非 mac / 无包装 App 时的兜底；mac 上仍可能被系统丢掉 args
      app.setAsDefaultProtocolClient(PROTOCOL, process.execPath, [APP_ROOT])
    } else {
      app.setAsDefaultProtocolClient(PROTOCOL)
    }
    // 覆盖 Electron 默认登记名，让 Edge/Chrome 显示「打开闲猫」
    registerWindowsProtocolDisplayName()
  } catch (err) {
    console.error('[xianmao-helper] protocol register failed', err)
  }
}

function flushPendingProgress() {
  if (!mainWindow || mainWindow.isDestroyed() || !uiReady) return
  const batch = pendingProgress.splice(0, pendingProgress.length)
  for (const payload of batch) {
    mainWindow.webContents.send('one-click:progress', payload)
  }
}

function markUiReady() {
  uiReady = true
  flushPendingProgress()
}

function createWindow() {
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.focus()
    // 已有窗口时补推最近一次进度（用户关过再开、或 reload 后）
    if (uiReady && lastProgress) {
      mainWindow.webContents.send('one-click:progress', lastProgress)
    }
    return mainWindow
  }
  uiReady = false
  mainWindow = new BrowserWindow({
    // 12 个 Agent：4×3 平铺一屏可见，避免底部被裁切
    width: 1180,
    height: 820,
    minWidth: 1080,
    minHeight: 760,
    resizable: true,
    title: '闲猫助手',
    ...(APP_ICON ? { icon: APP_ICON } : {}),
    webPreferences: {
      preload: path.join(__dirname, 'preload.cjs'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  })
  const reveal = () => {
    if (!mainWindow || mainWindow.isDestroyed()) return
    mainWindow.center()
    mainWindow.show()
    mainWindow.focus()
    // 短暂置顶，避免被空白 Electron 壳挡住、用户只看见欢迎页
    mainWindow.setAlwaysOnTop(true)
    setTimeout(() => {
      if (mainWindow && !mainWindow.isDestroyed()) mainWindow.setAlwaysOnTop(false)
    }, 2500)
  }
  // 先挂 ready-to-show 再 loadFile，避免本地页加载太快导致窗口一直不显示
  mainWindow.once('ready-to-show', reveal)
  mainWindow.webContents.on('did-finish-load', () => {
    // 页面脚本已跑完；再等一拍让 preload 的 onProgress 挂上
    setTimeout(markUiReady, 50)
  })
  void mainWindow.loadFile(path.join(__dirname, '..', 'ui', 'index.html')).then(reveal)
  mainWindow.on('closed', () => {
    mainWindow = null
    uiReady = false
  })
  return mainWindow
}

function parseOneClickURL(raw) {
  try {
    const u = new URL(raw)
    if (u.protocol !== `${PROTOCOL}:`) return null
    if (!u.hostname && !u.pathname.includes('one-click')) {
      // xianmao://one-click?... → hostname=one-click
    }
    const action = u.hostname || u.pathname.replace(/^\//, '')
    if (action !== 'one-click') return null
    const ticket = u.searchParams.get('ticket') || ''
    const client = u.searchParams.get('client') || ''
    const os = u.searchParams.get('os') || ''
    const api = u.searchParams.get('api') || ''
    if (!ticket || !api) return null
    return { ticket, client, os, api: api.replace(/\/$/, '') }
  } catch {
    return null
  }
}

async function exchangeTicket({ api, ticket }) {
  const res = await fetch(`${api}/api/one-click/exchange`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ ticket }),
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    const msg = body?.error?.message || body?.message || `HTTP ${res.status}`
    throw new Error(msg)
  }
  // 市场接口统一 { data: ... }
  return body.data || body
}

let helperAuthAbort = null

function resolveSiteAPI() {
  return canonicalSiteAPI()
}

function detectHelperOS() {
  if (process.platform === 'win32') return 'windows'
  if (process.platform === 'linux') return 'linux'
  return 'macos'
}

function newDeviceCode() {
  const { randomBytes } = require('crypto')
  return randomBytes(16).toString('hex')
}

async function startBrowserLogin(send) {
  if (helperAuthAbort) {
    helperAuthAbort.cancelled = true
    helperAuthAbort = null
  }
  const api = resolveSiteAPI()
  const code = newDeviceCode()
  const osName = detectHelperOS()
  const ctrl = { cancelled: false }
  helperAuthAbort = ctrl

  send({
    step: 'session',
    status: 'active',
    done: false,
    sessionSync: true,
    loginPending: true,
    message: `正在打开浏览器（${api}），请在网页完成登录授权…`,
    account: null,
  })

  let startRes
  let startBody = {}
  try {
    startRes = await fetch(`${api}/api/one-click/helper-auth/start`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ code, api_base: api, os: osName }),
    })
    startBody = await startRes.json().catch(() => ({}))
  } catch (err) {
    const detail = err instanceof Error ? err.message : String(err)
    throw new Error(`连不上闲猫站点（${api}）：${detail}`)
  }
  if (!startRes.ok) {
    const msg = startBody?.error?.message || startBody?.message || `HTTP ${startRes.status}`
    throw new Error(`无法发起登录：${msg}`)
  }

  const authURL = `${api}/helper-auth?code=${encodeURIComponent(code)}`
  await shell.openExternal(authURL)

  // 授权等待有上限：超时后恢复「浏览器登录」，避免右上角一直卡在等待态
  const AUTH_WAIT_MS = 2 * 60 * 1000
  const deadline = Date.now() + AUTH_WAIT_MS
  while (Date.now() < deadline) {
    if (ctrl.cancelled || (helperAuthAbort && helperAuthAbort !== ctrl)) {
      throw new Error('已取消登录')
    }
    await new Promise((r) => setTimeout(r, 1500))
    if (ctrl.cancelled) throw new Error('已取消登录')

    let pollBody = null
    try {
      const pollRes = await fetch(`${api}/api/one-click/helper-auth/${encodeURIComponent(code)}`, {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      })
      pollBody = await pollRes.json().catch(() => ({}))
      if (pollRes.status === 404) {
        throw new Error('登录已过期，请重新点「浏览器登录」')
      }
      if (!pollRes.ok) continue
    } catch (err) {
      if (err instanceof Error && /过期|取消/.test(err.message)) throw err
      continue
    }
    const data = pollBody?.data || pollBody
    if (!data || data.status !== 'authorized' || !data.ticket) {
      const leftSec = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
      send({
        step: 'session',
        status: 'active',
        done: false,
        sessionSync: true,
        loginPending: true,
        expiresIn: leftSec,
        message: `等待浏览器授权… ${leftSec}s`,
        account: null,
      })
      continue
    }

    const payload = await exchangeTicket({ api, ticket: data.ticket })
    writeBoundCredentials(payload, api)
    const bound = writeBoundAccount({
      account: payload.account || data.account,
      user_id: payload.user_id || data.user_id,
      api,
      key_name: payload.key_name,
    })
    const account = {
      ...bound,
      has_credentials: true,
      logged_in: true,
    }
    helperAuthAbort = null
    send({
      step: 'session',
      status: 'done',
      done: false,
      sessionSync: true,
      loginPending: false,
      message: `已登录 · ${account.account || account.user_id || ''}`,
      account,
    })
    return { ok: true, account }
  }
  helperAuthAbort = null
  throw new Error('登录超时（2 分钟），请重新点「浏览器登录」')
}

function cancelBrowserLogin() {
  if (helperAuthAbort) helperAuthAbort.cancelled = true
  helperAuthAbort = null
}

async function runOneClick(parsed, send) {
  // 一键任务必须以网站已登录账号签发的 ticket 换票；无有效账号不能装配置
  send({ step: 'exchange', status: 'active', message: '正在确认账号…' })
  let payload
  try {
    payload = await exchangeTicket(parsed)
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    const needLogin =
      /ticket|expired|already used|invalid|unauthorized|未授权|登录/i.test(msg) ||
      /TICKET/i.test(msg)
    throw new Error(
      needLogin
        ? '登录已失效，请先在闲猫网站登录，再点「一键使用」。'
        : '连接闲猫失败，请稍后再试。',
    )
  }
  const site = canonicalSiteAPI()
  writeBoundCredentials(payload, site)
  const bound = writeBoundAccount({
    account: payload.account,
    user_id: payload.user_id,
    api: site,
    key_name: payload.key_name,
  })
  send({
    step: 'exchange',
    status: 'done',
    message: bound?.account ? `已连接闲猫（${bound.account}）` : '已连接闲猫',
    account: bound,
  })

  const client = parsed.client || payload.client
  await applyAndOpenAgent(
    { ...payload, provider: '闲猫', client },
    client,
    send,
    bound,
  )
}

/** 安装 + 只写入闲猫配置 + 打开；网页一键与助手内「一键使用」共用。 */
async function applyAndOpenAgent(payload, client, send, bound) {
  const { throwIfCancelled, isCancelledError } = require('./auto-install.cjs')
  const guard = () => throwIfCancelled()
  // 安装前再拦一道：磁盘 / 系统不达标直接失败（网页一键也会走到这里）
  try {
    guard()
    const { runPreflight } = require('./preflight.cjs')
    const pf = runPreflight(client)
    send({
      step: 'download',
      status: 'active',
      message: '正在检测本机资源…',
      client,
      preflight: pf,
    })
    if (!pf.ok) {
      const bad = (pf.checks || [])
        .filter((c) => c.required && !c.ok)
        .map((c) => c.detail)
        .join('；')
      throw new Error(bad || '本机资源不满足安装要求')
    }
  } catch (err) {
    if (isCancelledError(err)) throw err
    if (/本机资源|磁盘|操作系统|不满足/.test(String(err?.message || err))) throw err
    // 检测模块异常不阻断（避免误伤），继续安装
  }
  guard()
  send({ step: 'download', status: 'active', message: '正在安装软件…', client })
  send({ step: 'import', status: 'active', message: '正在连上闲猫…', client })
  const result = await applyClientConfig({ ...payload, provider: '闲猫', client }, client, {
    onProgress: (p) => {
      if (!p?.message) return
      send({
        step: 'download',
        status: 'active',
        message: p.percent != null ? `${p.message}` : p.message,
        client,
      })
    },
  })
  guard()
  send({ step: 'download', status: 'done', message: result.detail, client })
  let openedKind = ''
  try {
    const { openInstalledApp, openCLIInTerminal } = require('./auto-install.cjs')
    if (result.openApp) {
      if (openInstalledApp(result.openApp)) {
        openedKind = 'app'
      } else if (result.openCli) {
        if (openCLIInTerminal(result.openCli, result.openCliArgs || [])) {
          openedKind = 'cli'
          result.openedCli = true
        }
      }
    } else if (result.openWeb) {
      await shell.openExternal(result.openWeb)
      openedKind = 'app'
    } else {
      const agentUrls = (result.urls || []).filter((u) => {
        if (!u || typeof u !== 'string') return false
        if (/^https?:\/\//i.test(u)) return false
        if (u.startsWith('file://')) return false
        return true
      })
      if (agentUrls.length) {
        await openUrls(agentUrls)
        openedKind = 'deeplink'
      } else if (result.openCli && !result.openedCli) {
        openCLIInTerminal(result.openCli)
        openedKind = 'cli'
      }
    }
  } catch {
    /* ignore */
  }
  guard()
  const helpUrl =
    result.helpUrl ||
    (result.helpUrls && result.helpUrls[0]) ||
    (result.urls || []).find((u) => typeof u === 'string' && /^https?:\/\//i.test(u)) ||
    null
  send({ step: 'import', status: 'done', message: result.detail, client })
  const { getAgent, cliGuideFor } = require('./agents.cjs')
  const agentMeta = getAgent(client)
  const isCliOnly = agentMeta?.open === 'cli' || openedKind === 'cli'
  // 完成页只展示一句人话，不重复路径/密钥名
  const doneMessage =
    openedKind === 'cli'
      ? result.detail || '已配好，请看打开的黑窗口'
      : openedKind === 'app' || openedKind === 'deeplink'
        ? result.detail || '已配好闲猫，客户端已打开'
        : helpUrl
          ? result.detail || '还差一点，请点「打开安装说明」'
          : result.detail || '已配好闲猫'
  send({
    step: 'open',
    status: 'done',
    message: doneMessage,
    done: true,
    keyName: payload.key_name,
    client,
    account: bound || readBoundAccount(),
    openCli: result.openCli || null,
    openApp: result.openApp || null,
    helpUrl,
    openedKind,
    openMode: agentMeta?.open || '',
    cliGuide: isCliOnly ? cliGuideFor(client) : null,
  })
}

function isInvalidAPIKeyMessage(msg) {
  const s = String(msg || '').toLowerCase()
  return /invalid api key|api key invalid|key_invalid|unauthorized/.test(s) || /\b401\b/.test(s)
}

async function fetchModelIds(baseURL, key) {
  const root = String(baseURL || '').replace(/\/$/, '')
  const endpoint = /\/v1$/i.test(root) ? `${root}/models` : `${root}/v1/models`
  const res = await fetch(endpoint, {
    headers: { Accept: 'application/json', Authorization: `Bearer ${key}` },
    cache: 'no-store',
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    const msg = body?.error?.message || body?.message || `HTTP ${res.status}`
    if (isInvalidAPIKeyMessage(msg) || res.status === 401) {
      throw new Error(
        '助手本地密钥已失效（和网页上看到的不是同一把）。请先点右上角退出，再「浏览器登录」；或回网站对 Codex 点一次「一键使用」把新密钥同步过来。',
      )
    }
    throw new Error(`拉取模型列表失败：${msg}`)
  }
  const data = body?.data || body?.models || []
  return Array.isArray(data)
    ? data.map((m) => String(m?.id || m?.name || '').trim()).filter(Boolean)
    : []
}

async function finalizeVendorKey(raw, client, want, api) {
  const { platformLabel, pickModelForPlatform, defaultModelForPlatform } = require('./vendors.cjs')
  const platform = String(raw.platform || want).trim().toLowerCase() || want
  if (platform !== want) {
    throw new Error(
      `需要 ${platformLabel(want)} 密钥，但服务端返回的是 ${platformLabel(platform)}。请换厂商后点「重新配置」。`,
    )
  }
  const baseURL = pickClientBaseURL(raw.base_url, api)
  let modelIds = []
  try {
    modelIds = await fetchModelIds(baseURL, raw.key)
  } catch (err) {
    throw new Error(err instanceof Error ? err.message : '无法验证密钥可用模型')
  }
  const model = pickModelForPlatform(modelIds, want) || defaultModelForPlatform(want)
  if (modelIds.length && !pickModelForPlatform(modelIds, want)) {
    const sample = modelIds.slice(0, 4).join('、') || '（空）'
    throw new Error(
      `${platformLabel(want)} 保障池没有匹配模型（当前是：${sample}）。Codex 必须用 ChatGPT/OpenAI 池，请到管理端检查该池上游，或换一把 ChatGPT 密钥后再点「重新配置」。`,
    )
  }
  const next = {
    key: raw.key,
    key_id: raw.key_id,
    key_name: raw.key_name,
    platform: want,
    base_url: baseURL,
    provider: raw.provider || '闲猫',
    helper_session: raw.helper_session,
    model,
    client,
  }
  writeBoundCredentials(next, api)
  return next
}

async function resolveVendorKey(creds, client, platform) {
  const { preferredVendor, platformLabel } = require('./vendors.cjs')
  const want = String(platform || preferredVendor(client) || '').trim().toLowerCase()
  const api = resolveSiteAPI()
  if (!want) {
    throw new Error('未指定厂商')
  }
  const bearer = helperAuthBearer(creds)
  if (!bearer) {
    throw new Error('还没有登录会话，请先点右上角「浏览器登录」')
  }
  // 每次「重新配置 / 一键使用」都向服务端按厂商取最新密钥（会话鉴权，不依赖网页旧 tm- key）
  try {
    const res = await fetch(`${api}/api/one-click/helper/resolve-key`, {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        Authorization: `Bearer ${bearer}`,
      },
      body: JSON.stringify({ client, platform: want }),
      cache: 'no-store',
    })
    const body = await res.json().catch(() => ({}))
    if (!res.ok) {
      const msg = body?.error?.message || body?.message || body?.error?.code || `HTTP ${res.status}`
      if (isInvalidAPIKeyMessage(msg) || res.status === 401) {
        const err = new Error(
          '登录会话或本地密钥已失效，正在打开浏览器重新授权以自动获取最新厂商密钥…',
        )
        err.code = 'HELPER_AUTH_EXPIRED'
        throw err
      }
      throw new Error(msg)
    }
    const data = body?.data || body
    return await finalizeVendorKey(
      {
        key: data.key,
        key_id: data.key_id,
        key_name: data.key_name,
        platform: data.platform || want,
        base_url: data.base_url || creds.base_url,
        provider: data.provider || '闲猫',
        helper_session: data.helper_session || creds.helper_session,
      },
      client,
      want,
      api,
    )
  } catch (err) {
    const tip = err instanceof Error ? err.message : String(err)
    if (err?.code === 'HELPER_AUTH_EXPIRED' || isInvalidAPIKeyMessage(tip) || /本地密钥已失效|登录会话/.test(tip)) {
      throw err instanceof Error ? err : new Error(tip)
    }
    // 仅当本地已是「同厂商」密钥时，才允许离线复用；跨厂商绝不回退
    if (creds?.key && String(creds.platform || '').toLowerCase() === want) {
      try {
        return await finalizeVendorKey(
          {
            key: creds.key,
            key_id: creds.key_id,
            key_name: creds.key_name,
            platform: want,
            base_url: creds.base_url,
            provider: '闲猫',
            helper_session: creds.helper_session,
          },
          client,
          want,
          api,
        )
      } catch (verifyErr) {
        throw verifyErr
      }
    }
    throw new Error(`无法准备 ${platformLabel(want)} 密钥：${tip}`)
  }
}

function enqueueLocalSetup(client, send, opts = {}) {
  const { isKnownAgent } = require('./agents.cjs')
  if (!isKnownAgent(client)) {
    send({ step: 'error', status: 'error', message: `不支持的客户端：${client}` })
    return Promise.resolve()
  }
  const bound = readBoundAccount()
  const creds = readBoundCredentials({ allowMigrate: true })
  if (!bound) {
    send({
      step: 'error',
      status: 'error',
      message: '请先点右上角「浏览器登录」，或在网站登录后点「一键使用」',
    })
    return Promise.resolve()
  }
  if (!hasHelperAuth(creds)) {
    send({
      step: 'error',
      status: 'error',
      message: '还没有连上闲猫。请先「浏览器登录」，或回网站再点一次「一键使用」',
    })
    return Promise.resolve()
  }
  const platform = String(opts.platform || '').trim()
  const seq = ++oneClickJobSeq
  const epoch = oneClickCancelEpoch
  const jobSend = bindProgress(send)
  const { resetCancel, isCancelledError } = require('./auto-install.cjs')
  jobSend({
    step: 'download',
    status: 'active',
    message: '正在准备配置…',
    cancellable: true,
    client,
  })
  const run = async () => {
    if (epoch !== oneClickCancelEpoch) {
      return
    }
    resetCancel()
    jobSend({
      step: 'exchange',
      status: 'active',
      message: '正在按厂商准备密钥…',
      account: bound,
      client,
    })
    let payload
    let workingCreds = creds
    try {
      payload = await resolveVendorKey(workingCreds, client, platform)
    } catch (err) {
      const needRelogin =
        err?.code === 'HELPER_AUTH_EXPIRED' ||
        isInvalidAPIKeyMessage(err?.message) ||
        /登录会话|本地密钥已失效|无法向服务器换/.test(String(err?.message || ''))
      if (needRelogin) {
        try {
          jobSend({
            step: 'exchange',
            status: 'active',
            message: '登录已失效，正在打开浏览器重新授权以获取最新厂商密钥…',
            account: bound,
            client,
            loginPending: true,
          })
          await startBrowserLogin(jobSend)
          workingCreds = readBoundCredentials({ allowMigrate: true })
          if (!hasHelperAuth(workingCreds)) {
            throw new Error('重新授权后仍未拿到会话，请再试一次「浏览器登录」')
          }
          jobSend({
            step: 'exchange',
            status: 'active',
            message: '授权成功，正在按厂商拉取最新密钥…',
            account: readBoundAccount(),
            client,
          })
          payload = await resolveVendorKey(workingCreds, client, platform)
        } catch (reErr) {
          jobSend({
            step: 'error',
            status: 'error',
            message: reErr instanceof Error ? reErr.message : '重新授权失败',
            client,
          })
          return
        }
      } else {
        jobSend({
          step: 'error',
          status: 'error',
          message: err instanceof Error ? err.message : '准备密钥失败',
          client,
        })
        return
      }
    }
    jobSend({
      step: 'exchange',
      status: 'done',
      message: '已连接闲猫',
      account: bound,
      client,
    })
    try {
      await withTimeout(
        applyAndOpenAgent(
          {
            key: payload.key,
            key_id: payload.key_id,
            key_name: payload.key_name,
            platform: payload.platform,
            model: payload.model,
            // 按实际站点写客户端配置，避免局域网场景把 127.0.0.1 写进 Codex/Claude
            base_url: payload.base_url,
            provider: '闲猫',
            client,
          },
          client,
          jobSend,
          bound,
        ),
        ONE_CLICK_JOB_TIMEOUT_MS,
        '一键使用',
      )
    } catch (err) {
      if (isCancelledError(err) || epoch !== oneClickCancelEpoch) {
        jobSend({ step: 'error', status: 'error', message: '已停止', cancelled: true })
        return
      }
      jobSend({
        step: 'error',
        status: 'error',
        message: err instanceof Error ? err.message : '配置失败',
        client,
      })
    }
  }
  void seq
  const prev = oneClickJobChain
  oneClickJobChain = Promise.race([
    prev.catch(() => {}),
    new Promise((resolve) => setTimeout(resolve, 15_000)),
  ]).then(run, run)
  return oneClickJobChain
}

function sendToUI(payload) {
  if (!payload) return
  lastProgress = payload
  if (payload.done || payload.step === 'error' || payload.status === 'error') {
    // 终态之后再来新任务会覆盖；这里先清「进行中」感
  }
  if (!mainWindow || mainWindow.isDestroyed() || !uiReady) {
    pendingProgress.push(payload)
    // 最多保留最近若干条，避免堆积
    if (pendingProgress.length > 40) pendingProgress.splice(0, pendingProgress.length - 40)
    return
  }
  mainWindow.webContents.send('one-click:progress', payload)
}

function isOpenHelperURL(raw) {
  try {
    const u = new URL(raw)
    if (u.protocol !== `${PROTOCOL}:`) return false
    const action = u.hostname || u.pathname.replace(/^\//, '')
    return action === 'open' || action === ''
  } catch {
    return false
  }
}

async function handleDeepLink(raw) {
  createWindow()
  // 网页只唤起窗口：xianmao://open
  if (isOpenHelperURL(raw) && !parseOneClickURL(raw)) return
  const parsed = parseOneClickURL(raw)
  if (!parsed) {
    // 未知深链不弹吓人错误，只打开窗口
    return
  }
  try {
    await enqueueOneClick(parsed, sendToUI)
  } catch (err) {
    sendToUI({
      step: 'error',
      status: 'error',
      message: err instanceof Error ? err.message : '一键使用失败',
    })
  }
}

function takeArgvDeepLink(argv) {
  return argv.find((a) => typeof a === 'string' && a.startsWith(`${PROTOCOL}://`))
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = []
    req.on('data', (c) => chunks.push(c))
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')))
    req.on('error', reject)
  })
}

function startLocalBridge() {
  if (localServer) return
  localServer = http.createServer(async (req, res) => {
    res.setHeader('Access-Control-Allow-Origin', '*')
    res.setHeader('Access-Control-Allow-Methods', 'GET,POST,OPTIONS')
    res.setHeader('Access-Control-Allow-Headers', 'Content-Type, Access-Control-Request-Private-Network')
    // Chrome Private Network Access：公网页访问 127.0.0.1 时预检需要此头，否则网页以为助手不在线
    res.setHeader('Access-Control-Allow-Private-Network', 'true')
    if (req.method === 'OPTIONS') {
      res.writeHead(204)
      res.end()
      return
    }
    const url = new URL(req.url || '/', `http://127.0.0.1:${LOCAL_PORT}`)
    if (req.method === 'GET' && url.pathname === '/health') {
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify({ ok: true, name: '闲猫助手', version: app.getVersion() }))
      return
    }
    // 网页「一键使用」只负责拉起助手窗口；选客户端在助手内完成
    if (req.method === 'POST' && url.pathname === '/open') {
      try {
        const raw = await readBody(req)
        const body = raw ? JSON.parse(raw) : {}
        if (body?.api) rememberPreferredSite(body.api)
      } catch {
        /* body 可选 */
      }
      createWindow()
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify({ ok: true, opened: true, api: resolveSiteAPI() }))
      return
    }
    // 仅同步站点根地址（未登录也可），供局域网 Windows 登录 / 换票使用
    if (req.method === 'POST' && url.pathname === '/session/site') {
      try {
        const raw = await readBody(req)
        const body = JSON.parse(raw || '{}')
        const saved = rememberPreferredSite(body.api || body.api_base || body.site)
        res.writeHead(saved ? 200 : 400, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify({ ok: !!saved, api: saved || null }))
      } catch (err) {
        res.writeHead(500, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify({ ok: false, error: err instanceof Error ? err.message : 'error' }))
      }
      return
    }
    if (req.method === 'GET' && url.pathname === '/status') {
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(
        JSON.stringify({
          ok: true,
          uiReady,
          jobSeq: oneClickJobSeq,
          pending: pendingProgress.length,
          last: lastProgress,
          account: readBoundAccount(),
        }),
      )
      return
    }
    if (req.method === 'POST' && url.pathname === '/one-click') {
      try {
        const raw = await readBody(req)
        const body = JSON.parse(raw || '{}')
        const parsed = {
          ticket: String(body.ticket || ''),
          client: String(body.client || ''),
          os: String(body.os || ''),
          api: String(body.api || '').replace(/\/$/, ''),
        }
        if (!parsed.ticket || !parsed.api) {
          res.writeHead(400, { 'Content-Type': 'application/json' })
          res.end(JSON.stringify({ ok: false, error: 'ticket and api required' }))
          return
        }
        createWindow()
        res.writeHead(202, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify({ ok: true, accepted: true, queued: true }))
        void enqueueOneClick(parsed, sendToUI).catch((err) => {
          sendToUI({
            step: 'error',
            status: 'error',
            message: err instanceof Error ? err.message : '一键使用失败',
          })
        })
      } catch (err) {
        res.writeHead(500, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify({ ok: false, error: err instanceof Error ? err.message : 'error' }))
      }
      return
    }
    // 网站登录态桥：浏览器无法把 Cookie 塞给助手，只能主动推送 / 退出时通知清本地
    if (req.method === 'POST' && url.pathname === '/session/sync') {
      try {
        const raw = await readBody(req)
        const body = JSON.parse(raw || '{}')
        const bound = syncBoundSession({
          user_id: body.user_id,
          account: body.account || body.email,
          api: body.api,
        })
        const creds = readBoundCredentials()
        const account = bound
          ? {
              ...bound,
              has_credentials: hasHelperAuth(creds),
              logged_in: true,
            }
          : null
        notifySessionUI(account)
        res.writeHead(200, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify({ ok: true, account }))
      } catch (err) {
        res.writeHead(500, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify({ ok: false, error: err instanceof Error ? err.message : 'error' }))
      }
      return
    }
    if (req.method === 'POST' && url.pathname === '/session/logout') {
      clearBoundAccount()
      notifySessionUI(null)
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify({ ok: true, logged_in: false }))
      return
    }
    res.writeHead(404, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ ok: false, error: 'not found' }))
  })
  localServer.listen(LOCAL_PORT, '127.0.0.1', () => {
    console.log(`[xianmao-helper] local bridge http://127.0.0.1:${LOCAL_PORT}`)
  })
  localServer.on('error', (err) => {
    console.error('[xianmao-helper] local bridge failed', err)
  })
}

registerProtocolClient()

const gotLock = app.requestSingleInstanceLock()
if (!gotLock) {
  app.quit()
} else {
  app.on('second-instance', (_event, argv) => {
    const link = takeArgvDeepLink(argv)
    if (link) void handleDeepLink(link)
    else createWindow()
  })

  // 关窗后仍保活本机桥（网页要继续探测 39217）；macOS 点程序坞图标会再开窗
  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit()
  })

  app.whenReady().then(() => {
    migrateSiteToProduction()
    if (process.platform === 'darwin' && app.dock) {
      try {
        const { nativeImage } = require('electron')
        const png = path.join(APP_ROOT, 'build', 'icon.png')
        const imgPath = fs.existsSync(png) ? png : APP_ICON
        if (imgPath) {
          const img = nativeImage.createFromPath(imgPath)
          if (!img.isEmpty()) app.dock.setIcon(img)
        }
      } catch {
        /* ignore */
      }
    }
    registerProtocolClient()
    startLocalBridge()
    createWindow()
    const fromArgv = takeArgvDeepLink(process.argv) || pendingDeepLink
    pendingDeepLink = null
    if (fromArgv) void handleDeepLink(fromArgv)

    app.on('activate', () => {
      createWindow()
    })
  })

  app.on('open-url', (event, url) => {
    event.preventDefault()
    if (app.isReady()) void handleDeepLink(url)
    else pendingDeepLink = url
  })
}

ipcMain.handle('one-click:cancel', () => {
  cancelOneClick(sendToUI)
  return { ok: true }
})

ipcMain.handle('one-click:ui-ready', () => {
  markUiReady()
  return { ok: true }
})

ipcMain.handle('one-click:account', () => {
  const acc = readBoundAccount()
  if (!acc) {
    // 无账号关联时丢掉孤儿密钥，避免「已退出仍显示已登录」
    try {
      fs.unlinkSync(credentialsStorePath())
    } catch {
      /* ignore */
    }
    return null
  }
  const creds = readBoundCredentials()
  return {
    ...acc,
    account: String(acc.account || '').trim(),
    has_credentials: hasHelperAuth(creds),
    logged_in: true,
  }
})

ipcMain.handle('one-click:site', () => {
  const api = resolveSiteAPI()
  return {
    ok: true,
    api,
    site: `${api}/`,
    login: `${api}/login`,
    needs_site: false,
  }
})

ipcMain.handle('one-click:set-site', () => {
  // 正式包不允许用户改站点；仅返回固定地址
  const api = resolveSiteAPI()
  return { ok: true, api, site: `${api}/`, login: `${api}/login`, needs_site: false }
})

ipcMain.handle('one-click:wallet', async () => {
  const bound = readBoundAccount()
  const creds = bound ? readBoundCredentials({ allowMigrate: true }) : null
  const bearer = helperAuthBearer(creds)
  if (!bound || !bearer) {
    return { ok: false, error: '未登录或尚未同步密钥' }
  }
  const api = resolveSiteAPI()
  try {
    const res = await fetch(`${api}/api/one-click/helper/wallet`, {
      headers: {
        Accept: 'application/json',
        Authorization: `Bearer ${bearer}`,
      },
      cache: 'no-store',
    })
    const body = await res.json().catch(() => ({}))
    if (!res.ok) {
      const msg = body?.error?.message || body?.message || `HTTP ${res.status}`
      return { ok: false, error: msg }
    }
    const data = body?.data || body
    return {
      ok: true,
      user_id: Number(data.user_id) || Number(bound.user_id) || 0,
      account: String(data.account || bound.account || '').trim(),
      balance: data.balance ?? null,
      refundable: data.refundable ?? null,
      ledger_cash: data.ledger_cash ?? null,
      recharge_url: String(data.recharge_url || '/buyer/wallet'),
    }
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : String(err) }
  }
})

/** 接口错误码 → 用户可读中文；避免把英文/技术文案直接甩到界面上。 */
function friendlyAPIError(code, message, fallback) {
  const c = String(code || '').toUpperCase()
  const byCode = {
    CHANNEL_REQUIRED: '请选择支付方式',
    CHANNEL_UNAVAILABLE: '当前支付方式暂不可用，请换一种再试',
    PAYMENT_UNAVAILABLE: '当前支付方式暂时不可用，请换一种再试',
    AMOUNT_OUT_OF_RANGE: '充值金额超出允许范围',
    AMOUNT_INVALID: '请输入正确的金额',
    UNAUTHORIZED: '登录已失效，请重新登录',
    FORBIDDEN: '没有权限执行此操作',
    NOT_FOUND: '未找到相关内容',
    RATE_LIMITED: '操作太频繁，请稍后再试',
  }
  if (byCode[c]) return byCode[c]
  const msg = String(message || '').trim()
  if (!msg) return fallback || '操作失败，请稍后再试'
  // 已是中文则原样展示
  if (/[\u4e00-\u9fff]/.test(msg)) return msg
  // 英文 / 技术文案统一兜底，不直接暴露给用户
  return fallback || '操作失败，请稍后再试'
}

async function helperAPI(pathname, { method = 'GET', body } = {}) {
  const bound = readBoundAccount()
  const creds = bound ? readBoundCredentials({ allowMigrate: true }) : null
  const bearer = helperAuthBearer(creds)
  if (!bound || !bearer) {
    return { ok: false, error: '未登录或尚未同步密钥' }
  }
  const api = resolveSiteAPI()
  const headers = {
    Accept: 'application/json',
    Authorization: `Bearer ${bearer}`,
  }
  let payload
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }
  try {
    const res = await fetch(`${api}${pathname}`, {
      method,
      headers,
      body: payload,
      cache: 'no-store',
    })
    const raw = await res.json().catch(() => ({}))
    if (!res.ok) {
      const code = raw?.error?.code || raw?.code || ''
      const msg = raw?.error?.message || raw?.message || ''
      return {
        ok: false,
        error: friendlyAPIError(code, msg, '操作失败，请稍后再试'),
        code: String(code || ''),
        status: res.status,
      }
    }
    return { ok: true, data: raw?.data !== undefined ? raw.data : raw, api }
  } catch (err) {
    const raw = err instanceof Error ? err.message : String(err)
    return { ok: false, error: friendlyAPIError('', raw, '网络异常，请检查网络后重试') }
  }
}

async function withQrDataURL(order) {
  if (!order || typeof order !== 'object') return order
  const payment = order.payment
  const qrText =
    (payment && payment.kind === 'qrcode' && payment.qr_code) ||
    (order.pay_kind === 'qrcode' && order.pay_payload) ||
    ''
  if (!qrText) return order
  try {
    const QRCode = require('qrcode')
    const qr_data_url = await QRCode.toDataURL(String(qrText), {
      width: 220,
      margin: 1,
      errorCorrectionLevel: 'M',
      color: { dark: '#17140F', light: '#FFFFFF' },
    })
    return {
      ...order,
      payment: payment
        ? { ...payment, qr_code: payment.qr_code || qrText, qr_data_url }
        : { kind: 'qrcode', qr_code: qrText, qr_data_url },
    }
  } catch {
    return order
  }
}

ipcMain.handle('one-click:recharge-options', async () => {
  const r = await helperAPI('/api/one-click/helper/recharge/options')
  if (!r.ok) return r
  return {
    ok: true,
    channels: Array.isArray(r.data?.channels) ? r.data.channels : [],
    recharge_min: Number(r.data?.recharge_min) || 1,
    recharge_max: Number(r.data?.recharge_max) || 50000,
  }
})

ipcMain.handle('one-click:create-recharge', async (_evt, amount, channel) => {
  const origin = resolveSiteAPI()
  const r = await helperAPI('/api/one-click/helper/recharge', {
    method: 'POST',
    body: {
      amount: Number(amount),
      channel: String(channel || ''),
      origin,
    },
  })
  if (!r.ok) return r
  const order = await withQrDataURL(r.data?.order || r.data)
  return { ok: true, order, payment: order?.payment || r.data?.payment || null }
})

ipcMain.handle('one-click:get-recharge', async (_evt, id) => {
  const orderId = Number(id)
  if (!orderId) return { ok: false, error: '无效订单' }
  const r = await helperAPI(`/api/one-click/helper/recharge/${orderId}`)
  if (!r.ok) return r
  const order = await withQrDataURL(r.data)
  return { ok: true, order }
})

/** 兼容旧调用：改为由 UI 弹窗处理；仍可打开网站钱包作兜底 */
ipcMain.handle('one-click:open-recharge', async () => {
  const bound = readBoundAccount()
  if (!bound) {
    return { ok: false, error: '请先登录闲猫' }
  }
  return { ok: true, in_app: true }
})

ipcMain.handle('one-click:logout-account', () => {
  cancelBrowserLogin()
  clearBoundAccount()
  return { ok: true, logged_in: false }
})

ipcMain.handle('one-click:login-account', async () => {
  createWindow()
  try {
    return await startBrowserLogin(sendToUI)
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    sendToUI({
      step: 'session',
      status: 'error',
      done: false,
      sessionSync: true,
      loginPending: false,
      message: msg,
      account: readBoundAccount(),
    })
    return { ok: false, error: msg }
  }
})

ipcMain.handle('one-click:cancel-login', () => {
  cancelBrowserLogin()
  sendToUI({
    step: 'session',
    status: 'idle',
    done: false,
    sessionSync: true,
    loginPending: false,
    message: '',
    account: readBoundAccount(),
  })
  return { ok: true }
})

ipcMain.handle('one-click:agents', async () => {
  const { listAgentsStatus } = require('./agents.cjs')
  const { loadAgentSavings } = require('./savings.cjs')
  const bound = readBoundAccount()
  if (!bound) {
    try {
      fs.unlinkSync(credentialsStorePath())
    } catch {
      /* ignore */
    }
  }
  const creds = bound ? readBoundCredentials() : null
  const hasCredentials = hasHelperAuth(creds)
  const userId = Number(bound?.user_id) || 0
  const account = String(bound?.account || '').trim()
  // 登录态只看 bound-account（网站一键使用换票关联），不单凭本地密钥文件
  const loggedIn = !!bound
  const agents = listAgentsStatus()
  const api = resolveSiteAPI()
  const savings = await loadAgentSavings(
    api,
    agents.map((a) => a.id),
  )
  for (const a of agents) {
    const s = savings[a.id]
    if (!s) continue
    a.best_mult = s.best_mult
    a.save_percent = s.save_percent
    a.save_label = s.save_label
  }
  return {
    ok: true,
    bound: loggedIn,
    loggedIn,
    hasCredentials,
    keyName: creds?.key_name || bound?.key_name || '',
    baseUrl: creds?.base_url || '',
    account,
    userId,
    agents,
  }
})

ipcMain.handle('one-click:preflight', async (_e, clientId) => {
  const { isKnownAgent } = require('./agents.cjs')
  const { runPreflight } = require('./preflight.cjs')
  const client = String(clientId || '').trim()
  if (!isKnownAgent(client)) return { ok: false, error: '不支持的客户端' }
  return { ok: true, ...runPreflight(client) }
})

ipcMain.handle('one-click:setup-agent', async (_e, clientId, opts) => {
  if (!readBoundAccount()) {
    return { ok: false, error: '请先登录闲猫（点右上角「浏览器登录」，或在网站点「一键使用」）' }
  }
  if (!readBoundCredentials({ allowMigrate: true })) {
    return {
      ok: false,
      error: '还没有连上闲猫。请回网站再点一次「一键使用」',
    }
  }
  const { isKnownAgent } = require('./agents.cjs')
  const { runPreflight } = require('./preflight.cjs')
  const client = String(clientId || '').trim()
  if (!isKnownAgent(client)) return { ok: false, error: '不支持的客户端' }
  const force = !!(opts && opts.force)
  const skipPreflight = !!(opts && opts.skipPreflight)
  const platform = String((opts && opts.platform) || '').trim()
  if (!skipPreflight) {
    const pf = runPreflight(client)
    if (!pf.ok && !force) {
      return { ok: false, error: '本机资源不满足安装要求', preflight: pf, blocked: true }
    }
  }
  createWindow()
  void enqueueLocalSetup(client, sendToUI, { platform })
  return { ok: true, queued: true, client, platform }
})

ipcMain.handle('one-click:vendors', async (_e, clientId) => {
  const { preferredVendor, platformLabel, AGENT_VENDORS } = require('./vendors.cjs')
  const client = String(clientId || '').trim()
  const bound = readBoundAccount()
  const creds = bound ? readBoundCredentials({ allowMigrate: true }) : null
  const def = preferredVendor(client)
  const fallback = () => {
    const supported = Object.prototype.hasOwnProperty.call(AGENT_VENDORS, client)
      ? AGENT_VENDORS[client]
      : null
    const list = Array.isArray(supported)
      ? supported
      : ['openai', 'anthropic', 'gemini', 'deepseek', 'kimi', 'composite']
    return {
      ok: true,
      client,
      vendors: list.map((p) => ({
        platform: p,
        label: platformLabel(p),
        default: p === def,
      })),
    }
  }
  const bearer = helperAuthBearer(creds)
  if (!bearer) return fallback()
  const api = resolveSiteAPI()
  try {
    const res = await fetch(`${api}/api/one-click/helper/vendors?client=${encodeURIComponent(client)}`, {
      headers: {
        Accept: 'application/json',
        Authorization: `Bearer ${bearer}`,
      },
      cache: 'no-store',
    })
    const body = await res.json().catch(() => ({}))
    if (!res.ok) return fallback()
    const data = body?.data || body
    const vendors = Array.isArray(data.vendors) ? data.vendors : []
    if (!vendors.length) return fallback()
    return { ok: true, client, vendors }
  } catch {
    return fallback()
  }
})

ipcMain.handle('one-click:open-external', async (_e, url) => {
  if (typeof url === 'string' && /^https?:\/\//i.test(url)) {
    await shell.openExternal(url)
    return true
  }
  return false
})

ipcMain.handle('one-click:open-cli', async (_e, which, binHint) => {
  if (!readBoundAccount()) {
    return { ok: false, error: '请先登录闲猫（点右上角「浏览器登录」，或在网站点「一键使用」）' }
  }
  const { openCLIInTerminal, whichBin } = require('./auto-install.cjs')
  const cliNameMap = {
    'claude-code': 'claude',
    hermes: 'hermes',
    pi: 'pi',
    opencode: 'opencode',
    cline: 'cline',
    aider: 'aider',
    'kimi-code': 'kimi',
    'qwen-code': 'qwen',
    gemini: 'gemini',
    goose: 'goose',
    crush: 'crush',
    'kilo-code': 'kilo',
    openclaw: 'openclaw',
  }
  const name = cliNameMap[which] || ''
  const bin =
    (typeof binHint === 'string' && binHint) ||
    (name && (whichBin(name) || whichBin(`${name}.cmd`) || whichBin(`${name}.exe`))) ||
    ''
  if (!bin) return { ok: false, error: `未找到 ${which || 'CLI'}，请先完成一键安装` }
  const ok = openCLIInTerminal(bin)
  return ok ? { ok: true } : { ok: false, error: '无法打开系统终端' }
})

ipcMain.handle('one-click:open-app', async (_e, which) => {
  // 未关联闲猫账号时，不允许当「免费本地启动器」用
  if (!readBoundAccount()) {
    return { ok: false, error: '请先登录闲猫（点右上角「浏览器登录」，或在网站点「一键使用」）' }
  }
  const { findInstalledApp, openInstalledApp } = require('./auto-install.cjs')
  const path = require('path')
  const win = process.platform === 'win32'
  const names =
    which === 'cherry'
      ? win
        ? ['Cherry Studio', path.join('Cherry Studio', 'Cherry Studio.exe')]
        : ['Cherry Studio.app', 'CherryStudio.app']
      : which === 'chatbox'
        ? win
          ? ['Chatbox', path.join('Chatbox', 'Chatbox.exe')]
          : ['Chatbox.app', 'chatbox.app']
        : which === 'codex'
          ? win
            ? [path.join('ChatGPT', 'ChatGPT.exe'), 'ChatGPT', path.join('Programs', 'ChatGPT', 'ChatGPT.exe')]
            : ['ChatGPT.app', 'Codex.app', 'ChatGPT Codex.app']
          : which === 'claude-desktop'
            ? win
              ? ['Claude', path.join('Claude', 'Claude.exe')]
              : ['Claude.app']
            : which === 'obsidian'
              ? win
                ? ['Obsidian', path.join('Obsidian', 'Obsidian.exe')]
                : ['Obsidian.app']
              : which === 'paseo'
                ? win
                  ? ['Paseo', path.join('Paseo', 'Paseo.exe')]
                  : ['Paseo.app']
                : which === 'zcode'
                  ? win
                    ? ['ZCode', path.join('ZCode', 'ZCode.exe')]
                    : ['ZCode.app']
                  : which === 'workbuddy'
                    ? win
                      ? ['WorkBuddy', path.join('WorkBuddy', 'WorkBuddy.exe')]
                      : ['WorkBuddy.app']
                    : which === 'codebuddy'
                      ? win
                        ? ['CodeBuddy', 'CodeBuddy CN', path.join('CodeBuddy', 'CodeBuddy.exe'), path.join('CodeBuddy CN', 'CodeBuddy CN.exe')]
                        : ['CodeBuddy.app', 'CodeBuddy IDE.app', 'CodeBuddy CN.app']
                      : which === 'trae'
                        ? win
                          ? ['Trae', 'Trae CN', path.join('Trae', 'Trae.exe'), path.join('Trae CN', 'Trae CN.exe')]
                          : ['Trae.app', 'TRAE.app', 'Trae CN.app']
                        : []
  let hit = findInstalledApp(names)
  if (!hit && which === 'codex' && win) {
    const { isChatGPTWinPackageInstalled, CHATGPT_WIN_APPPATH } = require('./auto-install.cjs')
    if (isChatGPTWinPackageInstalled()) hit = CHATGPT_WIN_APPPATH
  }
  if (!hit) {
    if (which === 'codex') {
      const { whichBin, openCLIInTerminal } = require('./auto-install.cjs')
      const cli = whichBin('codex') || whichBin('codex.cmd')
      if (cli) {
        const ok = openCLIInTerminal(cli)
        return ok ? { ok: true } : { ok: false, error: '无法打开终端' }
      }
    }
    return { ok: false, error: '未安装' }
  }
  try {
    const opened = openInstalledApp(hit)
    if (opened) return { ok: true }
    if (which === 'codex') {
      const { whichBin, openCLIInTerminal } = require('./auto-install.cjs')
      const cli = whichBin('codex') || whichBin('codex.cmd')
      if (cli) {
        const ok = openCLIInTerminal(cli)
        return ok ? { ok: true } : { ok: false, error: '无法打开终端' }
      }
    }
    return { ok: false, error: '打不开该应用' }
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : '打开失败' }
  }
})

ipcMain.handle('one-click:installed', async () => {
  const { listAgentsStatus } = require('./agents.cjs')
  const map = {}
  for (const a of listAgentsStatus()) {
    map[a.id] = a.installed
    if (a.id === 'cherry-studio') map.cherry = a.installed
    if (a.id === 'claude-desktop') map.claudeDesktop = a.installed
  }
  return map
})

ipcMain.handle('one-click:uninstall-agent', async (_e, clientId) => {
  const { isKnownAgent, getAgent } = require('./agents.cjs')
  const { uninstallAgent } = require('./uninstall-agent.cjs')
  const client = String(clientId || '').trim()
  if (!isKnownAgent(client)) return { ok: false, error: '不支持的客户端' }
  try {
    const r = uninstallAgent(client)
    return { ok: true, name: getAgent(client)?.name || client, ...r }
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : '卸载失败' }
  }
})

ipcMain.handle('one-click:open-agent', async (_e, clientId) => {
  if (!readBoundAccount()) {
    return { ok: false, error: '请先登录闲猫（点右上角「浏览器登录」，或在网站点「一键使用」）' }
  }
  const { getAgent, probeAgentInstalled } = require('./agents.cjs')
  const { openInstalledApp, openCLIInTerminal } = require('./auto-install.cjs')
  const agent = getAgent(String(clientId || ''))
  if (!agent) return { ok: false, error: '未知客户端' }
  const st = probeAgentInstalled(agent.id)
  if (agent.open === 'web') {
    await shell.openExternal('http://127.0.0.1:3080')
    return { ok: true }
  }
  if (agent.open === 'cli') {
    const bin = st.cliBin
    if (!bin) return { ok: false, error: '未安装，请先点「一键使用」自动安装' }
    const ok = openCLIInTerminal(bin)
    return ok ? { ok: true } : { ok: false, error: '无法打开终端' }
  }
  if (st.appPath) {
    try {
      if (openInstalledApp(st.appPath)) return { ok: true }
    } catch (err) {
      return { ok: false, error: err instanceof Error ? err.message : '打开失败' }
    }
  }
  // Codex / 桌面打不开：有 CLI 则开终端
  if (st.cliBin) {
    const ok = openCLIInTerminal(st.cliBin)
    return ok ? { ok: true } : { ok: false, error: '无法打开终端' }
  }
  return { ok: false, error: '未安装，请先点「一键使用」自动安装' }
})

// 开发时可用 file:// 打开 UI；生产由 loadFile
void pathToFileURL
