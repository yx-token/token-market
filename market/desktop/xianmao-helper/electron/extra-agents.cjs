/**
 * 扩展 Agent：安装探测 + 配置写入（OpenCode / Cline / Aider / …）。
 */
const fs = require('fs')
const os = require('os')
const path = require('path')
const { spawnSync } = require('child_process')
const https = require('https')
const http = require('http')
const {
  ensureNode,
  runNpmGlobal,
  whichBin,
  findInstalledApp,
  ensureShellPathHint,
  npmPrefixDir,
  throwIfCancelled,
  yieldEventLoop,
  openCLIInTerminal,
  downloadAndInstall,
  brewInstallCask,
} = require('./auto-install.cjs')
const { defaultModelForPlatform } = require('./vendors.cjs')

function isWin() {
  return process.platform === 'win32'
}

function isMac() {
  return process.platform === 'darwin'
}

function archTag() {
  const a = process.arch
  if (a === 'arm64' || a === 'aarch64') return 'arm64'
  return 'x64'
}

function fetchText(url, redirects = 0) {
  return new Promise((resolve, reject) => {
    const lib = String(url).startsWith('https') ? https : http
    const req = lib.get(url, { headers: { 'User-Agent': 'xianmao-helper', Accept: 'text/html,*/*' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location && redirects < 5) {
        res.resume()
        fetchText(res.headers.location, redirects + 1).then(resolve, reject)
        return
      }
      if (res.statusCode && res.statusCode >= 400) {
        res.resume()
        reject(new Error(`HTTP ${res.statusCode}`))
        return
      }
      const chunks = []
      res.on('data', (c) => chunks.push(c))
      res.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')))
    })
    req.on('error', reject)
    req.setTimeout(20000, () => {
      req.destroy(new Error('timeout'))
    })
  })
}

/** 从官网历史页解析最新安装包；失败则用已知可用直链兜底。 */
async function resolveWorkBuddyDownloadUrl() {
  const plat = isWin()
    ? 'win32-x64-user'
    : archTag() === 'arm64'
      ? 'darwin-arm64'
      : 'darwin-x64'
  const ext = isWin() ? 'exe' : 'dmg'
  const re = new RegExp(
    `https://[^"'\\s<>]+/workbuddy/saas/${plat}/WorkBuddy-${plat}-[0-9][^"'\\s<>]+\\.${ext}`,
    'i',
  )
  try {
    const html = await fetchText('https://www.workbuddy.cn/docs/workbuddy/Download-History')
    const m = html.match(re)
    if (m) return m[0]
  } catch {
    /* fallback below */
  }
  // 2026-11-15 归档直链（历史页不可达时用）
  if (isWin()) {
    return 'https://acc-1258344699.cos.ap-guangzhou.myqcloud.com/workbuddy/saas/win32-x64-user/WorkBuddy-win32-x64-user-5.11.3.43806934-ecace0f7.exe'
  }
  if (archTag() === 'arm64') {
    return 'https://acc-1258344699.cos.ap-guangzhou.myqcloud.com/workbuddy/saas/darwin-arm64/WorkBuddy-darwin-arm64-5.11.3.43806934-ecace0f7.dmg'
  }
  return 'https://acc-1258344699.cos.ap-guangzhou.myqcloud.com/workbuddy/saas/darwin-x64/WorkBuddy-darwin-x64-5.11.3.43806934-ecace0f7.dmg'
}

function withV1(base) {
  const b = String(base || '').replace(/\/$/, '')
  if (!b) return b
  return /\/v1$/i.test(b) ? b : `${b}/v1`
}

function mergeJSONFile(filePath, mergeFn) {
  let current = {}
  if (fs.existsSync(filePath)) {
    try {
      current = JSON.parse(fs.readFileSync(filePath, 'utf8'))
    } catch {
      current = {}
    }
    fs.copyFileSync(filePath, `${filePath}.bak-xianmao`)
  }
  const next = mergeFn(current)
  fs.mkdirSync(path.dirname(filePath), { recursive: true })
  fs.writeFileSync(filePath, JSON.stringify(next, null, 2) + '\n', 'utf8')
  return filePath
}

function writeEnvFile(filePath, entries) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true })
  if (fs.existsSync(filePath)) fs.copyFileSync(filePath, `${filePath}.bak-xianmao`)
  let cur = {}
  if (fs.existsSync(filePath)) {
    try {
      for (const line of fs.readFileSync(filePath, 'utf8').split(/\r?\n/)) {
        const m = line.match(/^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/)
        if (m) cur[m[1]] = m[2]
      }
    } catch {
      cur = {}
    }
  }
  cur = { ...cur, ...entries }
  const body = Object.entries(cur)
    .map(([k, v]) => `${k}=${v}`)
    .join('\n')
  fs.writeFileSync(filePath, body + '\n', 'utf8')
  return filePath
}

async function ensureNpmPkg(binName, pkg, label, fallbackUrl, onProgress) {
  refreshQuiet()
  const existing = whichBin(binName) || whichBin(`${binName}.cmd`)
  if (existing) return { already: true, bin: existing }
  await ensureNode(onProgress)
  if (typeof onProgress === 'function') onProgress({ message: `正在安装 ${label}…` })
  const r = runNpmGlobal(pkg, onProgress, label)
  refreshQuiet()
  const bin = whichBin(binName) || whichBin(`${binName}.cmd`)
  if (r.status === 0 && bin) {
    ensureShellPathHint(path.dirname(bin))
    return { already: false, installed: true, bin }
  }
  // 部分包 bin 名与包名不一致，再试一次带 prefix 的路径
  if (bin) {
    ensureShellPathHint(path.dirname(bin))
    return { already: false, installed: true, bin }
  }
  const err = new Error((r.stderr || r.stdout || `${label} 安装失败`).slice(0, 400))
  err.fallbackUrl = fallbackUrl
  throw err
}

function refreshQuiet() {
  try {
    const { refreshUserPath } = require('./auto-install.cjs')
    if (typeof refreshUserPath === 'function') refreshUserPath()
  } catch {
    /* ignore */
  }
}

async function ensureOpenCode(onProgress) {
  const existing = whichBin('opencode') || whichBin('opencode.cmd')
  if (existing) return { already: true, bin: existing }
  if (!isWin()) {
    if (typeof onProgress === 'function') onProgress({ message: '正在安装 OpenCode…' })
    const off = spawnSync('bash', ['-lc', 'curl -fsSL https://opencode.ai/v2/install | bash'], {
      encoding: 'utf8',
      timeout: 600000,
      env: process.env,
    })
    refreshQuiet()
    const bin = whichBin('opencode')
    if (off.status === 0 && bin) return { already: false, installed: true, bin, via: 'curl' }
  }
  return ensureNpmPkg('opencode', '@opencode/cli', 'OpenCode', 'https://opencode.ai/terminal', onProgress)
}

async function ensureCline(onProgress) {
  // npm 包 cline 提供 bin=cline；@cline/cli 的 bin 是 clite
  try {
    return await ensureNpmPkg('cline', 'cline', 'Cline', 'https://docs.cline.bot/', onProgress)
  } catch (err) {
    try {
      const r = await ensureNpmPkg('clite', '@cline/cli', 'Cline', 'https://docs.cline.bot/', onProgress)
      return { ...r, bin: r.bin || whichBin('clite') || whichBin('cline') }
    } catch {
      return { already: false, installed: false, fallbackUrl: err.fallbackUrl || 'https://docs.cline.bot/' }
    }
  }
}

async function ensureAider(onProgress) {
  const existing = whichBin('aider') || whichBin('aider.cmd')
  if (existing) return { already: true, bin: existing }
  if (typeof onProgress === 'function') onProgress({ message: '正在安装 Aider…' })
  const pip = whichBin('pip3') || whichBin('pip') || whichBin('pip3.exe') || whichBin('pip.exe')
  if (pip) {
    const r = spawnSync(pip, ['install', '--user', 'aider-chat'], {
      encoding: 'utf8',
      timeout: 600000,
      env: process.env,
      shell: isWin(),
    })
    refreshQuiet()
    const bin = whichBin('aider') || whichBin('aider.cmd')
    if (r.status === 0 && bin) return { already: false, installed: true, bin, via: 'pip' }
  }
  const err = new Error('Aider 安装失败，请先安装 Python/pip')
  err.fallbackUrl = 'https://aider.chat/docs/install.html'
  throw err
}

async function ensureKimiCode(onProgress) {
  const existing = whichBin('kimi') || whichBin('kimi.cmd')
  if (existing) return { already: true, bin: existing }
  const fallbackUrl = 'https://www.kimi.com/code'
  // 官方安装器（新一代 Kimi Code CLI）
  if (!isWin()) {
    if (typeof onProgress === 'function') onProgress({ message: '正在安装 Kimi Code…' })
    const off = spawnSync('bash', ['-lc', 'curl -fsSL https://code.kimi.com/kimi-code/install.sh | bash'], {
      encoding: 'utf8',
      timeout: 600000,
      env: process.env,
    })
    refreshQuiet()
    const bin = whichBin('kimi')
    if (off.status === 0 && bin) return { already: false, installed: true, bin, via: 'curl' }
  } else {
    if (typeof onProgress === 'function') onProgress({ message: '正在安装 Kimi Code…' })
    const off = spawnSync(
      'powershell.exe',
      ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', 'irm https://code.kimi.com/kimi-code/install.ps1 | iex'],
      { encoding: 'utf8', timeout: 600000, env: process.env },
    )
    refreshQuiet()
    const bin = whichBin('kimi') || whichBin('kimi.cmd')
    if (off.status === 0 && bin) return { already: false, installed: true, bin, via: 'ps1' }
  }
  try {
    return await ensureNpmPkg('kimi', '@moonshot-ai/kimi-code', 'Kimi Code', fallbackUrl, onProgress)
  } catch (err) {
    const e = new Error(err instanceof Error ? err.message : String(err))
    e.fallbackUrl = fallbackUrl
    throw e
  }
}

async function ensureQwenCode(onProgress) {
  try {
    return await ensureNpmPkg('qwen', '@qwen-code/qwen-code', 'Qwen Code', 'https://github.com/QwenLM/qwen-code', onProgress)
  } catch {
    return ensureNpmPkg('qwen', 'qwen-code', 'Qwen Code', 'https://github.com/QwenLM/qwen-code', onProgress)
  }
}

async function ensureGeminiCLI(onProgress) {
  return ensureNpmPkg('gemini', '@google/gemini-cli', 'Gemini CLI', 'https://geminicli.com/docs/get-started/installation/', onProgress)
}

async function ensureGoose(onProgress) {
  const existing = whichBin('goose') || whichBin('goose.cmd')
  if (existing) return { already: true, bin: existing }
  if (typeof onProgress === 'function') onProgress({ message: '正在安装 Goose…' })
  if (!isWin()) {
    const off = spawnSync(
      'bash',
      ['-lc', 'curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | bash'],
      { encoding: 'utf8', timeout: 600000, env: process.env },
    )
    refreshQuiet()
    const bin = whichBin('goose')
    if (off.status === 0 && bin) return { already: false, installed: true, bin, via: 'curl' }
  } else {
    const off = spawnSync(
      'powershell.exe',
      [
        '-NoProfile',
        '-ExecutionPolicy',
        'Bypass',
        '-Command',
        'irm https://github.com/block/goose/releases/download/stable/download_cli.ps1 | iex',
      ],
      { encoding: 'utf8', timeout: 600000, env: process.env },
    )
    refreshQuiet()
    const bin = whichBin('goose') || whichBin('goose.cmd')
    if (off.status === 0 && bin) return { already: false, installed: true, bin, via: 'ps1' }
  }
  const err = new Error('Goose 安装失败')
  err.fallbackUrl = 'https://block.github.io/goose/'
  throw err
}

async function ensureCrush(onProgress) {
  return ensureNpmPkg('crush', '@charmland/crush', 'Crush', 'https://crush.charm.land/installation/', onProgress)
}

async function ensureKiloCode(onProgress) {
  return ensureNpmPkg('kilo', '@kilocode/cli', 'Kilo Code', 'https://kilo.ai/docs/code-with-ai/platforms/cli', onProgress)
}

function probeDesktop(names, opts = {}) {
  const app = findInstalledApp(names, opts)
  return { installed: !!app, appPath: app || null }
}

function probeWorkBuddy() {
  const win = isWin()
  return probeDesktop(
    win
      ? [
          path.join('Programs', 'WorkBuddy', 'WorkBuddy.exe'),
          path.join('WorkBuddy', 'WorkBuddy.exe'),
          'WorkBuddy',
          path.join('Tencent', 'WorkBuddy', 'WorkBuddy.exe'),
        ]
      : ['WorkBuddy.app', 'workbuddy.app'],
    win
      ? {
          processExes: ['WorkBuddy.exe'],
          registryHints: ['workbuddy'],
          preferExes: ['WorkBuddy.exe'],
        }
      : {},
  )
}

function probeCodeBuddy() {
  const win = isWin()
  return probeDesktop(
    win
      ? [
          'CodeBuddy',
          'CodeBuddy CN',
          path.join('CodeBuddy', 'CodeBuddy.exe'),
          path.join('CodeBuddy CN', 'CodeBuddy CN.exe'),
          path.join('Tencent', 'CodeBuddy', 'CodeBuddy.exe'),
        ]
      : ['CodeBuddy.app', 'CodeBuddy IDE.app', 'CodeBuddy CN.app'],
    win
      ? {
          processExes: ['CodeBuddy.exe', 'CodeBuddy CN.exe'],
          registryHints: ['codebuddy'],
          preferExes: ['CodeBuddy.exe', 'CodeBuddy CN.exe'],
        }
      : {},
  )
}

function probeTrae() {
  const win = isWin()
  return probeDesktop(
    win
      ? ['Trae', 'Trae CN', path.join('Trae', 'Trae.exe'), path.join('Trae CN', 'Trae CN.exe'), path.join('ByteDance', 'Trae', 'Trae.exe')]
      : ['Trae.app', 'TRAE.app', 'Trae CN.app'],
    win
      ? {
          processExes: ['Trae.exe', 'Trae CN.exe'],
          registryHints: ['trae'],
          preferExes: ['Trae.exe', 'Trae CN.exe'],
        }
      : {},
  )
}

async function ensureWorkBuddy(onProgress) {
  const names = isWin()
    ? [
        path.join('Programs', 'WorkBuddy', 'WorkBuddy.exe'),
        path.join('WorkBuddy', 'WorkBuddy.exe'),
        'WorkBuddy',
        path.join('Tencent', 'WorkBuddy', 'WorkBuddy.exe'),
      ]
    : ['WorkBuddy.app', 'workbuddy.app']
  const hit = findInstalledApp(
    names,
    isWin()
      ? { processExes: ['WorkBuddy.exe'], registryHints: ['workbuddy'], preferExes: ['WorkBuddy.exe'] }
      : {},
  )
  if (hit) return { already: true, appPath: hit, installed: true }

  if (!isMac() && !isWin()) {
    return { already: false, fallbackUrl: 'https://www.workbuddy.cn' }
  }

  const fallbackUrl = isWin()
    ? 'https://www.workbuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Installation-Win-Guide'
    : 'https://www.workbuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Installation-Mac-Guide'

  // macOS：优先 Homebrew cask
  if (isMac()) {
    for (const cask of ['workbuddy-cn', 'workbuddy-ai']) {
      const brew = await brewInstallCask(cask, 'WorkBuddy', onProgress)
      if (brew && brew.status === 0) {
        const after = findInstalledApp(names)
        if (after) return { already: false, installed: true, appPath: after, via: 'brew' }
      }
    }
  }

  try {
    if (typeof onProgress === 'function') onProgress({ message: '正在下载 WorkBuddy…' })
    const url = await resolveWorkBuddyDownloadUrl()
    throwIfCancelled()
    const r = await downloadAndInstall({
      label: 'WorkBuddy',
      directUrl: url,
      appNames: isMac() ? ['WorkBuddy.app'] : [],
      onProgress,
    })
    const after = findInstalledApp(names)
    return { already: false, installed: !!(after || r.appPath), appPath: after || r.appPath || null, ...r }
  } catch (err) {
    const e = new Error(err instanceof Error ? err.message : String(err))
    e.fallbackUrl = fallbackUrl
    throw e
  }
}

async function resolveCodeBuddyDownloadUrl() {
  const arch = archTag() === 'arm64' ? 'arm64' : 'x64'
  let ver = '4.12.4.43368774'
  let hash = '62ef1801'
  try {
    // Windows update API 平台名不稳定，用 darwin 探测版本号再拼各平台直链
    const raw = await fetchText(
      `https://copilot.tencent.com/v2/update?platform=ide-darwin-${arch}&version=1.0.0&x-machine-id=default`,
    )
    const j = JSON.parse(raw)
    if (j && j.version) ver = String(j.version)
    const um = String(j.url || '').match(/CodeBuddy-[^-]+-[^-]+-[\d.]+-([0-9a-f]+)-cn/i)
    if (um) hash = um[1]
  } catch {
    /* fallback below */
  }
  if (isWin()) {
    return `https://download.codebuddy.cn/aiide/win32-x64-user/CodeBuddy-win32-x64-user-${ver}-${hash}-cn.exe`
  }
  if (isMac()) {
    return `https://download.codebuddy.cn/aiide/darwin-${arch}/CodeBuddy-darwin-${arch}-${ver}-${hash}-cn.dmg`
  }
  return null
}

async function resolveTraeDownloadUrl() {
  const api = 'https://api.trae.ai/icube/api/v1/native/version/trae/cn/latest'
  try {
    const raw = await fetchText(api)
    const j = JSON.parse(raw)
    const manifest = j?.data?.manifest || {}
    if (isWin()) {
      const list = manifest.win32?.download || []
      const cn = list.find((x) => x && x.region === 'cn' && x.x64)
      if (cn?.x64) return cn.x64
      const any = list.find((x) => x && x.x64)
      if (any?.x64) return any.x64
    }
    if (isMac()) {
      const list = manifest.darwin?.download || []
      const cn = list.find((x) => x && x.region === 'cn')
      const pick = cn || list[0]
      if (pick) {
        if (archTag() === 'arm64' && pick.apple) return pick.apple
        if (pick.intel) return pick.intel
        if (pick.apple) return pick.apple
      }
    }
  } catch {
    /* fallback */
  }
  // 已知可用直链兜底（版本可能落后）
  if (isWin()) {
    return 'https://lf-cdn.trae.com.cn/obj/trae-com-cn/pkg/app/releases/stable/2.3.108162/win32/TRAE_CN-Setup-x64.exe'
  }
  if (archTag() === 'arm64') {
    return 'https://lf-cdn.trae.com.cn/obj/trae-com-cn/pkg/app/releases/stable/2.3.108162/darwin/TRAE_CN-darwin-arm64.dmg'
  }
  return 'https://lf-cdn.trae.com.cn/obj/trae-com-cn/pkg/app/releases/stable/2.3.108162/darwin/TRAE_CN-darwin-x64.dmg'
}

async function ensureCodeBuddy(onProgress) {
  const names = isWin()
    ? [
        'CodeBuddy',
        path.join('CodeBuddy', 'CodeBuddy.exe'),
        'CodeBuddy CN',
        path.join('CodeBuddy CN', 'CodeBuddy CN.exe'),
        path.join('Programs', 'CodeBuddy CN', 'CodeBuddy CN.exe'),
        path.join('Programs', 'CodeBuddy', 'CodeBuddy.exe'),
      ]
    : ['CodeBuddy.app', 'CodeBuddy IDE.app', 'CodeBuddy CN.app']
  const hit = findInstalledApp(
    names,
    isWin()
      ? {
          processExes: ['CodeBuddy.exe', 'CodeBuddy CN.exe'],
          registryHints: ['codebuddy'],
          preferExes: ['CodeBuddy.exe', 'CodeBuddy CN.exe'],
        }
      : {},
  )
  if (hit) return { already: true, appPath: hit, installed: true }

  const fallbackUrl = 'https://www.codebuddy.cn/ide/'
  if (isMac()) {
    for (const cask of ['codebuddy-cn', 'codebuddy']) {
      const brew = await brewInstallCask(cask, 'CodeBuddy', onProgress)
      if (brew && brew.status === 0) {
        const after = findInstalledApp(names)
        if (after) return { already: false, installed: true, appPath: after, via: 'brew' }
      }
    }
  }

  try {
    if (typeof onProgress === 'function') onProgress({ message: '正在下载 CodeBuddy…' })
    const url = await resolveCodeBuddyDownloadUrl()
    if (!url) {
      const err = new Error('请先安装 CodeBuddy')
      err.fallbackUrl = fallbackUrl
      throw err
    }
    throwIfCancelled()
    const r = await downloadAndInstall({
      label: 'CodeBuddy',
      directUrl: url,
      appNames: isMac() ? ['CodeBuddy CN.app', 'CodeBuddy.app', 'CodeBuddy IDE.app'] : [],
      onProgress,
    })
    await new Promise((r) => setTimeout(r, 2000))
    const after = findInstalledApp(names)
    return { already: false, installed: !!(after || r.appPath), appPath: after || r.appPath || null, ...r }
  } catch (err) {
    const e = new Error(err instanceof Error ? err.message : String(err))
    e.fallbackUrl = fallbackUrl
    throw e
  }
}

async function ensureTrae(onProgress) {
  const names = isWin()
    ? [
        'Trae',
        'Trae CN',
        path.join('Trae', 'Trae.exe'),
        path.join('Trae CN', 'Trae CN.exe'),
        path.join('Programs', 'Trae CN', 'Trae CN.exe'),
        path.join('Programs', 'Trae', 'Trae.exe'),
      ]
    : ['Trae.app', 'TRAE.app', 'Trae CN.app']
  const hit = findInstalledApp(
    names,
    isWin()
      ? {
          processExes: ['Trae.exe', 'Trae CN.exe'],
          registryHints: ['trae'],
          preferExes: ['Trae.exe', 'Trae CN.exe'],
        }
      : {},
  )
  if (hit) return { already: true, appPath: hit, installed: true }

  const fallbackUrl = 'https://www.trae.cn/download'
  if (isMac()) {
    for (const cask of ['trae-cn', 'trae']) {
      const brew = await brewInstallCask(cask, 'Trae', onProgress)
      if (brew && brew.status === 0) {
        const after = findInstalledApp(names)
        if (after) return { already: false, installed: true, appPath: after, via: 'brew' }
      }
    }
  }

  try {
    if (typeof onProgress === 'function') onProgress({ message: '正在下载 Trae…' })
    const url = await resolveTraeDownloadUrl()
    throwIfCancelled()
    const r = await downloadAndInstall({
      label: 'Trae',
      directUrl: url,
      appNames: isMac() ? ['Trae CN.app', 'Trae.app', 'TRAE.app'] : [],
      onProgress,
    })
    await new Promise((r) => setTimeout(r, 2000))
    const after = findInstalledApp(names)
    return { already: false, installed: !!(after || r.appPath), appPath: after || r.appPath || null, ...r }
  } catch (err) {
    const e = new Error(err instanceof Error ? err.message : String(err))
    e.fallbackUrl = fallbackUrl
    throw e
  }
}

function modelOf(payload) {
  return payload.model || defaultModelForPlatform(payload.platform) || 'gpt-5.5'
}

function writeOpenCode(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const endpoint = withV1(base)
  const modelID = modelOf(payload)
  const file = path.join(os.homedir(), '.config', 'opencode', 'opencode.json')
  const wrote = mergeJSONFile(file, (current) => ({
    ...current,
    $schema: 'https://opencode.ai/config.json',
    model: `gateway/${modelID}`,
    provider: {
      ...(current.provider || {}),
      gateway: {
        npm: '@ai-sdk/openai-compatible',
        name: payload.provider || '闲猫',
        options: { baseURL: endpoint, apiKey: payload.key },
        models: { [modelID]: { name: modelID } },
      },
    },
  }))
  return { wrote: [wrote] }
}

function writeCline(payload) {
  // Cline CLI 读 ~/.cline/data/{globalState,secrets}.json；新版还会迁到 data/settings/providers.json
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const endpoint = withV1(base)
  const modelID = modelOf(payload)
  const key = String(payload.key || '')
  const dataDir = path.join(os.homedir(), '.cline', 'data')
  const settingsDir = path.join(dataDir, 'settings')
  fs.mkdirSync(settingsDir, { recursive: true })

  const globalState = path.join(dataDir, 'globalState.json')
  const secrets = path.join(dataDir, 'secrets.json')
  const providers = path.join(settingsDir, 'providers.json')
  const legacy = path.join(os.homedir(), '.cline', 'xianmao.json')

  const wrote = []
  wrote.push(
    mergeJSONFile(globalState, (current) => ({
      ...current,
      apiProvider: 'openai',
      actModeApiProvider: 'openai',
      planModeApiProvider: 'openai',
      openAiBaseUrl: endpoint,
      openAiModelId: modelID,
      planModeOpenAiModelId: modelID,
      openAiModelInfo: current.openAiModelInfo || undefined,
    })),
  )
  wrote.push(
    mergeJSONFile(secrets, (current) => ({
      ...current,
      openAiApiKey: key,
    })),
  )
  wrote.push(
    mergeJSONFile(providers, (current) => {
      const list = Array.isArray(current.providers) ? current.providers.filter((p) => p && p.id !== 'openai') : []
      list.unshift({
        id: 'openai',
        name: payload.provider || '闲猫',
        apiProvider: 'openai',
        openAiBaseUrl: endpoint,
        openAiApiKey: key,
        openAiModelId: modelID,
      })
      return {
        ...current,
        activeProviderId: 'openai',
        providers: list,
      }
    }),
  )
  wrote.push(
    mergeJSONFile(legacy, () => ({
      apiProvider: 'openai',
      openAiBaseUrl: endpoint,
      openAiApiKey: key,
      openAiModelId: modelID,
      providerName: payload.provider || '闲猫',
    })),
  )
  try {
    fs.chmodSync(secrets, 0o600)
  } catch {
    /* win */
  }
  return { wrote }
}

function writeAider(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const conf = path.join(os.homedir(), '.aider.conf.yml')
  const endpoint = withV1(base)
  const model = modelOf(payload)
  const yml = [
    `openai-api-key: ${JSON.stringify(payload.key)}`,
    `openai-api-base: ${JSON.stringify(endpoint)}`,
    `model: ${JSON.stringify(`openai/${model}`)}`,
    '',
  ].join('\n')
  if (fs.existsSync(conf)) fs.copyFileSync(conf, `${conf}.bak-xianmao`)
  fs.writeFileSync(conf, yml, 'utf8')
  // 同时写环境变量文件，方便 shell 启动
  const envFile = writeEnvFile(path.join(os.homedir(), '.aider', 'xianmao.env'), {
    OPENAI_API_KEY: payload.key,
    OPENAI_API_BASE: endpoint,
  })
  return { wrote: [conf, envFile] }
}

function writeKimiCode(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const file = writeEnvFile(path.join(os.homedir(), '.kimi', '.env'), {
    KIMI_BASE_URL: withV1(base),
    KIMI_API_KEY: payload.key,
    OPENAI_API_KEY: payload.key,
    OPENAI_BASE_URL: withV1(base),
  })
  return { wrote: [file] }
}

function writeQwenCode(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const file = writeEnvFile(path.join(os.homedir(), '.qwen', '.env'), {
    OPENAI_API_KEY: payload.key,
    OPENAI_BASE_URL: withV1(base),
    OPENAI_MODEL: modelOf(payload),
  })
  return { wrote: [file] }
}

function writeWorkBuddy(payload) {
  // WorkBuddy 自定义模型：官方路径是 ~/.workbuddy/models.json 与 ~/.workbuddy-ai/models.json。
  // vendor 必须是协议名 OpenAI（OpenAI 兼容），展示名用 name；写完需完全退出再开，并在底部模型下拉里选。
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const endpoint = withV1(base)
  const modelID = modelOf(payload)
  const display = `闲猫 · ${modelID}`
  const entry = {
    id: modelID,
    name: display,
    vendor: 'OpenAI',
    apiKey: String(payload.key || ''),
    url: `${endpoint}/chat/completions`,
    supportsToolCall: true,
    // 尽量标成默认；WorkBuddy 内置「快速/均衡/极致」仍可能优先，新会话未必自动切过去
    isDefault: true,
  }
  const paths = [
    path.join(os.homedir(), '.workbuddy', 'models.json'),
    path.join(os.homedir(), '.workbuddy-ai', 'models.json'),
    path.join(os.homedir(), '.codebuddy', 'models.json'),
    path.join(os.homedir(), '.codebuddycn', 'models.json'),
  ]
  const wrote = []
  for (const file of paths) {
    let current = { models: [], availableModels: [] }
    if (fs.existsSync(file)) {
      try {
        current = JSON.parse(fs.readFileSync(file, 'utf8')) || current
      } catch {
        current = { models: [], availableModels: [] }
      }
      fs.copyFileSync(file, `${file}.bak-xianmao`)
    }
    const models = Array.isArray(current.models)
      ? current.models
          .filter((m) => m && m.id !== modelID)
          .map((m) => (m && typeof m === 'object' ? { ...m, isDefault: false } : m))
      : []
    models.unshift(entry)
    const available = Array.isArray(current.availableModels)
      ? current.availableModels.filter((id) => id && id !== modelID)
      : []
    available.unshift(modelID)
    const body = {
      ...current,
      models,
      availableModels: available,
      defaultModel: modelID,
    }
    fs.mkdirSync(path.dirname(file), { recursive: true })
    fs.writeFileSync(file, JSON.stringify(body, null, 2) + '\n', 'utf8')
    wrote.push(file)
  }
  return { wrote }
}

function writeCodeBuddy(payload) {
  // CodeBuddy 与 WorkBuddy 同源 models.json
  return writeWorkBuddy(payload)
}

function writeTrae(payload) {
  // Trae 官方不读本地 models 文件（模型在云端/加密库）。写好「一键粘贴」卡片到多处，并尽量塞进 settings 备查。
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const endpoint = withV1(base)
  const modelID = modelOf(payload)
  const card = {
    name: payload.provider || '闲猫',
    apiKey: String(payload.key || ''),
    baseUrl: endpoint,
    fullUrl: `${endpoint}/chat/completions`,
    model: modelID,
    protocol: 'openai',
    apiFormat: 'OpenAI Chat Completions',
    howTo: [
      '打开 Trae → 设置 → 模型 → 添加模型 → 自定义模型',
      'API 格式选 OpenAI Chat Completions',
      '关闭「完整 URL」，粘贴 baseUrl；或打开后粘贴 fullUrl',
      '模型 ID / 展示名 / API 密钥按本文件填写',
    ],
  }
  const paths = [
    path.join(os.homedir(), '.trae', 'xianmao-provider.json'),
    path.join(os.homedir(), '.trae-cn', 'xianmao-provider.json'),
    path.join(os.homedir(), 'Downloads', 'xianmao-helper', 'trae-xianmao.json'),
  ]
  const wrote = []
  for (const file of paths) {
    wrote.push(mergeJSONFile(file, () => card))
  }
  return { wrote }
}

function writeGeminiCLI(payload) {
  // Gemini CLI 走 /v1beta/...，base 不要带 /v1，否则路径会错。
  const base = String(payload.base_url || '').replace(/\/$/, '').replace(/\/v1$/i, '')
  const modelID = modelOf(payload)
  const envFile = writeEnvFile(path.join(os.homedir(), '.gemini', '.env'), {
    GOOGLE_GEMINI_BASE_URL: base,
    GEMINI_API_KEY: payload.key,
    GEMINI_MODEL: modelID,
  })
  const settings = path.join(os.homedir(), '.gemini', 'settings.json')
  mergeJSONFile(settings, (current) => ({
    ...current,
    security: {
      ...(current.security || {}),
      auth: { selectedType: 'gemini-api-key' },
    },
  }))
  return { wrote: [envFile, settings] }
}

function writeGoose(payload) {
  // Goose：OPENAI_HOST 是根地址（无 /v1），会再拼 OPENAI_BASE_PATH=v1/chat/completions。
  // API Key 写进 config.yaml 会被忽略 → 401；应放 secrets.yaml。
  const base = String(payload.base_url || '').replace(/\/$/, '').replace(/\/v1$/i, '')
  const dir = path.join(os.homedir(), '.config', 'goose')
  fs.mkdirSync(dir, { recursive: true })
  const file = path.join(dir, 'config.yaml')
  const secrets = path.join(dir, 'secrets.yaml')
  if (fs.existsSync(file)) fs.copyFileSync(file, `${file}.bak-xianmao`)
  if (fs.existsSync(secrets)) fs.copyFileSync(secrets, `${secrets}.bak-xianmao`)
  const yml = [
    'GOOSE_PROVIDER: openai',
    `GOOSE_MODEL: ${modelOf(payload)}`,
    `OPENAI_HOST: ${base}`,
    'OPENAI_BASE_PATH: v1/chat/completions',
    '',
  ].join('\n')
  fs.writeFileSync(file, yml, 'utf8')
  const sec = [`OPENAI_API_KEY: ${JSON.stringify(String(payload.key || ''))}`, ''].join('\n')
  fs.writeFileSync(secrets, sec, 'utf8')
  try {
    fs.chmodSync(secrets, 0o600)
  } catch {
    /* win */
  }
  return { wrote: [file, secrets] }
}

function writeCrush(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const file = path.join(os.homedir(), '.config', 'crush', 'crush.json')
  const modelID = modelOf(payload)
  const wrote = mergeJSONFile(file, (current) => ({
    ...current,
    providers: {
      ...(current.providers || {}),
      xianmao: {
        name: payload.provider || '闲猫',
        base_url: withV1(base),
        api_key: payload.key,
        type: 'openai',
        models: [{ id: modelID, name: modelID }],
      },
    },
  }))
  return { wrote: [wrote] }
}

function writeKiloCode(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const file = path.join(os.homedir(), '.kilocode', 'config.json')
  const wrote = mergeJSONFile(file, (current) => ({
    ...current,
    apiProvider: 'openai',
    openAiBaseUrl: withV1(base),
    openAiApiKey: payload.key,
    openAiModelId: modelOf(payload),
  }))
  return { wrote: [wrote] }
}

/**
 * @returns {Promise<{ mode: string, detail: string, urls: string[], helpUrl?: string, wrote?: string[], openCli?: string, openApp?: string } | null>}
 */
async function applyExtraAgent(payload, client, onProgress) {
  const handlers = {
    opencode: { ensure: ensureOpenCode, write: writeOpenCode, help: 'https://opencode.ai/terminal', cli: true },
    cline: { ensure: ensureCline, write: writeCline, help: 'https://docs.cline.bot/', cli: true },
    aider: { ensure: ensureAider, write: writeAider, help: 'https://aider.chat/docs/install.html', cli: true },
    'kimi-code': { ensure: ensureKimiCode, write: writeKimiCode, help: 'https://www.kimi.com/code', cli: true },
    'qwen-code': { ensure: ensureQwenCode, write: writeQwenCode, help: 'https://github.com/QwenLM/qwen-code', cli: true },
    workbuddy: { ensure: ensureWorkBuddy, write: writeWorkBuddy, help: 'https://www.workbuddy.cn', app: true },
    codebuddy: { ensure: ensureCodeBuddy, write: writeCodeBuddy, help: 'https://copilot.tencent.com/codebuddy', app: true },
    trae: { ensure: ensureTrae, write: writeTrae, help: 'https://www.trae.ai/', app: true },
    gemini: { ensure: ensureGeminiCLI, write: writeGeminiCLI, help: 'https://geminicli.com/docs/get-started/installation/', cli: true },
    goose: { ensure: ensureGoose, write: writeGoose, help: 'https://block.github.io/goose/', cli: true },
    crush: { ensure: ensureCrush, write: writeCrush, help: 'https://crush.charm.land/installation/', cli: true },
    'kilo-code': { ensure: ensureKiloCode, write: writeKiloCode, help: 'https://kilo.ai/docs/code-with-ai/platforms/cli', cli: true },
  }
  const h = handlers[client]
  if (!h) return null

  let pack
  try {
    pack = await h.ensure(onProgress)
  } catch (err) {
    pack = {
      error: err instanceof Error ? err.message : String(err),
      fallbackUrl: err?.fallbackUrl || h.help,
    }
  }
  await yieldEventLoop()
  throwIfCancelled()
  const r = h.write(payload)
  const bin = pack?.bin || null
  const appPath = pack?.appPath || null
  const ok = !!(pack?.already || pack?.installed || bin || appPath)
  let detail
  if (client === 'trae') {
    detail = ok
      ? 'Trae 已装好；自定义模型请按提示粘贴（数值已写到 Downloads/xianmao-helper/trae-xianmao.json）'
      : 'Trae 还没装好；粘贴卡已写入，请点「打开安装说明」'
  } else {
    detail = ok
      ? `${labelOf(client)} 已配好闲猫${h.cli ? '，即将打开终端' : h.app ? '，即将打开' : ''}`
      : `${labelOf(client)} 还没装好，配置已写入；请点「打开安装说明」`
  }
  return {
    mode: 'files',
    detail,
    urls: [],
    helpUrl: ok ? undefined : pack?.fallbackUrl || h.help,
    wrote: r.wrote,
    openCli: h.cli ? bin || undefined : undefined,
    openApp: h.app ? appPath || undefined : undefined,
  }
}

function labelOf(id) {
  const map = {
    opencode: 'OpenCode',
    cline: 'Cline',
    aider: 'Aider',
    'kimi-code': 'Kimi Code',
    'qwen-code': 'Qwen Code',
    workbuddy: 'WorkBuddy',
    codebuddy: 'CodeBuddy',
    trae: 'Trae',
    gemini: 'Gemini CLI',
    goose: 'Goose',
    crush: 'Crush',
    'kilo-code': 'Kilo Code',
  }
  return map[id] || id
}

function probeCLI(names) {
  for (const n of names) {
    const bin = whichBin(n) || whichBin(`${n}.cmd`) || whichBin(`${n}.exe`)
    if (bin) return { installed: true, cliBin: bin }
  }
  return { installed: false, cliBin: null }
}

function probeExtraInstalled(id) {
  switch (id) {
    case 'opencode':
      return probeCLI(['opencode'])
    case 'cline':
      return probeCLI(['cline'])
    case 'aider':
      return probeCLI(['aider'])
    case 'kimi-code':
      return probeCLI(['kimi', 'kimi-code'])
    case 'qwen-code':
      return probeCLI(['qwen', 'qoder'])
    case 'workbuddy':
      return probeWorkBuddy()
    case 'codebuddy':
      return probeCodeBuddy()
    case 'trae':
      return probeTrae()
    case 'gemini':
      return probeCLI(['gemini'])
    case 'goose':
      return probeCLI(['goose'])
    case 'crush':
      return probeCLI(['crush'])
    case 'kilo-code':
      return probeCLI(['kilo'])
    default:
      return null
  }
}

module.exports = {
  applyExtraAgent,
  probeExtraInstalled,
  ensureOpenCode,
  ensureGeminiCLI,
  writeOpenCode,
  writeGeminiCLI,
  writeWorkBuddy,
  openCLIInTerminal,
}
