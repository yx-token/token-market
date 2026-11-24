/**
 * 一键使用：首页弹窗选客户端后，自动识别系统、拉起下载并导入配置。
 * 本机「闲猫助手」装好后可通过 xianmao:// 协议接管全自动安装；未装时走网页半自动降级。
 * 客户端清单对齐 https://flux-code.cc/aux/client-docs ，并保留 Cherry / Chatbox。
 */
import { detectSetupPlatform, type SetupPlatform } from '@/utils/clientSetupCommand'
import {
  buildDirectImportURL,
  buildConfigFile,
  type ClientImportKey,
  type ClientImportTargetId,
  type DirectImportTargetId,
} from '@/utils/clientImport'

export type OneClickOS = 'macos' | 'windows' | 'linux'

export type OneClickClientId =
  | 'codex'
  | 'claude-code'
  | 'claude-desktop'
  | 'pi'
  | 'hermes'
  | 'openclaw'
  | 'paseo'
  | 'zcode'
  | 'deepseek-harness'
  | 'obsidian'
  | 'cherry-studio'
  | 'chatbox'

export type OneClickPhase =
  | 'pick'
  | 'working'
  | 'done'
  | 'need-key'
  | 'error'

export interface OneClickDownload {
  id: string
  label: string
  /** 直接文件或安装说明页 */
  url: string
  /** 是否优先自动打开（下载/安装页） */
  auto: boolean
}

export interface OneClickClient {
  id: OneClickClientId
  name: string
  blurb: string
  icon: string
  /** 对应 clientImport 目标 */
  importTargetId: ClientImportTargetId
  downloads: Record<OneClickOS, OneClickDownload[]>
}

export const ONE_CLICK_HELPER_PROTOCOL = 'xianmao'
export const ONE_CLICK_PROVIDER_NAME = '闲猫'

/** 第一次未选过时的默认客户端（非技术用户最友好）。 */
export const ONE_CLICK_DEFAULT_CLIENT: OneClickClientId = 'cherry-studio'
const PREFERRED_CLIENT_KEY = 'xm-one-click-preferred'

export function readPreferredClient(): OneClickClientId {
  try {
    const v = localStorage.getItem(PREFERRED_CLIENT_KEY)
    if (v && ONE_CLICK_CLIENTS.some((c) => c.id === v)) return v as OneClickClientId
  } catch {
    /* ignore */
  }
  return ONE_CLICK_DEFAULT_CLIENT
}

export function writePreferredClient(id: OneClickClientId): void {
  try {
    localStorage.setItem(PREFERRED_CLIENT_KEY, id)
  } catch {
    /* ignore */
  }
}

const allOS = (items: OneClickDownload[]): Record<OneClickOS, OneClickDownload[]> => ({
  macos: items,
  windows: items,
  linux: items,
})

/** 安装包/说明页清单；一键路径不依赖 CC Switch，直接装对应 Agent 并写配置。 */
export const ONE_CLICK_CLIENTS: readonly OneClickClient[] = [
  {
    id: 'codex',
    name: 'Codex',
    blurb: '写代码、改项目；助手直接装 Codex 并写入配置',
    icon: 'CX',
    importTargetId: 'codex',
    downloads: {
      macos: [
        {
          id: 'codex-desktop',
          label: 'ChatGPT（含 Codex）',
          url: 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT.dmg',
          auto: true,
        },
      ],
      windows: [
        { id: 'codex-desktop', label: 'ChatGPT（含 Codex）', url: 'https://chatgpt.com/download/', auto: true },
      ],
      linux: [{ id: 'codex', label: 'Codex CLI', url: 'https://github.com/openai/codex', auto: true }],
    },
  },
  {
    id: 'claude-code',
    name: 'Claude Code',
    blurb: '终端里用 Claude 写代码；助手自动安装、写配置并打开',
    icon: 'CC',
    importTargetId: 'claude-code',
    downloads: allOS([
      { id: 'claude', label: 'Claude Code 说明', url: 'https://docs.anthropic.com/en/docs/claude-code/overview', auto: true },
    ]),
  },
  {
    id: 'claude-desktop',
    name: 'Claude Desktop',
    blurb: '官方桌面端；助手自动安装并写入可用配置后打开',
    icon: 'CD',
    importTargetId: 'claude-desktop',
    downloads: allOS([{ id: 'claude-desktop', label: 'Claude Desktop', url: 'https://claude.ai/download', auto: true }]),
  },
  {
    id: 'pi',
    name: 'Pi',
    blurb: '终端 Agent，助手直接安装、写配置并打开',
    icon: 'PI',
    importTargetId: 'pi',
    downloads: allOS([{ id: 'pi', label: 'Pi 说明', url: 'https://github.com/badlogic/pi-mono', auto: true }]),
  },
  {
    id: 'hermes',
    name: 'Hermes',
    blurb: 'Hermes Agent：助手官方安装、写配置并打开',
    icon: 'HA',
    importTargetId: 'hermes',
    downloads: allOS([
      {
        id: 'hermes',
        label: 'Hermes 安装说明',
        url: 'https://hermes-agent.nousresearch.com/docs/getting-started/installation',
        auto: false,
      },
    ]),
  },
  {
    id: 'openclaw',
    name: 'OpenClaw',
    blurb: '本地 Gateway；助手安装、写配置并打开',
    icon: 'OW',
    importTargetId: 'openclaw',
    downloads: allOS([{ id: 'openclaw', label: 'OpenClaw', url: 'https://github.com/openclaw/openclaw', auto: true }]),
  },
  {
    id: 'paseo',
    name: 'Paseo',
    blurb: '助手自动装 Paseo，并配好 Claude/Codex/Pi',
    icon: 'PA',
    importTargetId: 'paseo',
    downloads: allOS([{ id: 'paseo', label: 'Paseo', url: 'https://paseo.sh/download', auto: false }]),
  },
  {
    id: 'zcode',
    name: 'ZCode',
    blurb: '助手自动安装 ZCode 并准备供应商配置',
    icon: 'ZC',
    importTargetId: 'zcode',
    downloads: allOS([{ id: 'zcode', label: 'ZCode', url: 'https://zcode.z.ai/en/docs/install', auto: false }]),
  },
  {
    id: 'deepseek-harness',
    name: 'DeepSeek Harness',
    blurb: '本地 Web UI；助手自动安装、写配置并打开',
    icon: 'DH',
    importTargetId: 'deepseek-harness',
    downloads: allOS([
      {
        id: 'deepseek-harness',
        label: 'DeepSeek Harness',
        url: 'https://github.com/deepseek-ai/deepseek-harness',
        auto: false,
      },
    ]),
  },
  {
    id: 'obsidian',
    name: 'Obsidian',
    blurb: '笔记里用 Claudian 插件聊 Claude',
    icon: 'OB',
    importTargetId: 'obsidian',
    downloads: allOS([
      { id: 'obsidian', label: 'Obsidian', url: 'https://obsidian.md/download', auto: true },
      { id: 'claudian', label: 'Claudian 插件', url: 'https://github.com/YishenTu/claudian', auto: true },
    ]),
  },
  {
    id: 'cherry-studio',
    name: 'Cherry Studio',
    blurb: '图形界面聊天，上手最快',
    icon: 'CH',
    importTargetId: 'cherry-studio',
    downloads: allOS([
      {
        id: 'cherry',
        label: 'Cherry Studio',
        url: 'https://github.com/CherryHQ/cherry-studio/releases/latest',
        auto: true,
      },
    ]),
  },
  {
    id: 'chatbox',
    name: 'Chatbox',
    blurb: '桌面聊天客户端，一键导入配置',
    icon: 'CB',
    importTargetId: 'chatbox',
    downloads: {
      macos: [{ id: 'chatbox', label: 'Chatbox', url: 'https://chatboxai.app/', auto: true }],
      windows: [
        {
          id: 'chatbox',
          label: 'Chatbox',
          url: 'https://download.chatboxai.app/releases/Chatbox-1.24.4-Setup.exe',
          auto: true,
        },
      ],
      linux: [{ id: 'chatbox', label: 'Chatbox', url: 'https://chatboxai.app/', auto: true }],
    },
  },
] as const

export function getOneClickClient(id: OneClickClientId): OneClickClient {
  const found = ONE_CLICK_CLIENTS.find((c) => c.id === id)
  if (!found) throw new Error(`unknown one-click client: ${id}`)
  return found
}

export function setupPlatformToOS(platform: SetupPlatform = detectSetupPlatform()): OneClickOS {
  if (platform === 'windows') return 'windows'
  if (typeof navigator !== 'undefined') {
    const hint =
      (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ||
      navigator.platform ||
      ''
    if (/linux/i.test(hint) && !/android/i.test(hint)) return 'linux'
  }
  return 'macos'
}

export function detectOneClickOS(): OneClickOS {
  return setupPlatformToOS(detectSetupPlatform())
}

export function downloadsFor(client: OneClickClient, os: OneClickOS): OneClickDownload[] {
  return client.downloads[os] ?? client.downloads.macos
}

/** 网页路径只给一个下载入口，避免连开多个标签。 */
export function primaryDownload(client: OneClickClient, os: OneClickOS): OneClickDownload {
  const list = downloadsFor(client, os)
  return (
    list.find((d) => d.auto) ??
    list[0] ?? { id: 'fallback', label: client.name, url: 'https://idlicat.com', auto: false }
  )
}

/** 打开下载/安装页；浏览器无法静默安装，只能自动拉起下载入口。 */
export function openDownloads(items: OneClickDownload[]): void {
  for (const item of items.filter((d) => d.auto)) {
    window.open(item.url, '_blank', 'noopener,noreferrer')
  }
}

export function buildHelperProtocolURL(opts: {
  client: OneClickClientId
  ticket: string
  os: OneClickOS
  apiBase: string
}): string {
  const api = opts.apiBase.replace(/\/$/, '')
  const q = new URLSearchParams({
    ticket: opts.ticket,
    client: opts.client,
    os: opts.os,
    api,
  })
  return `${ONE_CLICK_HELPER_PROTOCOL}://one-click?${q.toString()}`
}

/** 助手安装包入口（本站同源；与 GET /api/one-click/helper 对齐）。 */
export const ONE_CLICK_HELPER_DOWNLOADS: Record<OneClickOS, OneClickDownload> = {
  macos: {
    id: 'helper',
    label: '闲猫助手',
    url: '/api/one-click/helper/download/macos',
    auto: true,
  },
  windows: {
    id: 'helper',
    label: '闲猫助手',
    url: '/api/one-click/helper/download/windows',
    auto: true,
  },
  linux: {
    id: 'helper',
    label: '闲猫助手',
    url: '/api/one-click/helper/download/linux',
    auto: true,
  },
}

export const ONE_CLICK_HELPER_LOCAL = 'http://127.0.0.1:39217'

const HELPER_INSTALLED_KEY = 'xianmao.helper.installed'

/** 本机是否曾成功连上 / 用户确认装过助手（避免已装仍反复下安装包）。 */
export function markHelperInstalled(): void {
  try {
    localStorage.setItem(HELPER_INSTALLED_KEY, String(Date.now()))
  } catch {
    /* ignore */
  }
}

export function hasHelperInstalledMark(): boolean {
  try {
    return !!localStorage.getItem(HELPER_INSTALLED_KEY)
  } catch {
    return false
  }
}

/** 唤起失败 / 确认已卸载时清掉标记，避免一直跳过「下载安装」。 */
export function clearHelperInstalledMark(): void {
  try {
    localStorage.removeItem(HELPER_INSTALLED_KEY)
  } catch {
    /* ignore */
  }
}

/** 唤起自定义协议。用隐藏 <a> 点击，避免 window.location 把整页跳走。 */
export function launchImportURL(url: string): void {
  if (!url || typeof document === 'undefined') return
  const a = document.createElement('a')
  a.href = url
  a.rel = 'noopener noreferrer'
  a.style.display = 'none'
  document.body.appendChild(a)
  a.click()
  a.remove()
}

/** 浏览器是否正访问「本机」站点。局域网 IP / 域名下，服务端 39217 是跑站点的那台机器，不能代表用户电脑。 */
function siteIsLocalHost(): boolean {
  if (typeof location === 'undefined') return false
  const h = location.hostname
  return h === 'localhost' || h === '127.0.0.1' || h === '[::1]' || h === '::1'
}

async function probeHelperOnce(timeoutMs: number): Promise<boolean> {
  if (typeof fetch === 'undefined') return false
  // 仅本机访问时采信站点代探；局域网/远程访问必须直连用户自己的 127.0.0.1:39217
  if (siteIsLocalHost()) {
    const ctrl = new AbortController()
    const timer = window.setTimeout(() => ctrl.abort(), Math.max(timeoutMs, 1200))
    try {
      const res = await fetch('/api/one-click/helper/status', {
        signal: ctrl.signal,
        cache: 'no-store',
        headers: { Accept: 'application/json' },
      })
      if (res.ok) {
        const body = (await res.json().catch(() => null)) as { data?: { online?: boolean }; online?: boolean } | null
        if (body?.data?.online || body?.online) return true
        // 同源明确说离线时，不再误用直连（直连常被浏览器静默失败）
        if (body && (body.data?.online === false || body.online === false)) return false
      }
    } catch {
      /* 站点代探失败再试直连 */
    } finally {
      window.clearTimeout(timer)
    }
  }

  // 局域网页 → 127.0.0.1 常有 Private Network Access 预检，超时要留足
  const ctrl2 = new AbortController()
  const timer2 = window.setTimeout(() => ctrl2.abort(), Math.max(timeoutMs, 1500))
  try {
    const res = await fetch(`${ONE_CLICK_HELPER_LOCAL}/health`, {
      signal: ctrl2.signal,
      mode: 'cors',
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    })
    if (!res.ok) return false
    const body = (await res.json().catch(() => null)) as { ok?: boolean } | null
    return !!body?.ok
  } catch {
    return false
  } finally {
    window.clearTimeout(timer2)
  }
}

/** 探测本机助手；局域网下默认多试几次，避免偶发预检失败误判「未安装」又去下载。 */
export async function probeHelper(timeoutMs = 350, attempts = 1): Promise<boolean> {
  const n = Math.max(1, attempts)
  for (let i = 0; i < n; i++) {
    if (await probeHelperOnce(timeoutMs)) return true
    if (i + 1 < n) await new Promise((r) => setTimeout(r, 400))
  }
  return false
}

/** 把当前网页的站点根地址推给本机助手（局域网 IP 场景下避免助手仍去连 127.0.0.1）。 */
export async function preferHelperSite(apiBase?: string): Promise<boolean> {
  if (typeof fetch === 'undefined') return false
  const api = String(apiBase || (typeof location !== 'undefined' ? location.origin : '')).replace(/\/$/, '')
  if (!api) return false
  try {
    const ctrl = new AbortController()
    const timer = window.setTimeout(() => ctrl.abort(), 1200)
    const res = await fetch(`${ONE_CLICK_HELPER_LOCAL}/session/site`, {
      method: 'POST',
      mode: 'cors',
      cache: 'no-store',
      headers: { 'Content-Type': 'application/json' },
      signal: ctrl.signal,
      body: JSON.stringify({ api }),
    })
    window.clearTimeout(timer)
    if (res.ok) return true
  } catch {
    /* 旧版助手无此接口时，退回 /open 带 api */
  }
  try {
    const ctrl = new AbortController()
    const timer = window.setTimeout(() => ctrl.abort(), 1200)
    const res = await fetch(`${ONE_CLICK_HELPER_LOCAL}/open`, {
      method: 'POST',
      mode: 'cors',
      cache: 'no-store',
      headers: { 'Content-Type': 'application/json' },
      signal: ctrl.signal,
      body: JSON.stringify({ api }),
    })
    window.clearTimeout(timer)
    return res.ok
  } catch {
    return false
  }
}

/**
 * 尝试让已安装的助手窗口出现在前面。
 * 优先 HTTP（助手已在跑）；否则用 xianmao:// 唤起一次。
 * 浏览器安全策略不允许网页静默启动桌面程序，最多会弹一次「要打开 xxx 吗？」，需用户点「打开」。
 */
export async function openHelperWindow(opts?: { allowProtocol?: boolean }): Promise<boolean> {
  if (typeof fetch === 'undefined') return false
  const allowProtocol = opts?.allowProtocol !== false
  const api = typeof location !== 'undefined' ? location.origin : ''
  try {
    const ctrl = new AbortController()
    const timer = window.setTimeout(() => ctrl.abort(), 1200)
    const res = await fetch(`${ONE_CLICK_HELPER_LOCAL}/open`, {
      method: 'POST',
      mode: 'cors',
      cache: 'no-store',
      headers: { 'Content-Type': 'application/json' },
      signal: ctrl.signal,
      body: JSON.stringify(api ? { api } : {}),
    })
    window.clearTimeout(timer)
    if (res.ok) {
      void preferHelperSite(api)
      return true
    }
  } catch {
    /* 助手可能没在跑 */
  }
  if (!allowProtocol) return false
  const launched = await tryLaunchHelper(`${ONE_CLICK_HELPER_PROTOCOL}://open`, 1500)
  if (!launched) return false
  for (let i = 0; i < 12; i++) {
    await new Promise((r) => setTimeout(r, 500))
    if (await probeHelper(800)) return true
  }
  return false
}

export type HelperDownloadProgress = { loaded: number; total: number; percent: number }

export function helperPackageFilename(os: OneClickOS): string {
  if (os === 'windows') return 'xianmao-helper-windows-x64.zip'
  if (os === 'linux') return 'xianmao-helper-linux-x64.AppImage'
  return 'xianmao-helper-macos.zip'
}

/** 触发浏览器保存安装包（无法静默安装，需用户打开）。 */
export function saveHelperPackage(blob: Blob, os: OneClickOS): void {
  if (typeof document === 'undefined') return
  const filename = helperPackageFilename(os)
  const type =
    os === 'windows' && filename.endsWith('.exe')
      ? 'application/vnd.microsoft.portable-executable'
      : blob.type || 'application/octet-stream'
  const url = URL.createObjectURL(blob instanceof Blob ? blob : new Blob([blob], { type }))
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.rel = 'noopener'
  a.style.display = 'none'
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 30_000)
}

export type HelperPackageDownload = { blob: Blob; filename: string }

/** 本站下载助手安装包（带进度）。网络失败抛错，由 UI 统一提示。 */
export async function downloadHelperPackage(
  os: OneClickOS,
  onProgress?: (p: HelperDownloadProgress) => void,
): Promise<HelperPackageDownload> {
  const path = ONE_CLICK_HELPER_DOWNLOADS[os]?.url || ONE_CLICK_HELPER_DOWNLOADS.macos.url
  const url = path.startsWith('http') ? path : `${typeof location !== 'undefined' ? location.origin : ''}${path}`
  let res: Response
  try {
    res = await fetch(url, { cache: 'no-store', mode: 'cors' })
  } catch {
    throw new Error('NETWORK')
  }
  if (!res.ok) {
    // 未打包该系统时后端会 400；与纯网络故障区分，方便 UI 提示
    if (res.status === 400 || res.status === 404) throw new Error('HELPER_OS_UNSUPPORTED')
    throw new Error('NETWORK')
  }
  const total = Number(res.headers.get('content-length') || 0)
  const headerName = res.headers.get('content-disposition') || ''
  const matched = /filename="?([^";]+)"?/i.exec(headerName)
  const filename = matched?.[1] || helperPackageFilename(os)
  if (!res.body || typeof res.body.getReader !== 'function') {
    const blob = await res.blob().catch(() => {
      throw new Error('NETWORK')
    })
    onProgress?.({ loaded: blob.size, total: blob.size || total, percent: 100 })
    return { blob, filename }
  }
  const reader = res.body.getReader()
  const chunks: Uint8Array[] = []
  let loaded = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    if (value) {
      chunks.push(value)
      loaded += value.length
      const percent = total > 0 ? Math.min(99, Math.round((loaded / total) * 100)) : Math.min(90, 10 + Math.round(loaded / 1e5))
      onProgress?.({ loaded, total, percent })
    }
  }
  onProgress?.({ loaded, total: total || loaded, percent: 100 })
  return {
    blob: new Blob(chunks as BlobPart[], { type: res.headers.get('content-type') || 'application/octet-stream' }),
    filename,
  }
}

/** 请本机市场服务拉起助手（仅本机开发/同源部署可用）。 */
export async function launchHelperViaSite(): Promise<boolean> {
  if (typeof fetch === 'undefined') return false
  try {
    const res = await fetch('/api/one-click/helper/launch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: '{}',
      cache: 'no-store',
    })
    if (!res.ok) return false
    const body = (await res.json().catch(() => null)) as { data?: { online?: boolean; launched?: boolean } } | null
    // 服务端已确认 online 时可直接视为成功，不必再等浏览器探活
    if (body?.data?.online) return true
    return !!body?.data?.launched || res.ok
  } catch {
    return false
  }
}

async function waitHelperOnline(
  attempts: number,
  onProgress?: (p: HelperDownloadProgress) => void,
): Promise<boolean> {
  for (let i = 0; i < attempts; i++) {
    await new Promise((r) => setTimeout(r, 500))
    if (await probeHelper(800)) {
      await openHelperWindow()
      return true
    }
    onProgress?.({ loaded: i + 1, total: attempts, percent: Math.round(((i + 1) / attempts) * 100) })
  }
  return false
}

/** 助手已在线时只走 HTTP 拉窗，绝不再点 xianmao://（避免浏览器确认框）。 */
async function focusHelperWindowHttp(): Promise<void> {
  const api = typeof location !== 'undefined' ? location.origin : ''
  try {
    await fetch(`${ONE_CLICK_HELPER_LOCAL}/open`, {
      method: 'POST',
      mode: 'cors',
      cache: 'no-store',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(api ? { api } : {}),
    })
  } catch {
    /* ignore */
  }
  void preferHelperSite(api)
}

/**
 * 一键使用：只按本机探测决策，且不自动点 xianmao://（避免浏览器反复弹「要打开闲猫吗」）。
 * 1) 39217 在线 → HTTP 打开助手
 * 2) 本机访问时可由站点代启（不经自定义协议）
 * 3) 曾下载/装过但暂时探不到 → 进安装引导，不重复下载
 * 4) 确认未装 → 下载一次安装包
 */
export async function ensureHelperRunning(opts: {
  os: OneClickOS
  /** 跳过唤起，强制重新下载安装包。 */
  forceDownload?: boolean
  onProgress?: (phase: 'download' | 'launch' | 'install', p: HelperDownloadProgress & { message?: string }) => void
}): Promise<boolean> {
  const lanAttempts = siteIsLocalHost() ? 2 : 4
  if (!opts.forceDownload && (await probeHelper(1500, lanAttempts))) {
    markHelperInstalled()
    await focusHelperWindowHttp()
    return true
  }

  // 仅本机站点可代启（不弹浏览器协议确认框）。局域网绝不自动 xianmao://。
  if (!opts.forceDownload && siteIsLocalHost()) {
    opts.onProgress?.('launch', { loaded: 0, total: 8, percent: 0, message: '正在打开闲猫助手…' })
    const launched = await launchHelperViaSite()
    if (launched) {
      if (await probeHelper(1500, 2) || (await waitHelperOnline(8, (p) => opts.onProgress?.('launch', p)))) {
        markHelperInstalled()
        await focusHelperWindowHttp()
        return true
      }
    }
  }

  // 已经下过/装过：不要再自动下一遍（助手开着但探测被浏览器拦截时尤甚）
  if (!opts.forceDownload && hasHelperInstalledMark()) {
    opts.onProgress?.('install', {
      loaded: 1,
      total: 1,
      percent: 100,
      message: '已下载过安装包。若助手已打开，点「我已打开助手」；否则按步骤安装。',
    })
    if (await probeHelper(2000, 3)) {
      markHelperInstalled()
      await focusHelperWindowHttp()
      return true
    }
    throw new Error('INSTALL_GUIDE')
  }

  // 确认未装 → 下载一次
  opts.onProgress?.('download', { loaded: 0, total: 1, percent: 0 })
  let pack: HelperPackageDownload
  try {
    pack = await downloadHelperPackage(opts.os, (p) => opts.onProgress?.('download', p))
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    if (msg === 'HELPER_OS_UNSUPPORTED') throw new Error('HELPER_OS_UNSUPPORTED')
    throw new Error('NETWORK')
  }
  {
    const url = URL.createObjectURL(pack.blob)
    const a = document.createElement('a')
    a.href = url
    a.download = pack.filename || helperPackageFilename(opts.os)
    a.style.display = 'none'
    document.body.appendChild(a)
    a.click()
    a.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 30_000)
  }

  // 只要触发过下载就打标，避免下次一键又重复下载
  markHelperInstalled()
  opts.onProgress?.('install', {
    loaded: 1,
    total: 1,
    percent: 100,
    message: '安装包已下载，请按步骤安装',
  })
  if (await probeHelper(2000, 2)) {
    await focusHelperWindowHttp()
    return true
  }
  throw new Error('INSTALL_GUIDE')
}

/** 网站登录后把账号身份推给本机助手（不同步 Cookie；密钥仍靠一键使用换票）。 */
export async function syncHelperSession(opts: {
  userId: number
  account: string
  apiBase?: string
  retries?: number
}): Promise<boolean> {
  if (typeof fetch === 'undefined') return false
  const api = String(opts.apiBase || (typeof location !== 'undefined' ? location.origin : '')).replace(/\/$/, '')
  const retries = Math.max(1, Number(opts.retries) || 1)
  for (let i = 0; i < retries; i++) {
    const ctrl = new AbortController()
    const timer = window.setTimeout(() => ctrl.abort(), 1500)
    try {
      const res = await fetch(`${ONE_CLICK_HELPER_LOCAL}/session/sync`, {
        method: 'POST',
        mode: 'cors',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json' },
        signal: ctrl.signal,
        body: JSON.stringify({
          user_id: Number(opts.userId) || 0,
          account: String(opts.account || '').trim(),
          api,
        }),
      })
      if (res.ok) return true
    } catch {
      /* helper 可能尚未启动，稍后重试 */
    } finally {
      window.clearTimeout(timer)
    }
    if (i + 1 < retries) await new Promise((r) => setTimeout(r, 800 * (i + 1)))
  }
  return false
}

/** 网站退出时通知助手清本地关联（助手不在线则忽略）。 */
export async function logoutHelperSession(): Promise<boolean> {
  if (typeof fetch === 'undefined') return false
  const ctrl = new AbortController()
  const timer = window.setTimeout(() => ctrl.abort(), 1500)
  try {
    const res = await fetch(`${ONE_CLICK_HELPER_LOCAL}/session/logout`, {
      method: 'POST',
      mode: 'cors',
      cache: 'no-store',
      headers: { 'Content-Type': 'application/json' },
      signal: ctrl.signal,
      body: '{}',
    })
    return res.ok
  } catch {
    return false
  } finally {
    window.clearTimeout(timer)
  }
}

/** @deprecated 用 probeHelper */
export const probeHelperOnline = probeHelper

/**
 * 尝试唤起 xianmao://。blur 只说明「有东西被唤起」，不能保证是闲猫助手。
 */
export function tryLaunchHelper(url: string, timeoutMs = 800): Promise<boolean> {
  return new Promise((resolve) => {
    if (typeof window === 'undefined') {
      resolve(false)
      return
    }
    let settled = false
    const finish = (ok: boolean) => {
      if (settled) return
      settled = true
      window.removeEventListener('blur', onBlur)
      resolve(ok)
    }
    const onBlur = () => finish(true)
    window.addEventListener('blur', onBlur)
    launchImportURL(url)
    window.setTimeout(() => finish(false), timeoutMs)
  })
}

/**
 * 把一键任务交给本机助手。优先 HTTP；不通再试 xianmao://，唤起后再探测本机桥并投递。
 */
async function readHelperStatus(): Promise<{ ok?: boolean; jobSeq?: number; last?: { client?: string } } | null> {
  try {
    const ctrl = new AbortController()
    const timer = window.setTimeout(() => ctrl.abort(), 800)
    const st = await fetch(`${ONE_CLICK_HELPER_LOCAL}/status`, {
      mode: 'cors',
      cache: 'no-store',
      signal: ctrl.signal,
    })
    window.clearTimeout(timer)
    if (!st.ok) return null
    return (await st.json().catch(() => null)) as {
      ok?: boolean
      jobSeq?: number
      last?: { client?: string }
    } | null
  } catch {
    return null
  }
}

async function postOneClickJob(job: {
  ticket: string
  client: OneClickClientId
  os: OneClickOS
  api: string
}): Promise<boolean> {
  const ctrl = new AbortController()
  const timer = window.setTimeout(() => ctrl.abort(), 2500)
  try {
    const res = await fetch(`${ONE_CLICK_HELPER_LOCAL}/one-click`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(job),
      mode: 'cors',
      cache: 'no-store',
      signal: ctrl.signal,
    })
    return res.ok
  } catch {
    return false
  } finally {
    window.clearTimeout(timer)
  }
}

function helperAcceptedJob(
  body: { ok?: boolean; jobSeq?: number; last?: { client?: string; message?: string; status?: string } } | null,
  beforeSeq: number,
  client: string,
): boolean {
  if (!body?.ok) return false
  if ((body.jobSeq ?? 0) > beforeSeq) return true
  const last = body.last
  if (!last) return false
  if (last.client && last.client === client && (body.jobSeq ?? 0) >= beforeSeq) return true
  // 助手 UI 已显示「已接到任务 / 开始处理」即视为交接成功（探测偶发失败时别误报离线）
  if ((body.jobSeq ?? 0) >= beforeSeq && /接到任务|开始处理|排队|安装|写入|密钥/i.test(String(last.message || ''))) {
    return true
  }
  return false
}

export async function handoffToHelper(job: {
  ticket: string
  client: OneClickClientId
  os: OneClickOS
  api: string
}): Promise<boolean> {
  const before = (await probeHelper(1200)) ? await readHelperStatus() : null
  const beforeSeq = before?.jobSeq ?? 0

  // ticket 只能用一次：HTTP 一旦 202 成功就立刻返回，禁止再深链/重投同一 ticket
  if (await probeHelper(1200)) {
    if (await postOneClickJob(job)) return true
  }

  const deeplink = buildHelperProtocolURL({
    ticket: job.ticket,
    client: job.client,
    os: job.os,
    apiBase: job.api,
  })
  const launched = await tryLaunchHelper(deeplink, 1500)
  // blur 只能说明「有东西被唤起」，可能是裸 Electron 欢迎页——必须以本机桥 /health 为准
  let sawHelperOnline = false
  for (let i = 0; i < 12; i++) {
    await new Promise((r) => setTimeout(r, 400))
    const online = await probeHelper(2000)
    if (!online) continue
    sawHelperOnline = true
    const body = await readHelperStatus()
    if (helperAcceptedJob(body, beforeSeq, job.client)) return true
    // 助手在线但深链未必交货：补一次 HTTP（ticket 若已在深链消费会 4xx，忽略即可）
    if (i <= 1 && (await postOneClickJob(job))) return true
  }
  if (sawHelperOnline) {
    const body = await readHelperStatus()
    return helperAcceptedJob(body, beforeSeq, job.client)
  }
  // 唤起了窗口但助手桥从未上线 → 多半是协议绑错成空白 Electron，不算成功
  void launched
  return false
}

/** 网页半自动兜底：仅 Cherry / Chatbox 有直连深链；其余交给助手装软件 + 写配置。 */
export function buildOneClickImportURL(opts: {
  client: OneClickClient
  key: ClientImportKey
  baseURL: string
  model?: string
}): string {
  const directId = opts.client.importTargetId as DirectImportTargetId
  if (directId === 'cherry-studio' || directId === 'chatbox') {
    const config = buildConfigFile(directId, opts.key, opts.baseURL, opts.model ?? '', ONE_CLICK_PROVIDER_NAME)
    return buildDirectImportURL(directId, config)
  }
  return ''
}

export type OneClickProgressStep = {
  id: string
  label: string
  status: 'pending' | 'active' | 'done' | 'error'
}

export function initialProgressSteps(clientName: string): OneClickProgressStep[] {
  return [
    { id: 'key', label: '准备闲猫密钥', status: 'pending' },
    { id: 'download', label: `下载 ${clientName} 安装包`, status: 'pending' },
    { id: 'import', label: '写入连接配置', status: 'pending' },
    { id: 'open', label: '打开就能聊', status: 'pending' },
  ]
}

export function setStep(
  steps: OneClickProgressStep[],
  id: string,
  status: OneClickProgressStep['status'],
): OneClickProgressStep[] {
  return steps.map((s) => {
    if (s.id === id) return { ...s, status }
    if (status === 'active' && s.status === 'active') return { ...s, status: 'pending' }
    return s
  })
}
