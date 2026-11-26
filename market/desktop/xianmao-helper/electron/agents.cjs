/**
 * 助手内可操作的 Agent 清单。配置只能写入闲猫（由换票得到的密钥），不允许自定义上游。
 */
const fs = require('fs')
const path = require('path')

const { preferredVendor, isMultiVendor } = require('./vendors.cjs')

const LOGO_DIR = path.join(__dirname, '..', 'ui', 'assets', 'agents')

function isUsableLogoFile(file) {
  try {
    const fd = fs.openSync(file, 'r')
    const buf = Buffer.alloc(64)
    const n = fs.readSync(fd, buf, 0, 64, 0)
    fs.closeSync(fd)
    if (n <= 0) return false
    const head = buf.slice(0, n).toString('utf8').trimStart()
    // 之前有误把官网 HTML 存成 .svg 的情况，这里直接跳过
    if (head.startsWith('<!DOCTYPE') || head.startsWith('<html') || head.startsWith('<!doctype')) return false
    if (head.startsWith('<svg') || head.startsWith('<?xml')) return true
    if (buf[0] === 0x89 && buf[1] === 0x50) return true // PNG
    if (buf[0] === 0xff && buf[1] === 0xd8) return true // JPEG
    if (buf[0] === 0x00 && buf[1] === 0x00 && buf[2] === 0x01 && buf[3] === 0x00) return true // ICO
    if (head.startsWith('RIFF')) return true // WEBP
    return false
  } catch {
    return false
  }
}

function resolveAgentLogo(id) {
  for (const ext of ['.svg', '.png', '.webp', '.ico']) {
    const file = path.join(LOGO_DIR, `${id}${ext}`)
    if (isUsableLogoFile(file)) return `./assets/agents/${id}${ext}`
  }
  return null
}

const AGENTS = [
  // 专属客户端（桌面 / IDE / Web UI）
  { id: 'codex', name: 'Codex', blurb: '写代码 · 图形/终端', open: 'app', openWhich: 'codex' },
  { id: 'workbuddy', name: 'WorkBuddy', blurb: '腾讯 Agent 办公', open: 'app', openWhich: 'workbuddy' },
  { id: 'zcode', name: 'ZCode', blurb: '图形编程', open: 'app', openWhich: 'zcode' },
  { id: 'deepseek-harness', name: 'DeepSeek Harness', blurb: '浏览器里用', open: 'web', openWhich: 'deepseek-harness' },
  { id: 'cherry-studio', name: 'Cherry Studio', blurb: '图形聊天', open: 'app', openWhich: 'cherry' },
  { id: 'chatbox', name: 'Chatbox', blurb: '桌面聊天', open: 'app', openWhich: 'chatbox' },
  { id: 'claude-desktop', name: 'Claude Desktop', blurb: '官方桌面窗口', open: 'app', openWhich: 'claude-desktop' },
  { id: 'codebuddy', name: 'CodeBuddy', blurb: '腾讯编程助手', open: 'app', openWhich: 'codebuddy' },
  { id: 'trae', name: 'Trae', blurb: '字节 AI IDE', open: 'app', openWhich: 'trae' },
  { id: 'paseo', name: 'Paseo', blurb: '桌面调度', open: 'app', openWhich: 'paseo' },
  { id: 'obsidian', name: 'Obsidian', blurb: '笔记桌面', open: 'app', openWhich: 'obsidian' },
  // 仅 CLI
  { id: 'claude-code', name: 'Claude Code', blurb: '仅终端 · 无窗口', open: 'cli', openWhich: 'claude-code' },
  { id: 'pi', name: 'Pi', blurb: '仅终端 · 无窗口', open: 'cli', openWhich: 'pi' },
  { id: 'hermes', name: 'Hermes', blurb: '仅终端 · 无窗口', open: 'cli', openWhich: 'hermes' },
  { id: 'openclaw', name: 'OpenClaw', blurb: '仅终端 · 无窗口', open: 'cli', openWhich: 'openclaw' },
  { id: 'opencode', name: 'OpenCode', blurb: '终端编程', open: 'cli', openWhich: 'opencode' },
  { id: 'cline', name: 'Cline', blurb: 'IDE / 终端 Agent', open: 'cli', openWhich: 'cline' },
  { id: 'aider', name: 'Aider', blurb: '结对编程 · 终端', open: 'cli', openWhich: 'aider' },
  { id: 'kimi-code', name: 'Kimi Code', blurb: 'Kimi 编程 CLI', open: 'cli', openWhich: 'kimi-code' },
  { id: 'qwen-code', name: 'Qwen Code', blurb: '通义 / Qoder CLI', open: 'cli', openWhich: 'qwen-code' },
  { id: 'gemini', name: 'Gemini CLI', blurb: '仅终端 · 无窗口', open: 'cli', openWhich: 'gemini' },
  { id: 'goose', name: 'Goose', blurb: 'Block 本地 Agent', open: 'cli', openWhich: 'goose' },
  { id: 'crush', name: 'Crush', blurb: 'Charm 终端 Agent', open: 'cli', openWhich: 'crush' },
  { id: 'kilo-code', name: 'Kilo Code', blurb: '终端编程 Agent', open: 'cli', openWhich: 'kilo-code' },
]

/** 终端版完成后的零基础提示（短句） */
function cliGuideFor(clientId) {
  const commonTail = ['已自动连上闲猫', '关掉黑窗口即退出']
  const map = {
    'claude-code': ['已打开黑色终端窗口', '直接打字提问即可', ...commonTail],
    pi: ['已打开黑色终端窗口', '直接打字提问即可', ...commonTail],
    hermes: ['已打开黑色终端窗口', '按提示输入命令或提问', ...commonTail],
    openclaw: ['已打开黑色终端窗口', '按屏幕提示操作', ...commonTail],
    codex: ['当前是终端版 Codex', '在黑窗口里打字提问', ...commonTail],
    opencode: ['已打开黑色终端窗口', '直接打字提问即可', ...commonTail],
    cline: ['已打开黑色终端窗口', '按提示操作即可', ...commonTail],
    aider: ['已打开黑色终端窗口', '在仓库目录里提问改代码', ...commonTail],
    'kimi-code': ['已打开黑色终端窗口', '直接打字提问即可', ...commonTail],
    'qwen-code': ['已打开黑色终端窗口', '直接打字提问即可', ...commonTail],
    gemini: ['已打开黑色终端窗口', '直接打字提问即可', ...commonTail],
    goose: ['已打开黑色终端窗口', '按屏幕提示操作', ...commonTail],
    crush: ['已打开黑色终端窗口', '按屏幕提示操作', ...commonTail],
    'kilo-code': ['已打开黑色终端窗口', '直接打字提问即可', ...commonTail],
  }
  return map[clientId] || ['已打开黑色终端窗口', '在窗口里输入问题即可', ...commonTail]
}

/** 图形端配完后的操作提示（不会自动切模型的客户端） */
function guiGuideFor(clientId) {
  const map = {
    workbuddy: [
      '请先完全退出 WorkBuddy（托盘也退出）再打开',
      '点对话底部「快速 / Fast」打开模型列表',
      '在「自定义模型」里点「闲猫 · …」（官方不会自动切，必须手选一次）',
      '也可：左下角头像 → 系统设置 → 模型',
    ],
    codebuddy: [
      '请完全退出 CodeBuddy 后重开',
      '在模型设置里确认闲猫模型，并在对话中选中',
    ],
  }
  return map[clientId] || null
}

function isKnownAgent(id) {
  return AGENTS.some((a) => a.id === id)
}

function getAgent(id) {
  return AGENTS.find((a) => a.id === id) || null
}

function probeAgentInstalled(id) {
  const { findInstalledApp, whichBin, refreshUserPath } = require('./auto-install.cjs')
  // 安装刚结束时 PATH 可能还没进探测；每次探测前刷新
  try {
    refreshUserPath()
  } catch {
    /* ignore */
  }
  const win = process.platform === 'win32'
  switch (id) {
    case 'codex': {
      const { isChatGPTWinPackageInstalled, CHATGPT_WIN_APPPATH } = require('./auto-install.cjs')
      const app = findInstalledApp(
        win
          ? [
              path.join('ChatGPT', 'ChatGPT.exe'),
              'ChatGPT',
              path.join('Programs', 'ChatGPT', 'ChatGPT.exe'),
              path.join('Programs', 'ChatGPT'),
              path.join('OpenAI', 'ChatGPT', 'ChatGPT.exe'),
            ]
          : ['ChatGPT.app', 'Codex.app', 'ChatGPT Codex.app'],
      )
      const msixApp = win && isChatGPTWinPackageInstalled() ? CHATGPT_WIN_APPPATH : null
      const cli = whichBin('codex') || whichBin('codex.cmd') || whichBin('codex.exe')
      const appPath = app || msixApp || null
      return { installed: !!(appPath || cli), appPath, cliBin: cli || null }
    }
    case 'claude-code': {
      const cli = whichBin('claude') || whichBin('claude.cmd')
      return { installed: !!cli, cliBin: cli || null }
    }
    case 'claude-desktop': {
      const app = findInstalledApp(win ? ['Claude', path.join('Claude', 'Claude.exe')] : ['Claude.app'])
      return { installed: !!app, appPath: app || null }
    }
    case 'pi': {
      const cli = whichBin('pi') || whichBin('pi.cmd')
      return { installed: !!cli, cliBin: cli || null }
    }
    case 'hermes': {
      const cli = whichBin('hermes') || whichBin('hermes.cmd')
      return { installed: !!cli, cliBin: cli || null }
    }
    case 'openclaw': {
      const cli = whichBin('openclaw') || whichBin('openclaw.cmd')
      return { installed: !!cli, cliBin: cli || null }
    }
    case 'paseo': {
      const app = findInstalledApp(win ? ['Paseo', path.join('Paseo', 'Paseo.exe')] : ['Paseo.app'])
      return { installed: !!app, appPath: app || null }
    }
    case 'zcode': {
      const app = findInstalledApp(win ? ['ZCode', path.join('ZCode', 'ZCode.exe')] : ['ZCode.app'])
      return { installed: !!app, appPath: app || null }
    }
    case 'deepseek-harness': {
      const cli = whichBin('dsh') || whichBin('dsh.cmd')
      return { installed: !!cli, cliBin: cli || null }
    }
    case 'obsidian': {
      const app = findInstalledApp(win ? ['Obsidian', path.join('Obsidian', 'Obsidian.exe')] : ['Obsidian.app'])
      return { installed: !!app, appPath: app || null }
    }
    case 'cherry-studio': {
      const app = findInstalledApp(
        win
          ? ['Cherry Studio', path.join('Cherry Studio', 'Cherry Studio.exe')]
          : ['Cherry Studio.app', 'CherryStudio.app'],
      )
      return { installed: !!app, appPath: app || null }
    }
    case 'chatbox': {
      const app = findInstalledApp(
        win ? ['Chatbox', path.join('Chatbox', 'Chatbox.exe')] : ['Chatbox.app', 'chatbox.app'],
      )
      return { installed: !!app, appPath: app || null }
    }
    default: {
      const { probeExtraInstalled } = require('./extra-agents.cjs')
      const extra = probeExtraInstalled(id)
      if (extra) return extra
      return { installed: false }
    }
  }
}

function listAgentsStatus() {
  return AGENTS.map((a) => {
    const st = probeAgentInstalled(a.id)
    return {
      ...a,
      installed: !!st.installed,
      appPath: st.appPath || null,
      cliBin: st.cliBin || null,
      multi_vendor: isMultiVendor(a.id),
      default_vendor: preferredVendor(a.id),
      logo: resolveAgentLogo(a.id),
    }
  })
}

module.exports = {
  AGENTS,
  isKnownAgent,
  getAgent,
  probeAgentInstalled,
  listAgentsStatus,
  cliGuideFor,
  guiGuideFor,
}
