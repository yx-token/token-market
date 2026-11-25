/**
 * 安装前本机资源检测：系统、架构、磁盘、内存。
 */
const fs = require('fs')
const os = require('os')
const path = require('path')
const { execFileSync } = require('child_process')
const { getAgent, probeAgentInstalled } = require('./agents.cjs')

/** 各客户端建议门槛（已安装则只做配置，磁盘门槛降低）。 */
const REQUIREMENTS = {
  default: { diskGB: 1.5, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  codex: { diskGB: 2.5, ramGB: 8, platforms: ['darwin', 'win32', 'linux'], note: '桌面端约 600MB+' },
  'claude-code': { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  'claude-desktop': { diskGB: 2, ramGB: 8, platforms: ['darwin', 'win32'] },
  pi: { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  hermes: { diskGB: 2, ramGB: 4, platforms: ['darwin', 'linux', 'win32'] },
  openclaw: { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  paseo: { diskGB: 1.5, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  zcode: { diskGB: 2, ramGB: 8, platforms: ['darwin', 'win32', 'linux'] },
  'deepseek-harness': { diskGB: 1.5, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  obsidian: { diskGB: 1.5, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  'cherry-studio': { diskGB: 2, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  chatbox: { diskGB: 1.5, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  opencode: { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  cline: { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  aider: { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  'kimi-code': { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  'qwen-code': { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  workbuddy: { diskGB: 2, ramGB: 4, platforms: ['darwin', 'win32'] },
  codebuddy: { diskGB: 2, ramGB: 4, platforms: ['darwin', 'win32'] },
  trae: { diskGB: 2, ramGB: 4, platforms: ['darwin', 'win32'] },
  gemini: { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  goose: { diskGB: 1.5, ramGB: 4, platforms: ['darwin', 'linux', 'win32'] },
  crush: { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
  'kilo-code': { diskGB: 1, ramGB: 4, platforms: ['darwin', 'win32', 'linux'] },
}

function platformLabel(p) {
  if (p === 'darwin') return 'macOS'
  if (p === 'win32') return 'Windows'
  if (p === 'linux') return 'Linux'
  return p
}

function formatGB(bytes) {
  const gb = bytes / (1024 * 1024 * 1024)
  return Math.round(gb * 10) / 10
}

function freeDiskBytes(targetPath) {
  try {
    if (typeof fs.statfsSync === 'function') {
      const st = fs.statfsSync(targetPath)
      const bsize = Number(st.bsize || st.frsize || 0)
      const avail = Number(st.bavail != null ? st.bavail : st.bfree || 0)
      if (bsize > 0 && avail >= 0) return bsize * avail
    }
  } catch {
    /* fall through */
  }
  try {
    if (process.platform === 'win32') {
      const drive = path.parse(targetPath).root.replace(/\\$/, '')
      const out = execFileSync(
        'powershell.exe',
        ['-NoProfile', '-Command', `(Get-PSDrive -Name '${drive.replace(':', '')}').Free`],
        { encoding: 'utf8', timeout: 8000 },
      )
      const n = Number(String(out).trim())
      if (Number.isFinite(n) && n >= 0) return n
    } else {
      const out = execFileSync('df', ['-k', targetPath], { encoding: 'utf8', timeout: 8000 })
      const lines = out.trim().split('\n')
      const cols = lines[lines.length - 1].trim().split(/\s+/)
      // df -k: Filesystem 1K-blocks Used Available Capacity Mounted
      const availK = Number(cols[3])
      if (Number.isFinite(availK) && availK >= 0) return availK * 1024
    }
  } catch {
    /* ignore */
  }
  return null
}

function probeMachine() {
  const home = os.homedir()
  const diskTarget = process.platform === 'win32' ? path.parse(home).root : '/'
  const freeDisk = freeDiskBytes(diskTarget)
  const totalRam = os.totalmem()
  const freeRam = os.freemem()
  return {
    platform: process.platform,
    platformLabel: platformLabel(process.platform),
    arch: os.arch(),
    freeDiskBytes: freeDisk,
    freeDiskGB: freeDisk == null ? null : formatGB(freeDisk),
    totalRamBytes: totalRam,
    totalRamGB: formatGB(totalRam),
    freeRamBytes: freeRam,
    freeRamGB: formatGB(freeRam),
    home,
  }
}

function requirementsFor(clientId) {
  return { ...REQUIREMENTS.default, ...(REQUIREMENTS[clientId] || {}) }
}

/**
 * @returns {{
 *   ok: boolean,
 *   canForce: boolean,
 *   client: string,
 *   name: string,
 *   installed: boolean,
 *   machine: object,
 *   requirements: object,
 *   checks: Array<{ id: string, label: string, ok: boolean, required?: boolean, detail: string }>
 * }}
 */
function runPreflight(clientId) {
  const agent = getAgent(clientId)
  if (!agent) {
    return {
      ok: false,
      canForce: false,
      client: clientId,
      name: clientId,
      installed: false,
      machine: probeMachine(),
      requirements: REQUIREMENTS.default,
      checks: [{ id: 'client', label: '客户端', ok: false, required: true, detail: '未知客户端' }],
    }
  }

  const installed = !!probeAgentInstalled(clientId).installed
  const req = requirementsFor(clientId)
  // 已安装只写配置：磁盘门槛降到 0.3GB
  const needDisk = installed ? Math.min(0.3, req.diskGB) : req.diskGB
  const machine = probeMachine()
  const checks = []

  const platformOk = req.platforms.includes(machine.platform)
  checks.push({
    id: 'platform',
    label: '操作系统',
    ok: platformOk,
    required: true,
    detail: platformOk
      ? `当前 ${machine.platformLabel}（${machine.arch}）可用`
      : `当前 ${machine.platformLabel}，此客户端需要 ${req.platforms.map(platformLabel).join(' / ')}`,
  })

  if (machine.freeDiskGB == null) {
    checks.push({
      id: 'disk',
      label: '磁盘空间',
      ok: false,
      required: false,
      detail: '无法读取可用空间，建议确认磁盘充足后再装',
    })
  } else {
    const diskOk = machine.freeDiskGB >= needDisk
    checks.push({
      id: 'disk',
      label: '磁盘空间',
      ok: diskOk,
      required: true,
      detail: diskOk
        ? `可用约 ${machine.freeDiskGB} GB（需要 ≥ ${needDisk} GB${installed ? '，已安装仅写配置' : ''}${req.note ? `；${req.note}` : ''}）`
        : `可用约 ${machine.freeDiskGB} GB，不足 ${needDisk} GB${req.note ? `（${req.note}）` : ''}，请清理磁盘后再试`,
    })
  }

  const ramOk = machine.totalRamGB >= req.ramGB
  checks.push({
    id: 'ram',
    label: '内存',
    ok: ramOk,
    required: false,
    detail: ramOk
      ? `本机 ${machine.totalRamGB} GB（建议 ≥ ${req.ramGB} GB；空闲约 ${machine.freeRamGB} GB）`
      : `本机 ${machine.totalRamGB} GB，低于建议 ${req.ramGB} GB，安装后可能较卡`,
  })

  const hardFail = checks.some((c) => c.required && !c.ok)
  return {
    ok: !hardFail,
    canForce: !hardFail,
    client: clientId,
    name: agent.name,
    installed,
    machine,
    requirements: { ...req, diskGB: needDisk },
    checks,
  }
}

module.exports = {
  REQUIREMENTS,
  probeMachine,
  runPreflight,
}
