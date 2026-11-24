/**
 * 助手内可操作的 Agent 清单。配置只能写入闲猫（由换票得到的密钥），不允许自定义上游。
 */
const path = require('path')

const AGENTS = [
  { id: 'codex', name: 'Codex', blurb: '写代码 · 图形/终端', open: 'app', openWhich: 'codex' },
  { id: 'claude-code', name: 'Claude Code', blurb: '仅终端 · 无窗口', open: 'cli', openWhich: 'claude-code' },
  { id: 'claude-desktop', name: 'Claude Desktop', blurb: '官方桌面窗口', open: 'app', openWhich: 'claude-desktop' },
  { id: 'pi', name: 'Pi', blurb: '仅终端 · 无窗口', open: 'cli', openWhich: 'pi' },
  { id: 'hermes', name: 'Hermes', blurb: '仅终端 · 无窗口', open: 'cli', openWhich: 'hermes' },
  { id: 'openclaw', name: 'OpenClaw', blurb: '仅终端 · 无窗口', open: 'cli', openWhich: 'openclaw' },
  { id: 'paseo', name: 'Paseo', blurb: '桌面调度', open: 'app', openWhich: 'paseo' },
  { id: 'zcode', name: 'ZCode', blurb: '图形编程', open: 'app', openWhich: 'zcode' },
  { id: 'deepseek-harness', name: 'DeepSeek Harness', blurb: '浏览器里用', open: 'web', openWhich: 'deepseek-harness' },
  { id: 'obsidian', name: 'Obsidian', blurb: '笔记桌面', open: 'app', openWhich: 'obsidian' },
  { id: 'cherry-studio', name: 'Cherry Studio', blurb: '图形聊天', open: 'app', openWhich: 'cherry' },
  { id: 'chatbox', name: 'Chatbox', blurb: '桌面聊天', open: 'app', openWhich: 'chatbox' },
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
  }
  return map[clientId] || ['已打开黑色终端窗口', '在窗口里输入问题即可', ...commonTail]
}

function isKnownAgent(id) {
  return AGENTS.some((a) => a.id === id)
}

function getAgent(id) {
  return AGENTS.find((a) => a.id === id) || null
}

function probeAgentInstalled(id) {
  const { findInstalledApp, whichBin } = require('./auto-install.cjs')
  const win = process.platform === 'win32'
  switch (id) {
    case 'codex': {
      const { isChatGPTWinPackageInstalled, CHATGPT_WIN_APPPATH } = require('./auto-install.cjs')
      const app = findInstalledApp(
        win
          ? [path.join('ChatGPT', 'ChatGPT.exe'), 'ChatGPT', path.join('Programs', 'ChatGPT', 'ChatGPT.exe')]
          : ['ChatGPT.app', 'Codex.app', 'ChatGPT Codex.app'],
      )
      const msixApp = win && isChatGPTWinPackageInstalled() ? CHATGPT_WIN_APPPATH : null
      const cli = whichBin('codex') || whichBin('codex.cmd')
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
    default:
      return { installed: false }
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
}
