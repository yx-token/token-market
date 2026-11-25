/**
 * 助手侧自动下载/安装客户端（浏览器做不到静默安装）。
 * - macOS：dmg → 挂载拷贝到 /Applications
 * - Windows：msi/exe → 下载后拉起安装程序（需用户点下一步/UAC）
 * - CLI（Codex / Claude Code）：零基础也能装——先官方安装器，再便携 Node + npm，不依赖本机预装环境
 */
const fs = require('fs')
const os = require('os')
const path = require('path')
const { execFileSync, spawn, spawnSync } = require('child_process')
const https = require('https')
const http = require('http')

function archTag() {
  const a = os.arch()
  if (a === 'arm64') return 'arm64'
  if (a === 'x64' || a === 'ia32') return 'x64'
  return a
}

function platformKey() {
  if (process.platform === 'darwin') return 'mac'
  if (process.platform === 'win32') return 'win'
  return 'linux'
}

function isWin() {
  return process.platform === 'win32'
}

function isMac() {
  return process.platform === 'darwin'
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms))
}

/** 让出事件循环，避免长时间 spawnSync 时本机 /health 探活全部超时、网页误报离线。 */
function yieldEventLoop() {
  return new Promise((resolve) => setImmediate(resolve))
}

/** 当前一键任务是否要求停止；下载子进程登记在此，停止时 SIGTERM。 */
let cancelRequested = false
const activeChildren = new Set()

function cancelledError(msg = '已停止') {
  const err = new Error(msg)
  err.cancelled = true
  return err
}

function isCancelledError(err) {
  return !!(err && (err.cancelled || /已停止/.test(String(err.message || err))))
}

function resetCancel() {
  cancelRequested = false
}

function throwIfCancelled() {
  if (cancelRequested) throw cancelledError()
}

function trackChild(child) {
  if (!child) return () => {}
  activeChildren.add(child)
  const clear = () => activeChildren.delete(child)
  child.once('exit', clear)
  child.once('close', clear)
  return clear
}

function requestCancel() {
  cancelRequested = true
  for (const child of [...activeChildren]) {
    try {
      child.kill('SIGTERM')
    } catch {
      /* ignore */
    }
    try {
      if (isWin()) child.kill()
    } catch {
      /* ignore */
    }
  }
  activeChildren.clear()
}

function downloadDir() {
  const dir = path.join(os.homedir(), 'Downloads', 'xianmao-helper')
  fs.mkdirSync(dir, { recursive: true })
  return dir
}

/** 各端常见安装路径 */
function appLookupPaths(names) {
  const home = os.homedir()
  if (isMac()) {
    return names.flatMap((n) => [`/Applications/${n}`, path.join(home, 'Applications', n)])
  }
  if (isWin()) {
    const local = process.env.LOCALAPPDATA || path.join(home, 'AppData', 'Local')
    const roaming = process.env.APPDATA || path.join(home, 'AppData', 'Roaming')
    const pf = process.env.ProgramFiles || 'C:\\Program Files'
    const pf86 = process.env['ProgramFiles(x86)'] || 'C:\\Program Files (x86)'
    return names.flatMap((n) => [
      path.join(local, n),
      path.join(local, 'Programs', n),
      path.join(roaming, n),
      path.join(pf, n),
      path.join(pf86, n),
    ])
  }
  return names.flatMap((n) => [`/usr/local/${n}`, path.join(home, '.local', n)])
}

/** Windows：安装目录里找可启动的 .exe（排除卸载/安装器）；数据目录无 exe 则视为未安装。 */
function resolveWinExeInDir(dir) {
  const base = path.basename(dir)
  const prefer = [
    path.join(dir, `${base}.exe`),
    path.join(dir, `${base.replace(/\s+/g, '')}.exe`),
    path.join(dir, 'ChatGPT.exe'),
    path.join(dir, 'Codex.exe'),
    path.join(dir, 'app', `${base}.exe`),
  ]
  for (const p of prefer) {
    try {
      if (fs.existsSync(p) && fs.statSync(p).isFile()) return p
    } catch {
      /* ignore */
    }
  }
  try {
    const exes = fs
      .readdirSync(dir)
      .filter((n) => /\.exe$/i.test(n) && !/uninstall|setup|update|crash|helper/i.test(n))
      .map((n) => path.join(dir, n))
    if (exes.length === 1) return exes[0]
    const lower = base.toLowerCase()
    const match = exes.find((p) => path.basename(p).toLowerCase().startsWith(lower.slice(0, Math.min(4, lower.length))))
    if (match) return match
  } catch {
    /* ignore */
  }
  return null
}

/** 把「路径候选」解析成真正可打开的目标；纯数据目录（如 Local\\Codex）返回 null。 */
function resolveLaunchableApp(candidate) {
  if (!candidate) return null
  if (String(candidate).startsWith('msix:')) return candidate
  try {
    if (!fs.existsSync(candidate)) return null
    const st = fs.statSync(candidate)
    if (st.isFile()) {
      if (isWin() && !/\.(exe|cmd|bat)$/i.test(candidate)) return null
      return candidate
    }
    if (st.isDirectory()) {
      if (isMac() && /\.app$/i.test(candidate)) return candidate
      if (isWin()) return resolveWinExeInDir(candidate)
    }
  } catch {
    return null
  }
  return null
}

function findInstalledApp(names) {
  for (const p of appLookupPaths(names)) {
    const hit = resolveLaunchableApp(p)
    if (hit) return hit
  }
  return undefined
}

function runtimeRoot() {
  const dir = path.join(os.homedir(), '.xianmao-helper', 'runtime')
  fs.mkdirSync(dir, { recursive: true })
  return dir
}

function npmPrefixDir() {
  const dir = path.join(runtimeRoot(), 'npm-global')
  fs.mkdirSync(dir, { recursive: true })
  return dir
}

/** 把用户可写的 bin 目录塞进当前进程 PATH，安装后立刻能 which 到。 */
function prependPath(dirs) {
  const sep = isWin() ? ';' : ':'
  const cur = String(process.env.PATH || process.env.Path || '')
  const parts = cur.split(sep).filter(Boolean)
  const add = dirs.filter((d) => d && fs.existsSync(d) && !parts.includes(d))
  if (!add.length) return
  const next = [...add, ...parts].join(sep)
  process.env.PATH = next
  if (isWin()) process.env.Path = next
}

function hermesHomeDir() {
  if (process.env.HERMES_HOME) return process.env.HERMES_HOME
  if (isWin()) {
    const local = process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData', 'Local')
    return path.join(local, 'hermes')
  }
  return path.join(os.homedir(), '.hermes')
}

function refreshUserPath() {
  const home = os.homedir()
  const dirs = [
    path.join(home, '.local', 'bin'),
    path.join(hermesHomeDir(), 'bin'),
    path.join(npmPrefixDir(), isWin() ? '' : 'bin'),
    path.join(runtimeRoot(), 'node', 'bin'),
    path.join(runtimeRoot(), 'node'), // Windows zip 根目录就有 node.exe
    '/usr/local/bin',
    '/opt/homebrew/bin',
  ]
  if (isWin()) {
    const local = process.env.LOCALAPPDATA || path.join(home, 'AppData', 'Local')
    dirs.push(
      path.join(npmPrefixDir()),
      path.join(local, 'hermes', 'bin'),
      path.join(local, 'Programs', 'nodejs'),
      path.join(process.env.ProgramFiles || 'C:\\Program Files', 'nodejs'),
    )
  }
  prependPath(dirs)
}

function whichBin(name) {
  refreshUserPath()
  const candidates = []
  if (isWin()) {
    const r = spawnSync('where', [name], { encoding: 'utf8', shell: true, env: process.env })
    if (r.status === 0) {
      for (const line of String(r.stdout || '').split(/\r?\n/)) {
        if (line.trim()) candidates.push(line.trim())
      }
    }
    const extras = [
      path.join(os.homedir(), '.local', 'bin', name),
      path.join(os.homedir(), '.local', 'bin', `${name}.cmd`),
      path.join(npmPrefixDir(), `${name}.cmd`),
      path.join(npmPrefixDir(), name),
      path.join(runtimeRoot(), 'node', `${name}.cmd`),
      path.join(runtimeRoot(), 'node', `${name}.exe`),
    ]
    for (const p of extras) {
      if (fs.existsSync(p)) candidates.push(p)
    }
  } else {
    const r = spawnSync('which', [name], { encoding: 'utf8', env: process.env })
    if (r.status === 0 && r.stdout.trim()) candidates.push(r.stdout.trim())
    const extras = [
      path.join(os.homedir(), '.local', 'bin', name),
      path.join(npmPrefixDir(), 'bin', name),
      path.join(runtimeRoot(), 'node', 'bin', name),
      `/usr/local/bin/${name}`,
      `/opt/homebrew/bin/${name}`,
    ]
    for (const p of extras) {
      if (fs.existsSync(p)) candidates.push(p)
    }
  }
  return candidates.find((p) => {
    try {
      return fs.existsSync(p)
    } catch {
      return false
    }
  }) || ''
}

/** 把 ~/.local/bin 写进常见 shell 配置，新开终端也能找到 claude。 */
function ensureShellPathHint(binDir) {
  if (isWin() || !binDir) return
  const home = os.homedir()
  const line = `export PATH="${binDir}:$PATH" # xianmao-helper`
  for (const file of ['.zprofile', '.zshrc', '.bash_profile', '.bashrc', '.profile']) {
    const p = path.join(home, file)
    try {
      const cur = fs.existsSync(p) ? fs.readFileSync(p, 'utf8') : ''
      if (cur.includes('xianmao-helper') && cur.includes(binDir)) continue
      if (cur.includes(binDir) && cur.includes('PATH=')) continue
      fs.appendFileSync(p, `\n${line}\n`, 'utf8')
    } catch {
      /* ignore */
    }
  }
}

function githubJSON(pathname) {
  return new Promise((resolve, reject) => {
    const req = https.request(
      {
        hostname: 'api.github.com',
        path: pathname,
        method: 'GET',
        headers: { 'User-Agent': 'xianmao-helper', Accept: 'application/vnd.github+json' },
      },
      (res) => {
        const chunks = []
        res.on('data', (c) => chunks.push(c))
        res.on('end', () => {
          try {
            resolve(JSON.parse(Buffer.concat(chunks).toString('utf8')))
          } catch (e) {
            reject(e)
          }
        })
      },
    )
    req.on('error', reject)
    req.end()
  })
}

async function resolveNodeLTS() {
  // nodejs.org/dist/index.json 含 LTS 标记
  const list = await new Promise((resolve, reject) => {
    https
      .get('https://nodejs.org/dist/index.json', { headers: { 'User-Agent': 'xianmao-helper' } }, (res) => {
        const chunks = []
        res.on('data', (c) => chunks.push(c))
        res.on('end', () => {
          try {
            resolve(JSON.parse(Buffer.concat(chunks).toString('utf8')))
          } catch (e) {
            reject(e)
          }
        })
      })
      .on('error', reject)
  })
  const lts = (Array.isArray(list) ? list : []).find((row) => row.lts)
  const ver = String(lts?.version || 'v22.14.0').replace(/^v/, '')
  return ver
}

/**
 * 无 Node 时下载官方便携包到 ~/.xianmao-helper/runtime/node（不需管理员）。
 */
async function ensureNode(onProgress) {
  refreshUserPath()
  const existing = whichBin('node') || whichBin('node.exe')
  const npmExisting = whichBin('npm') || whichBin('npm.cmd')
  if (existing && npmExisting) {
    return { already: true, node: existing, npm: npmExisting }
  }

  const report = (msg, pct) => {
    if (typeof onProgress === 'function') onProgress({ message: msg, percent: pct })
  }
  report('正在准备运行环境…', 0)

  const ver = await resolveNodeLTS().catch(() => '22.14.0')
  const arch = archTag() === 'arm64' ? 'arm64' : 'x64'
  let url
  let archiveName
  if (isMac()) {
    archiveName = `node-v${ver}-darwin-${arch}.tar.gz`
    url = `https://nodejs.org/dist/v${ver}/${archiveName}`
  } else if (isWin()) {
    archiveName = `node-v${ver}-win-${arch}.zip`
    url = `https://nodejs.org/dist/v${ver}/${archiveName}`
  } else {
    archiveName = `node-v${ver}-linux-${arch}.tar.xz`
    url = `https://nodejs.org/dist/v${ver}/${archiveName}`
  }

  const dest = path.join(downloadDir(), archiveName)
  await downloadFile(url, dest, (pct) => report(`正在下载运行环境… ${pct}%`, pct))

  const extractRoot = path.join(runtimeRoot(), 'node-extract')
  const finalDir = path.join(runtimeRoot(), 'node')
  try {
    fs.rmSync(extractRoot, { recursive: true, force: true })
  } catch {
    /* ignore */
  }
  fs.mkdirSync(extractRoot, { recursive: true })

  if (isWin()) {
    const ps = spawnSync(
      'powershell.exe',
      ['-NoProfile', '-Command', `Expand-Archive -LiteralPath '${dest.replace(/'/g, "''")}' -DestinationPath '${extractRoot.replace(/'/g, "''")}' -Force`],
      { encoding: 'utf8', timeout: 120000 },
    )
    if (ps.status !== 0) throw new Error(ps.stderr || ps.stdout || '解压 Node 失败')
  } else if (/\.tar\.gz$/i.test(archiveName)) {
    const tar = spawnSync('tar', ['-xzf', dest, '-C', extractRoot], { encoding: 'utf8' })
    if (tar.status !== 0) throw new Error(tar.stderr || '解压 Node 失败')
  } else {
    const tar = spawnSync('tar', ['-xJf', dest, '-C', extractRoot], { encoding: 'utf8' })
    if (tar.status !== 0) throw new Error(tar.stderr || '解压 Node 失败')
  }

  const entries = fs.readdirSync(extractRoot)
  const unpacked = entries.map((n) => path.join(extractRoot, n)).find((p) => fs.statSync(p).isDirectory())
  if (!unpacked) throw new Error('Node 解压后未找到目录')
  try {
    fs.rmSync(finalDir, { recursive: true, force: true })
  } catch {
    /* ignore */
  }
  fs.renameSync(unpacked, finalDir)
  try {
    fs.rmSync(extractRoot, { recursive: true, force: true })
  } catch {
    /* ignore */
  }

  refreshUserPath()
  const node = whichBin('node') || whichBin('node.exe')
  const npm = whichBin('npm') || whichBin('npm.cmd')
  if (!node || !npm) throw new Error('运行环境准备失败，请重试')
  report('运行环境已就绪', 100)
  return { already: false, installed: true, node, npm, version: ver }
}

function runNpmGlobal(pkg, onProgress, label) {
  refreshUserPath()
  const npmCmd = whichBin('npm.cmd') || whichBin('npm') || (isWin() ? 'npm.cmd' : 'npm')
  const prefix = npmPrefixDir()
  if (typeof onProgress === 'function') onProgress({ message: `正在安装 ${label || pkg}…` })
  const args = ['install', '-g', pkg, `--prefix=${prefix}`]
  const r = spawnSync(npmCmd, args, {
    encoding: 'utf8',
    timeout: 300000,
    shell: isWin(),
    env: {
      ...process.env,
      npm_config_prefix: prefix,
    },
  })
  refreshUserPath()
  return r
}

function headContentLength(url) {
  return new Promise((resolve) => {
    const getter = url.startsWith('https') ? https : http
    const req = getter.request(url, { method: 'HEAD', headers: { 'User-Agent': 'xianmao-helper' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        headContentLength(res.headers.location).then(resolve)
        return
      }
      resolve(Number(res.headers['content-length'] || 0))
    })
    req.on('error', () => resolve(0))
    req.end()
  })
}

function curlBin() {
  if (!isWin()) return 'curl'
  // Windows 10+ 自带 curl.exe
  const w = whichBin('curl.exe') || whichBin('curl')
  return w || 'curl.exe'
}

/** curl 优先；Windows 无 curl 时用 PowerShell。支持 requestCancel() 中断。 */
/** 浏览器 UA：部分 CDN（Claude 等）会拒 xianmao-helper 自定义 UA。 */
const DOWNLOAD_UA =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'

/**
 * 用 HEAD/Range 探一下总大小，便于进度条；失败就返回 0。
 */
function probeContentLength(url) {
  return new Promise((resolve) => {
    try {
      const bin = curlBin()
      const r = spawnSync(
        bin,
        ['-sI', '-L', '--connect-timeout', '12', '--max-time', '25', '-A', DOWNLOAD_UA, url],
        { encoding: 'utf8', timeout: 30000 },
      )
      const headers = String(r.stdout || '')
      const m = headers.match(/content-length:\s*(\d+)/i)
      resolve(m ? Number(m[1]) || 0 : 0)
    } catch {
      resolve(0)
    }
  })
}

function downloadFile(url, dest, onProgress) {
  return new Promise((resolve, reject) => {
    throwIfCancelled()
    const tmp = `${dest}.partial`
    // 保留未完成的 .partial，下面用 curl -C - 断点续传（大包超时后重试能接着下）
    if (typeof onProgress === 'function') onProgress(1)

    let settled = false
    let clearTrack = () => {}
    let expectLen = 0

    const tick = setInterval(() => {
      if (cancelRequested) {
        clearInterval(tick)
        try {
          clearTrack()
        } catch {
          /* ignore */
        }
        if (!settled) {
          settled = true
          // 取消才删 partial；超时失败保留以便续传
          try {
            fs.unlinkSync(tmp)
          } catch {
            /* ignore */
          }
          reject(cancelledError())
        }
        return
      }
      try {
        if (!fs.existsSync(tmp)) return
        const sz = fs.statSync(tmp).size
        let pct
        if (expectLen > 0) {
          pct = Math.min(95, Math.max(1, Math.round((sz / expectLen) * 100)))
        } else {
          // 未知大小：按每 8MB 约 1%，封顶 90，避免大包刚下几十 MB 就显示 95%
          pct = Math.min(90, 5 + Math.round(sz / (8 * 1024 * 1024)))
        }
        if (typeof onProgress === 'function') onProgress(pct)
      } catch {
        /* ignore */
      }
    }, 800)

    const finishOk = () => {
      if (settled) return
      settled = true
      clearInterval(tick)
      clearTrack()
      try {
        fs.renameSync(tmp, dest)
      } catch (e) {
        reject(e)
        return
      }
      if (typeof onProgress === 'function') onProgress(100)
      resolve(dest)
    }

    const fail = (msg, { keepPartial = false } = {}) => {
      if (settled) return
      settled = true
      clearInterval(tick)
      clearTrack()
      if (!keepPartial) {
        try {
          fs.unlinkSync(tmp)
        } catch {
          /* ignore */
        }
      }
      if (cancelRequested) reject(cancelledError())
      else reject(new Error(msg))
    }

    const tryCurl = () => {
      throwIfCancelled()
      const bin = curlBin()
      // 大包（ChatGPT ~580MB）在国内常见几百 KB/s，5 分钟必超时；不设整段 max-time，只限连接超时。
      // -C -：断点续传；--retry：瞬时断网再试。
      const child = spawn(
        bin,
        [
          '-L',
          '--fail',
          '--retry',
          '5',
          '--retry-delay',
          '3',
          '--retry-all-errors',
          '--connect-timeout',
          '30',
          '-C',
          '-',
          '-A',
          DOWNLOAD_UA,
          '-o',
          tmp,
          url,
        ],
        { stdio: ['ignore', 'ignore', 'pipe'], windowsHide: true },
      )
      clearTrack = trackChild(child)
      let errBuf = ''
      child.stderr.on('data', (c) => {
        errBuf += c
      })
      child.on('error', () => {
        if (cancelRequested) fail('已停止')
        else if (isWin()) tryPowerShell()
        else fail(`无法启动 curl: ${errBuf.slice(0, 120)}`)
      })
      child.on('close', (code) => {
        if (cancelRequested) fail('已停止')
        else if (code === 0) finishOk()
        else if (isWin()) tryPowerShell()
        else {
          const hint = /HTTP error 403|error: 22/i.test(errBuf)
            ? '（可能被地区/CDN 拒绝，请换网络或点「打开安装说明」）'
            : ''
          // 保留 partial，下次同 URL 可续传
          fail(`curl 下载失败 code=${code}${hint} ${errBuf.slice(0, 180)}`, { keepPartial: true })
        }
      })
    }

    const tryPowerShell = () => {
      if (cancelRequested) {
        fail('已停止')
        return
      }
      const ps = spawn(
        'powershell.exe',
        [
          '-NoProfile',
          '-Command',
          `Invoke-WebRequest -Uri '${url.replace(/'/g, "''")}' -OutFile '${tmp.replace(/'/g, "''")}' -UseBasicParsing -UserAgent '${DOWNLOAD_UA.replace(/'/g, "''")}'`,
        ],
        { stdio: ['ignore', 'ignore', 'pipe'], windowsHide: true },
      )
      clearTrack = trackChild(ps)
      let errBuf = ''
      ps.stderr.on('data', (c) => {
        errBuf += c
      })
      ps.on('error', (err) => fail(`PowerShell 下载失败: ${err.message}`))
      ps.on('close', (code) => {
        if (cancelRequested) fail('已停止')
        else if (code === 0) finishOk()
        else fail(`PowerShell 下载失败 code=${code} ${errBuf.slice(0, 200)}`)
      })
    }

    void (async () => {
      try {
        expectLen = await probeContentLength(url)
        tryCurl()
      } catch (err) {
        fail(err instanceof Error ? err.message : String(err))
      }
    })()
  })
}

function githubJSON(apiPath) {
  return new Promise((resolve, reject) => {
    https
      .get(
        `https://api.github.com${apiPath}`,
        {
          headers: {
            'User-Agent': 'xianmao-helper',
            Accept: 'application/vnd.github+json',
          },
        },
        (res) => {
          let raw = ''
          res.on('data', (c) => (raw += c))
          res.on('end', () => {
            if (res.statusCode !== 200) {
              reject(new Error(`GitHub API ${res.statusCode}: ${raw.slice(0, 200)}`))
              return
            }
            try {
              resolve(JSON.parse(raw))
            } catch (e) {
              reject(e)
            }
          })
        },
      )
      .on('error', reject)
  })
}

function pickAsset(assets, predicates) {
  const names = (assets || []).map((a) => ({ name: a.name || '', url: a.browser_download_url }))
  for (const pred of predicates) {
    const hit = names.find((a) => pred(a.name))
    if (hit) return hit
  }
  return null
}

async function resolveGitHubAsset(ownerRepo, predicates) {
  const rel = await githubJSON(`/repos/${ownerRepo}/releases/latest`)
  const asset = pickAsset(rel.assets || [], predicates)
  if (!asset) throw new Error(`未找到适合本机的安装包（${ownerRepo} / ${platformKey()}-${archTag()}）`)
  return { tag: rel.tag_name, name: asset.name, url: asset.url }
}

/** 挂载 dmg，把里面的 .app 拷到 Applications。 */
function installMacDmg(dmgPath, preferredAppNames = []) {
  if (!isMac()) throw new Error('仅支持 macOS 自动安装 dmg')
  const attach = spawnSync('hdiutil', ['attach', dmgPath, '-nobrowse', '-readonly', '-plist'], {
    encoding: 'utf8',
  })
  if (attach.status !== 0) {
    execFileSync('open', [dmgPath], { stdio: 'ignore' })
    return { mode: 'opened-dmg', appPath: null }
  }
  let mountPoint = null
  try {
    const m = attach.stdout.match(/<key>mount-point<\/key>\s*<string>([^<]+)<\/string>/)
    mountPoint = m && m[1]
    if (!mountPoint) {
      execFileSync('open', [dmgPath], { stdio: 'ignore' })
      return { mode: 'opened-dmg', appPath: null }
    }
    const entries = fs.readdirSync(mountPoint).filter((n) => n.endsWith('.app'))
    let appName = entries[0]
    for (const want of preferredAppNames) {
      const hit = entries.find(
        (n) => n === want || n.replace(/\.app$/, '') === String(want).replace(/\.app$/, ''),
      )
      if (hit) {
        appName = hit
        break
      }
    }
    if (!appName) {
      execFileSync('open', [dmgPath], { stdio: 'ignore' })
      return { mode: 'opened-dmg', appPath: null }
    }
    const src = path.join(mountPoint, appName)
    const destCandidates = [`/Applications/${appName}`, path.join(os.homedir(), 'Applications', appName)]
    let dest = destCandidates[0]
    try {
      fs.mkdirSync(path.dirname(dest), { recursive: true })
      if (fs.existsSync(dest)) execFileSync('rm', ['-rf', dest], { stdio: 'ignore' })
      execFileSync('cp', ['-R', src, dest], { stdio: 'ignore' })
    } catch {
      dest = destCandidates[1]
      fs.mkdirSync(path.dirname(dest), { recursive: true })
      if (fs.existsSync(dest)) execFileSync('rm', ['-rf', dest], { stdio: 'ignore' })
      execFileSync('cp', ['-R', src, dest], { stdio: 'ignore' })
    }
    try {
      execFileSync('xattr', ['-cr', dest], { stdio: 'ignore' })
    } catch {
      /* ignore */
    }
    return { mode: 'installed', appPath: dest }
  } finally {
    if (mountPoint) {
      try {
        execFileSync('hdiutil', ['detach', mountPoint, '-quiet'], { stdio: 'ignore' })
      } catch {
        /* ignore */
      }
    }
  }
}

function openInstalledApp(appPath) {
  if (!appPath) return false
  try {
    if (isMac()) {
      execFileSync('open', ['-a', appPath], { stdio: 'ignore' })
      return true
    }
    if (isWin()) {
      // MSIX / 商店包：用 AppsFolder AUMID 打开
      if (String(appPath).startsWith('msix:')) {
        const aumid = String(appPath).slice('msix:'.length)
        const child = spawn('explorer.exe', [`shell:AppsFolder\\${aumid}`], {
          detached: true,
          stdio: 'ignore',
          windowsHide: false,
        })
        child.on('error', () => {})
        child.unref()
        return true
      }
      const target = resolveLaunchableApp(appPath)
      if (!target) return false
      const child = spawn(target, [], { detached: true, stdio: 'ignore', windowsHide: false })
      // 必须监听 error，否则 ENOENT 会变成「主进程 Uncaught Exception」弹窗
      child.on('error', () => {})
      child.unref()
      return true
    }
    execFileSync('xdg-open', [appPath], { stdio: 'ignore' })
    return true
  } catch {
    try {
      require('electron').shell.openPath(appPath)
      return true
    } catch {
      return false
    }
  }
}

/** 后台拉起进程（不弹 Terminal），适合 Web UI 类 Agent。 */
function spawnDetached(bin, args = [], opts = {}) {
  if (!bin) return false
  try {
    const child = spawn(bin, args.map(String), {
      detached: true,
      stdio: 'ignore',
      env: { ...process.env, ...(opts.env || {}) },
      cwd: opts.cwd || os.homedir(),
      shell: isWin(),
      windowsHide: true,
    })
    child.on('error', () => {})
    child.unref()
    return true
  } catch {
    return false
  }
}

async function waitHttpOk(url, timeoutMs = 45000) {
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    throwIfCancelled()
    try {
      const ok = await new Promise((resolve) => {
        const lib = String(url).startsWith('https') ? https : http
        const req = lib.get(url, { timeout: 2000 }, (res) => {
          res.resume()
          resolve(res.statusCode && res.statusCode < 500)
        })
        req.on('error', () => resolve(false))
        req.on('timeout', () => {
          req.destroy()
          resolve(false)
        })
      })
      if (ok) return true
    } catch {
      /* retry */
    }
    await sleep(800)
  }
  return false
}

function dshHomeDir() {
  return process.env.DSH_HOME || path.join(os.homedir(), '.dsh')
}

/** 安装 @deepseek-ai/dsh（Node Web UI），返回可执行 bin。 */
async function ensureDeepSeekHarness(onProgress) {
  refreshUserPath()
  let bin = whichBin('dsh') || whichBin('dsh.cmd')
  if (bin) return { already: true, bin }

  await ensureNode(onProgress)
  if (typeof onProgress === 'function') onProgress({ message: '正在安装 DeepSeek Harness…' })
  const npm = runNpmGlobal('@deepseek-ai/dsh', onProgress, 'DeepSeek Harness')
  refreshUserPath()
  bin = whichBin('dsh') || whichBin('dsh.cmd')
  if (npm.status === 0 && bin) {
    ensureShellPathHint(path.dirname(bin))
    return { already: false, installed: true, bin, via: 'npm' }
  }
  // 全局装失败时仍可用 npx
  const npx = whichBin('npx') || whichBin('npx.cmd')
  if (npx) {
    return { already: false, installed: false, bin: npx, args: ['-y', '@deepseek-ai/dsh'], via: 'npx' }
  }
  const err = new Error((npm.stderr || 'DeepSeek Harness 安装失败').slice(0, 400))
  err.fallbackUrl = 'https://github.com/deepseek-ai/deepseek-harness'
  throw err
}

/** 在系统终端里拉起 CLI（Pi / Hermes / Claude Code 等），方便用户立刻开始聊。 */
function openCLIInTerminal(bin, args = []) {
  if (!bin) return false
  const cmd = [bin, ...args.map(String)].map((p) => (/\s/.test(p) ? JSON.stringify(p) : p)).join(' ')
  try {
    if (isMac()) {
      // Terminal.app 新开一窗执行；失败再试 iTerm
      const apple = `tell application "Terminal" to do script ${JSON.stringify(cmd)}`
      const r = spawnSync('osascript', ['-e', apple], { encoding: 'utf8', timeout: 15000 })
      if (r.status === 0) return true
      spawnSync('osascript', ['-e', `tell application "iTerm" to create window with default profile command ${JSON.stringify(cmd)}`], {
        encoding: 'utf8',
        timeout: 15000,
      })
      return true
    }
    if (isWin()) {
      const child = spawn('cmd.exe', ['/c', 'start', 'cmd', '/k', cmd], {
        detached: true,
        stdio: 'ignore',
        windowsHide: false,
        shell: false,
      })
      child.on('error', () => {})
      child.unref()
      return true
    }
    const term = whichBin('x-terminal-emulator') || whichBin('gnome-terminal') || whichBin('konsole') || whichBin('xterm')
    if (term) {
      const child = spawn(term, ['-e', 'bash', '-lc', cmd], { detached: true, stdio: 'ignore' })
      child.on('error', () => {})
      child.unref()
      return true
    }
  } catch {
    /* ignore */
  }
  return false
}

function openInstaller(filePath) {
  if (isMac()) {
    execFileSync('open', [filePath], { stdio: 'ignore' })
    return
  }
  if (isWin()) {
    const ext = path.extname(filePath).toLowerCase()
    if (ext === '.msi') {
      spawn('msiexec.exe', ['/i', filePath], { detached: true, stdio: 'ignore', windowsHide: false }).unref()
      return
    }
    spawn(filePath, [], { detached: true, stdio: 'ignore', windowsHide: false }).unref()
    return
  }
  execFileSync('xdg-open', [filePath], { stdio: 'ignore' })
}

/**
 * @param {{ label: string, ownerRepo?: string, predicates?: Function[], directUrl?: string, appNames?: string[], onProgress?: Function }} spec
 */
async function downloadAndInstall(spec) {
  const { label, ownerRepo, predicates, directUrl, appNames = [], onProgress } = spec
  const report = (msg, pct) => {
    throwIfCancelled()
    if (typeof onProgress === 'function') onProgress({ message: msg, percent: pct })
  }

  throwIfCancelled()
  await yieldEventLoop()
  let url = directUrl
  let filename = path.basename((directUrl || '').split('?')[0] || `${label}.bin`)
  if (!url) {
    report(`正在查找 ${label} 安装包…`)
    const asset = await resolveGitHubAsset(ownerRepo, predicates)
    throwIfCancelled()
    await yieldEventLoop()
    url = asset.url
    filename = asset.name
  }
  // redirect / latest 这类 URL 没有真实扩展名，按平台给安装包名
  const urlPath = (url || '').split('?')[0]
  if (/\.msix$/i.test(urlPath)) filename = path.basename(urlPath) || `${label.replace(/\s+/g, '-')}.msix`
  else if (!/\.(dmg|pkg|exe|msi|zip|AppImage|msix)$/i.test(filename) || /redirect|latest|download/i.test(filename)) {
    if (isMac()) filename = `${label.replace(/\s+/g, '-')}.dmg`
    else if (isWin()) filename = `${label.replace(/\s+/g, '-')}-Setup.exe`
    else filename = `${label.replace(/\s+/g, '-')}.bin`
  }

  const dest = path.join(downloadDir(), filename)
  const expectLen = await headContentLength(url)
  const cached = fs.existsSync(dest) ? fs.statSync(dest).size : 0
  const cacheOk = cached > 1024 * 1024 && (expectLen <= 0 || cached === expectLen)
  if (!cacheOk) {
    try {
      if (fs.existsSync(dest)) fs.unlinkSync(dest)
    } catch {
      /* ignore */
    }
    report(`正在下载 ${label}…`, 0)
    await downloadFile(url, dest, (pct) => report(`正在下载 ${label}… ${pct}%`, pct))
    const got = fs.existsSync(dest) ? fs.statSync(dest).size : 0
    if (expectLen > 0 && got !== expectLen) {
      throw new Error(`下载不完整：期望 ${expectLen} 字节，实际 ${got}`)
    }
    if (got < 256 * 1024) throw new Error(`下载异常过小：${got} 字节`)
  } else {
    report(`已缓存 ${label} 安装包，开始安装…`)
  }

  if (isMac() && /\.dmg$/i.test(dest)) {
    report(`正在安装 ${label}…`, 100)
    const r = installMacDmg(dest, appNames)
    if (r.appPath) openInstalledApp(r.appPath)
    return { ok: true, file: dest, appPath: r.appPath, mode: r.mode }
  }

  // Windows MSIX：静默安装商店包（Codex 图形版）
  if (isWin() && /\.msix$/i.test(dest)) {
    const ok = installChatGPTWinMsix(dest, onProgress)
    const appPath = ok ? CHATGPT_WIN_APPPATH : null
    if (appPath) openInstalledApp(appPath)
    return { ok, file: dest, appPath, mode: ok ? 'msix' : 'msix-failed' }
  }

  // Windows / 其它：拉起安装包（msi/exe/zip）
  report(`正在打开 ${label} 安装程序…`, 100)
  try {
    openInstaller(dest)
  } catch {
    await require('electron').shell.openPath(dest)
  }
  return { ok: true, file: dest, appPath: null, mode: 'opened-installer' }
}

/** 按平台挑选 GitHub release 资源 */
function releasePredicates(kinds) {
  const arch = archTag()
  const list = []
  if (kinds.includes('cherry')) {
    if (isMac()) {
      list.push((n) => /mac/i.test(n) && new RegExp(arch === 'arm64' ? 'arm64' : 'x64', 'i').test(n) && /\.dmg$/i.test(n) && !/blockmap/i.test(n))
      list.push((n) => /mac/i.test(n) && /\.dmg$/i.test(n) && !/blockmap/i.test(n))
    } else if (isWin()) {
      list.push((n) => /win/i.test(n) && new RegExp(arch === 'arm64' ? 'arm64' : 'x64', 'i').test(n) && /setup\.exe$/i.test(n) && !/blockmap/i.test(n))
      list.push((n) => /win/i.test(n) && /x64/i.test(n) && /setup\.exe$/i.test(n) && !/blockmap/i.test(n))
      list.push((n) => /win/i.test(n) && /setup\.exe$/i.test(n) && !/blockmap/i.test(n))
    }
  }
  if (kinds.includes('cc-switch')) {
    if (isMac()) {
      list.push((n) => /macOS/i.test(n) && /\.dmg$/i.test(n))
      list.push((n) => /macOS/i.test(n) && /\.zip$/i.test(n) && !/\.sig$/i.test(n))
    } else if (isWin()) {
      if (arch === 'arm64') {
        list.push((n) => /Windows-arm64\.msi$/i.test(n))
        list.push((n) => /Windows-arm64/i.test(n) && /\.msi$/i.test(n))
      }
      list.push((n) => /Windows\.msi$/i.test(n) && !/arm64/i.test(n))
      list.push((n) => /Windows/i.test(n) && /\.msi$/i.test(n) && !/arm64/i.test(n))
      list.push((n) => /Windows/i.test(n) && /\.msi$/i.test(n))
    }
  }
  if (kinds.includes('paseo')) {
    if (isMac()) {
      list.push((n) => new RegExp(arch === 'arm64' ? 'arm64' : 'x64', 'i').test(n) && /\.dmg$/i.test(n) && !/blockmap/i.test(n))
      list.push((n) => /\.dmg$/i.test(n) && !/blockmap/i.test(n))
    } else if (isWin()) {
      list.push((n) => /Setup/i.test(n) && new RegExp(arch === 'arm64' ? 'arm64' : 'x64', 'i').test(n) && /\.exe$/i.test(n) && !/blockmap/i.test(n))
      list.push((n) => /Setup/i.test(n) && /\.exe$/i.test(n) && !/blockmap/i.test(n))
    } else {
      list.push((n) => /\.AppImage$/i.test(n))
    }
  }
  return list
}

async function brewInstallCask(cask, label, onProgress) {
  const brew = whichBin('brew')
  if (!isMac() || !brew) return null
  if (typeof onProgress === 'function') onProgress({ message: `正在用 Homebrew 安装 ${label}…` })
  const r = spawnSync(brew, ['install', '--cask', cask], {
    encoding: 'utf8',
    timeout: 900000,
    env: process.env,
  })
  refreshUserPath()
  return r
}

async function ensurePaseo(onProgress) {
  const names = isWin()
    ? ['Paseo', path.join('Paseo', 'Paseo.exe')]
    : ['Paseo.app']
  const hit = findInstalledApp(names)
  if (hit) return { already: true, appPath: hit }

  const brew = await brewInstallCask('paseo', 'Paseo', onProgress)
  if (brew && brew.status === 0) {
    const after = findInstalledApp(names)
    if (after) return { already: false, installed: true, appPath: after, via: 'brew' }
  }

  if (!isMac() && !isWin() && process.platform !== 'linux') {
    return { already: false, fallbackUrl: 'https://paseo.sh/download' }
  }
  try {
    const r = await downloadAndInstall({
      label: 'Paseo',
      ownerRepo: 'getpaseo/paseo',
      predicates: releasePredicates(['paseo']),
      appNames: isMac() ? ['Paseo.app'] : [],
      onProgress,
    })
    return { already: false, ...r }
  } catch (err) {
    const e = new Error(err instanceof Error ? err.message : String(err))
    e.fallbackUrl = 'https://paseo.sh/download'
    throw e
  }
}

function zcodeDirectUrl() {
  const arch = archTag()
  // 版本号随官网更新；失败时仍可走 brew / 说明页
  const ver = '3.19.1'
  const base = `https://cdn-zcode.z.ai/zcode/electron/releases/${ver}`
  if (isMac()) {
    return arch === 'arm64'
      ? `${base}/macos-arm64/ZCode-${ver}-mac-arm64.dmg`
      : `${base}/macos-x64/ZCode-${ver}-mac-x64.dmg`
  }
  if (isWin()) {
    return arch === 'arm64'
      ? `${base}/windows-arm64/ZCode-${ver}-win-arm64.exe`
      : `${base}/windows-x64/ZCode-${ver}-win-x64.exe`
  }
  return `${base}/linux-x64/ZCode-${ver}-linux-x64.AppImage`
}

async function ensureZCode(onProgress) {
  const names = isWin()
    ? ['ZCode', path.join('ZCode', 'ZCode.exe')]
    : ['ZCode.app']
  const hit = findInstalledApp(names)
  if (hit) return { already: true, appPath: hit }

  const brew = await brewInstallCask('zcode', 'ZCode', onProgress)
  if (brew && brew.status === 0) {
    const after = findInstalledApp(names)
    if (after) return { already: false, installed: true, appPath: after, via: 'brew' }
  }

  const url = zcodeDirectUrl()
  try {
    const r = await downloadAndInstall({
      label: 'ZCode',
      directUrl: url,
      appNames: isMac() ? ['ZCode.app'] : [],
      onProgress,
    })
    return { already: false, ...r }
  } catch (err) {
    const e = new Error(err instanceof Error ? err.message : String(err))
    e.fallbackUrl = 'https://zcode.z.ai/en/docs/install'
    throw e
  }
}

async function ensureCCSwitch(onProgress) {
  const names = isWin()
    ? ['CC Switch', 'CC-Switch', 'cc-switch', path.join('CC Switch', 'CC Switch.exe')]
    : ['CC Switch.app', 'CCSwitch.app']
  const hit = findInstalledApp(names)
  if (hit) return { already: true, appPath: hit }

  if (!isMac() && !isWin()) {
    return { already: false, fallbackUrl: 'https://github.com/farion1231/cc-switch/releases/latest' }
  }
  const r = await downloadAndInstall({
    label: 'CC Switch',
    ownerRepo: 'farion1231/cc-switch',
    predicates: releasePredicates(['cc-switch']),
    appNames: isMac() ? ['CC Switch.app', 'CCSwitch.app'] : [],
    onProgress,
  })
  return { already: false, ...r }
}

async function ensureCherry(onProgress) {
  const names = isWin()
    ? ['Cherry Studio', 'cherry-studio', path.join('Cherry Studio', 'Cherry Studio.exe')]
    : ['Cherry Studio.app', 'CherryStudio.app']
  const hit = findInstalledApp(names)
  if (hit) return { already: true, appPath: hit }

  if (!isMac() && !isWin()) {
    return { already: false, fallbackUrl: 'https://github.com/CherryHQ/cherry-studio/releases/latest' }
  }
  const r = await downloadAndInstall({
    label: 'Cherry Studio',
    ownerRepo: 'CherryHQ/cherry-studio',
    predicates: releasePredicates(['cherry']),
    appNames: isMac() ? ['Cherry Studio.app', 'CherryStudio.app'] : [],
    onProgress,
  })
  return { already: false, ...r }
}

async function ensureChatbox(onProgress) {
  const names = isWin()
    ? ['Chatbox', 'chatbox', path.join('Chatbox', 'Chatbox.exe')]
    : ['Chatbox.app', 'chatbox.app']
  const hit = findInstalledApp(names)
  if (hit) return { already: true, appPath: hit }

  let ver = '1.24.4'
  try {
    const rel = await githubJSON('/repos/chatboxai/chatbox/releases/latest')
    ver = String(rel.tag_name || ver).replace(/^v/, '')
  } catch {
    /* default */
  }

  let url
  if (isMac()) {
    const arch = archTag() === 'arm64' ? 'arm64' : 'x64'
    // Intel 包名为 Chatbox-{ver}.dmg，arm64 为 Chatbox-{ver}-arm64.dmg
    url =
      arch === 'arm64'
        ? `https://download.chatboxai.app/releases/Chatbox-${ver}-arm64.dmg`
        : `https://download.chatboxai.app/releases/Chatbox-${ver}.dmg`
  } else if (isWin()) {
    url = `https://download.chatboxai.app/releases/Chatbox-${ver}-Setup.exe`
  } else {
    return { already: false, fallbackUrl: 'https://chatboxai.app/' }
  }

  const r = await downloadAndInstall({
    label: 'Chatbox',
    directUrl: url,
    appNames: isMac() ? ['Chatbox.app', 'chatbox.app'] : [],
    onProgress,
  })
  return { already: false, ...r }
}

async function installClaudeOfficial(onProgress) {
  if (isWin()) return { ok: false, error: 'official installer 不支持 Windows' }
  if (typeof onProgress === 'function') onProgress({ message: '正在用官方安装器安装 Claude Code…' })
  // 官方脚本装到 $HOME/.local/bin，不需 sudo / Node
  const r = spawnSync('bash', ['-lc', 'curl -fsSL https://claude.ai/install.sh | bash'], {
    encoding: 'utf8',
    timeout: 600000,
    env: process.env,
  })
  refreshUserPath()
  ensureShellPathHint(path.join(os.homedir(), '.local', 'bin'))
  const bin = whichBin('claude')
  if (r.status === 0 && bin) return { ok: true, bin, via: 'official' }
  return {
    ok: false,
    error: (r.stderr || r.stdout || `exit ${r.status}`).slice(0, 400),
  }
}

async function ensureClaudeDesktop(onProgress) {
  const names = isWin()
    ? ['Claude', path.join('Claude', 'Claude.exe')]
    : ['Claude.app']
  const hit = findInstalledApp(names)
  if (hit) return { already: true, appPath: hit }

  // 国内/部分地区 claude.ai 直链会 region_unavailable(403)；先试 brew
  const brew = await brewInstallCask('claude', 'Claude Desktop', onProgress)
  if (brew && brew.status === 0) {
    const afterBrew = findInstalledApp(names)
    if (afterBrew) return { already: false, installed: true, appPath: afterBrew, via: 'brew' }
  }

  const arch = archTag() === 'arm64' ? 'arm64' : 'x64'
  let url
  if (isMac()) {
    url = `https://claude.ai/api/desktop/darwin/${arch}/offline/latest/redirect`
  } else if (isWin()) {
    url = `https://claude.ai/api/desktop/win32/${arch}/exe/latest/redirect`
  } else {
    return { already: false, fallbackUrl: 'https://claude.ai/download' }
  }

  try {
    const r = await downloadAndInstall({
      label: 'Claude Desktop',
      directUrl: url,
      appNames: isMac() ? ['Claude.app'] : [],
      onProgress,
    })
    await sleep(2500)
    const after = findInstalledApp(names)
    return { already: false, ...r, appPath: after || r.appPath || null }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    const e = new Error(
      /403|region/i.test(msg)
        ? 'Claude Desktop 当前地区无法直链下载，请点「打开安装说明」或使用代理后重试'
        : msg,
    )
    e.fallbackUrl = 'https://claude.ai/download'
    throw e
  }
}

async function ensurePi(onProgress) {
  refreshUserPath()
  const existing = whichBin('pi') || whichBin('pi.cmd')
  if (existing) return { already: true, bin: existing }

  if (!isWin()) {
    if (typeof onProgress === 'function') onProgress({ message: '正在用官方安装器安装 Pi…' })
    const off = spawnSync('bash', ['-lc', 'curl -fsSL https://pi.dev/install.sh | sh'], {
      encoding: 'utf8',
      timeout: 600000,
      env: process.env,
    })
    refreshUserPath()
    ensureShellPathHint(path.join(os.homedir(), '.local', 'bin'))
    const bin = whichBin('pi')
    if (off.status === 0 && bin) return { already: false, installed: true, bin, via: 'official' }
  } else {
    if (typeof onProgress === 'function') onProgress({ message: '正在用官方安装器安装 Pi…' })
    const off = spawnSync(
      'powershell.exe',
      ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', 'irm https://pi.dev/install.ps1 | iex'],
      { encoding: 'utf8', timeout: 600000, env: process.env },
    )
    refreshUserPath()
    const bin = whichBin('pi') || whichBin('pi.cmd')
    if (off.status === 0 && bin) return { already: false, installed: true, bin, via: 'official' }
  }

  await ensureNode(onProgress)
  const npm = runNpmGlobal('@earendil-works/pi-coding-agent', onProgress, 'Pi')
  // package may expose `pi` via bin
  const args = ['install', '-g', '--ignore-scripts', '@earendil-works/pi-coding-agent', `--prefix=${npmPrefixDir()}`]
  const npmCmd = whichBin('npm.cmd') || whichBin('npm') || (isWin() ? 'npm.cmd' : 'npm')
  const npm2 = spawnSync(npmCmd, args, {
    encoding: 'utf8',
    timeout: 300000,
    shell: isWin(),
    env: { ...process.env, npm_config_prefix: npmPrefixDir() },
  })
  refreshUserPath()
  const bin = whichBin('pi') || whichBin('pi.cmd')
  if ((npm.status === 0 || npm2.status === 0) && bin) {
    ensureShellPathHint(path.dirname(bin))
    return { already: false, installed: true, bin, via: 'npm' }
  }
  const err = new Error((npm2.stderr || npm.stderr || 'Pi 安装失败').slice(0, 400))
  err.fallbackUrl = 'https://pi.dev'
  throw err
}

function findHermesBin() {
  refreshUserPath()
  const hit = whichBin('hermes') || whichBin('hermes.cmd') || whichBin('hermes.exe')
  if (hit) return hit
  const home = hermesHomeDir()
  const candidates = isWin()
    ? [path.join(home, 'bin', 'hermes.exe'), path.join(home, 'bin', 'hermes.cmd'), path.join(home, 'bin', 'hermes')]
    : [path.join(os.homedir(), '.local', 'bin', 'hermes'), path.join(home, 'bin', 'hermes')]
  return candidates.find((p) => {
    try {
      return fs.existsSync(p)
    } catch {
      return false
    }
  })
}

/** 官方安装器静默装 Hermes CLI（不要打开 GitHub 源码页）。 */
async function ensureHermes(onProgress) {
  const existing = findHermesBin()
  if (existing) {
    ensureShellPathHint(path.dirname(existing))
    return { already: true, bin: existing }
  }

  const fallbackUrl = 'https://hermes-agent.nousresearch.com/docs/getting-started/installation'
  if (typeof onProgress === 'function') onProgress({ message: '正在安装 Hermes Agent（官方安装器，可能需要几分钟）…' })

  let status = -1
  let errText = ''
  if (!isWin()) {
    // --non-interactive：不弹交互向导；跳过浏览器/电脑操控可选组件，加快一键配置
    const off = spawnSync(
      'bash',
      [
        '-lc',
        'curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash -s -- --non-interactive --skip-browser --skip-computer-use',
      ],
      { encoding: 'utf8', timeout: 900000, env: process.env },
    )
    status = off.status
    errText = (off.stderr || off.stdout || '').slice(0, 500)
  } else {
    const ps1 = path.join(downloadDir(), 'hermes-install.ps1')
    try {
      await downloadFile('https://hermes-agent.nousresearch.com/install.ps1', ps1, onProgress)
      const off = spawnSync(
        'powershell.exe',
        [
          '-NoProfile',
          '-ExecutionPolicy',
          'Bypass',
          '-File',
          ps1,
          '-NonInteractive',
          '-SkipBrowser',
          '-SkipComputerUse',
        ],
        { encoding: 'utf8', timeout: 900000, env: process.env },
      )
      status = off.status
      errText = (off.stderr || off.stdout || '').slice(0, 500)
    } catch (err) {
      const e = new Error(err instanceof Error ? err.message : String(err))
      e.fallbackUrl = fallbackUrl
      throw e
    }
  }

  refreshUserPath()
  const bin = findHermesBin()
  if (status === 0 && bin) {
    ensureShellPathHint(path.dirname(bin))
    return { already: false, installed: true, bin, via: 'official' }
  }
  // 有的脚本非 0 但仍装好了；再扫一遍
  if (bin) {
    ensureShellPathHint(path.dirname(bin))
    return { already: false, installed: true, bin, via: 'official', warn: errText || `exit ${status}` }
  }
  const err = new Error(errText || `Hermes 安装失败（exit ${status}）`)
  err.fallbackUrl = fallbackUrl
  throw err
}

async function ensureOpenClawCLI(onProgress) {
  refreshUserPath()
  const existing = whichBin('openclaw') || whichBin('openclaw.cmd')
  if (existing) return { already: true, bin: existing }
  await ensureNode(onProgress)
  const npm = runNpmGlobal('openclaw', onProgress, 'OpenClaw')
  const bin = whichBin('openclaw') || whichBin('openclaw.cmd')
  if (npm.status === 0 && bin) {
    ensureShellPathHint(path.dirname(bin))
    return { already: false, installed: true, bin, via: 'npm' }
  }
  // 包名可能是 @openclaw/cli 等，再试常见名
  for (const pkg of ['@openclaw/cli', 'openclaw-cli']) {
    const r = runNpmGlobal(pkg, onProgress, pkg)
    const b = whichBin('openclaw') || whichBin('openclaw.cmd')
    if (r.status === 0 && b) return { already: false, installed: true, bin: b, via: 'npm' }
  }
  const err = new Error((npm.stderr || 'OpenClaw 安装失败').slice(0, 400))
  err.fallbackUrl = 'https://github.com/openclaw/openclaw'
  throw err
}

async function ensureObsidian(onProgress) {
  const names = isWin()
    ? ['Obsidian', path.join('Obsidian', 'Obsidian.exe')]
    : ['Obsidian.app']
  const hit = findInstalledApp(names)
  if (hit) return { already: true, appPath: hit }

  // GitHub releases：挑当前平台 dmg/exe
  const predicates = []
  if (isMac()) {
    predicates.push((n) => /\.dmg$/i.test(n) && !/legacy/i.test(n))
  } else if (isWin()) {
    predicates.push((n) => /\.exe$/i.test(n) && !/legacy/i.test(n))
  }
  if (!predicates.length) return { already: false, fallbackUrl: 'https://obsidian.md/download' }

  const r = await downloadAndInstall({
    label: 'Obsidian',
    ownerRepo: 'obsidianmd/obsidian-releases',
    predicates,
    appNames: isMac() ? ['Obsidian.app'] : [],
    onProgress,
  })
  await sleep(2500)
  const after = findInstalledApp(names)
  return { already: false, ...r, appPath: after || r.appPath || null }
}

async function ensureClaudeCode(onProgress) {
  refreshUserPath()
  const existing = whichBin('claude') || whichBin('claude.cmd')
  if (existing) {
    ensureShellPathHint(path.dirname(existing))
    return { already: true, bin: existing }
  }

  const errors = []

  // 1) macOS / Linux：官方安装器（自带二进制，零基础首选）
  if (!isWin()) {
    try {
      const off = await installClaudeOfficial(onProgress)
      if (off.ok) return { already: false, installed: true, bin: off.bin, via: 'official' }
      errors.push(`official: ${off.error}`)
    } catch (err) {
      errors.push(`official: ${err instanceof Error ? err.message : String(err)}`)
    }
  }

  // 2) 确保 Node（便携包，无需管理员）→ npm 装到用户目录
  try {
    await ensureNode(onProgress)
  } catch (err) {
    errors.push(`node: ${err instanceof Error ? err.message : String(err)}`)
  }

  const npm = runNpmGlobal('@anthropic-ai/claude-code', onProgress, 'Claude Code')
  const binAfterNpm = whichBin('claude') || whichBin('claude.cmd')
  if (npm.status === 0 && binAfterNpm) {
    ensureShellPathHint(path.dirname(binAfterNpm))
    return { already: false, installed: true, bin: binAfterNpm, via: 'npm' }
  }
  errors.push(`npm: ${(npm.stderr || npm.stdout || `exit ${npm.status}`).slice(0, 300)}`)

  // 3) Windows：尝试 winget（部分机器可用）
  if (isWin()) {
    if (typeof onProgress === 'function') onProgress({ message: '正在尝试 winget 安装 Claude Code…' })
    const wg = spawnSync(
      'winget',
      ['install', '-e', '--id', 'Anthropic.ClaudeCode', '--accept-package-agreements', '--accept-source-agreements'],
      { encoding: 'utf8', timeout: 600000, shell: true },
    )
    refreshUserPath()
    const bin = whichBin('claude') || whichBin('claude.cmd')
    if (wg.status === 0 && bin) {
      return { already: false, installed: true, bin, via: 'winget' }
    }
    errors.push(`winget: ${(wg.stderr || wg.stdout || `exit ${wg.status}`).slice(0, 200)}`)
  }

  // 仍失败：抛错，让上层明确提示（不再假装成功只开文档页）
  const msg = errors.filter(Boolean).join(' | ') || 'Claude Code 安装失败'
  const err = new Error(msg)
  err.fallbackUrl = 'https://docs.anthropic.com/en/docs/claude-code/overview'
  throw err
}

async function ensureCodexCLI(onProgress) {
  refreshUserPath()
  const existing = whichBin('codex') || whichBin('codex.cmd')
  if (existing) return { already: true, bin: existing }

  const errors = []
  try {
    await ensureNode(onProgress)
  } catch (err) {
    errors.push(`node: ${err instanceof Error ? err.message : String(err)}`)
  }

  const npm = runNpmGlobal('@openai/codex', onProgress, 'Codex CLI')
  const binNpm = whichBin('codex') || whichBin('codex.cmd')
  if (npm.status === 0 && binNpm) {
    ensureShellPathHint(path.dirname(binNpm))
    return { already: false, installed: true, bin: binNpm, via: 'npm' }
  }
  errors.push(`npm: ${(npm.stderr || npm.stdout || `exit ${npm.status}`).slice(0, 300)}`)

  if (isMac()) {
    if (typeof onProgress === 'function') onProgress({ message: '换一种方式安装 Codex…' })
    const brew = spawnSync('brew', ['install', '--cask', 'codex'], {
      encoding: 'utf8',
      timeout: 300000,
    })
    refreshUserPath()
    if (brew.status === 0 && whichBin('codex')) {
      return { already: false, installed: true, bin: whichBin('codex'), via: 'brew' }
    }
    errors.push(`brew: ${(brew.stderr || brew.stdout || '').slice(0, 200)}`)
  }

  const err = new Error(errors.filter(Boolean).join(' | ') || 'Codex CLI 安装失败')
  err.fallbackUrl = 'https://github.com/openai/codex'
  throw err
}

/** macOS：oaistatic dmg；Windows：官方最新 MSIX（含 Codex 图形界面） */
function chatGPTDesktopUrl() {
  if (isMac()) {
    return archTag() === 'arm64'
      ? 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT.dmg'
      : 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-latest-x64.dmg'
  }
  if (isWin()) {
    return archTag() === 'arm64'
      ? 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-arm64.msix'
      : 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-x64.msix'
  }
  return 'https://chatgpt.com/download/'
}

/** Windows 商店包 AUMID（winget / MSIX 安装后用于打开） */
const CHATGPT_WIN_AUMID = 'OpenAI.Codex_2p2nqsd0c76g0!App'
const CHATGPT_WIN_APPPATH = `msix:${CHATGPT_WIN_AUMID}`

function isChatGPTWinPackageInstalled() {
  if (!isWin()) return false
  try {
    const r = spawnSync(
      'powershell.exe',
      [
        '-NoProfile',
        '-Command',
        "if (Get-AppxPackage -Name 'OpenAI.Codex' -ErrorAction SilentlyContinue) { '1' } else { '0' }",
      ],
      { encoding: 'utf8', timeout: 20000, windowsHide: true },
    )
    return String(r.stdout || '').trim() === '1'
  } catch {
    return false
  }
}

function installChatGPTWinMsix(filePath, onProgress) {
  const report = (msg, pct) => {
    if (typeof onProgress === 'function') onProgress({ message: msg, percent: pct })
  }
  report('正在安装图形版 Codex…', 95)
  const r = spawnSync(
    'powershell.exe',
    ['-NoProfile', '-Command', `Add-AppxPackage -Path ${JSON.stringify(filePath)} -ErrorAction Stop`],
    { encoding: 'utf8', timeout: 300000, windowsHide: true },
  )
  if (r.status !== 0) {
    const detail = String(r.stderr || r.stdout || `exit ${r.status}`).slice(0, 400)
    const err = new Error(`图形版安装失败：${detail}`)
    err.fallbackUrl = 'https://chatgpt.com/download/'
    throw err
  }
  return isChatGPTWinPackageInstalled()
}

function tryWingetInstallChatGPT(onProgress) {
  if (!isWin()) return false
  const winget = whichBin('winget') || whichBin('winget.exe')
  if (!winget) return false
  if (typeof onProgress === 'function') onProgress({ message: '正在安装图形版 Codex…' })
  const r = spawnSync(
    winget,
    [
      'install',
      '--id',
      'OpenAI.ChatGPT',
      '--exact',
      '--accept-package-agreements',
      '--accept-source-agreements',
      '--disable-interactivity',
    ],
    { encoding: 'utf8', timeout: 600000, windowsHide: true },
  )
  return r.status === 0 && isChatGPTWinPackageInstalled()
}

async function ensureChatGPTDesktop(onProgress) {
  const names = isWin()
    ? [path.join('ChatGPT', 'ChatGPT.exe'), 'ChatGPT', path.join('Programs', 'ChatGPT', 'ChatGPT.exe')]
    : ['ChatGPT.app', 'Codex.app', 'ChatGPT Codex.app']
  const hit = findInstalledApp(names)
  if (hit) return { already: true, appPath: hit }
  if (isWin() && isChatGPTWinPackageInstalled()) {
    return { already: true, appPath: CHATGPT_WIN_APPPATH }
  }

  const url = chatGPTDesktopUrl()
  if (!url) return { already: false, fallbackUrl: 'https://chatgpt.com/download/' }

  // Windows：优先 winget，否则下官方 MSIX 静默安装（约 700MB+）
  if (isWin()) {
    try {
      if (tryWingetInstallChatGPT(onProgress)) {
        return { already: false, installed: true, appPath: CHATGPT_WIN_APPPATH, via: 'winget' }
      }
    } catch {
      /* 再试 MSIX */
    }
    if (typeof onProgress === 'function') {
      onProgress({ message: '正在下载图形版 Codex（约 700MB，请稍候）…' })
    }
    const r = await downloadAndInstall({
      label: 'Codex 图形版',
      directUrl: url,
      appNames: [],
      onProgress,
    })
    if (r?.appPath) return { already: false, ...r }
    if (isChatGPTWinPackageInstalled()) {
      return { already: false, installed: true, appPath: CHATGPT_WIN_APPPATH, file: r?.file, via: 'msix' }
    }
    return {
      already: false,
      fallbackUrl: 'https://chatgpt.com/download/',
      mode: 'need-manual',
      error: r?.error || '图形版未装上',
    }
  }

  if (!/\.(dmg|exe|msi|zip|msix)$/i.test(url.split('?')[0])) {
    return { already: false, fallbackUrl: url, mode: 'need-manual' }
  }

  if (typeof onProgress === 'function') {
    onProgress({ message: '正在下载图形版 Codex（较大，请稍候）…' })
  }

  const r = await downloadAndInstall({
    label: 'Codex 图形版',
    directUrl: url,
    appNames: isMac() ? ['ChatGPT.app', 'Codex.app'] : [],
    onProgress,
  })
  return { already: false, ...r }
}

async function ensureCodex(onProgress) {
  let cli
  try {
    cli = await ensureCodexCLI(onProgress)
  } catch (err) {
    cli = {
      error: err instanceof Error ? err.message : String(err),
      fallbackUrl: err?.fallbackUrl || 'https://github.com/openai/codex',
    }
  }
  let desktop = { skipped: true }
  try {
    desktop = await ensureChatGPTDesktop(onProgress)
  } catch (err) {
    desktop = {
      error: err instanceof Error ? err.message : String(err),
      fallbackUrl: 'https://chatgpt.com/download/',
    }
  }
  return { cli, desktop }
}

module.exports = {
  archTag,
  platformKey,
  downloadAndInstall,
  ensureNode,
  ensureCCSwitch,
  ensureCherry,
  ensureChatbox,
  ensureClaudeCode,
  ensureClaudeDesktop,
  ensurePi,
  ensureHermes,
  ensureOpenClawCLI,
  ensureObsidian,
  hermesHomeDir,
  ensureCodex,
  ensureCodexCLI,
  ensureChatGPTDesktop,
  isChatGPTWinPackageInstalled,
  CHATGPT_WIN_APPPATH,
  ensureDeepSeekHarness,
  ensurePaseo,
  ensureZCode,
  dshHomeDir,
  chatGPTDesktopUrl,
  chatGPTDesktopDmgUrl: chatGPTDesktopUrl, // 兼容旧名
  openInstalledApp,
  openCLIInTerminal,
  spawnDetached,
  waitHttpOk,
  whichBin,
  findInstalledApp,
  appLookupPaths,
  sleep,
  yieldEventLoop,
  requestCancel,
  resetCancel,
  throwIfCancelled,
  isCancelledError,
  runNpmGlobal,
  refreshUserPath,
  npmPrefixDir,
  ensureShellPathHint,
  brewInstallCask,
}
