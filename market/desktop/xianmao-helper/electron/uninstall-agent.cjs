/**
 * 卸载本机 Agent：尽量卸软件 + 清掉助手写过的闲猫配置。
 */
const fs = require('fs')
const os = require('os')
const path = require('path')
const { spawnSync } = require('child_process')
const { getAgent, probeAgentInstalled } = require('./agents.cjs')
const {
  whichBin,
  refreshUserPath,
  npmPrefixDir,
  isChatGPTWinPackageInstalled,
  hermesHomeDir,
  dshHomeDir,
} = require('./auto-install.cjs')

function isWin() {
  return process.platform === 'win32'
}

function isMac() {
  return process.platform === 'darwin'
}

function tryRm(file) {
  try {
    if (fs.existsSync(file)) {
      fs.rmSync(file, { recursive: true, force: true })
      return true
    }
  } catch {
    /* ignore */
  }
  return false
}

function restoreBak(file) {
  const bak = `${file}.bak-xianmao`
  try {
    if (fs.existsSync(bak)) {
      fs.copyFileSync(bak, file)
      return true
    }
  } catch {
    /* ignore */
  }
  return tryRm(file)
}

function clearCodexConfig() {
  const dir = process.env.CODEX_HOME || path.join(os.homedir(), '.codex')
  const cleared = []
  for (const name of ['config.toml', 'auth.json']) {
    const f = path.join(dir, name)
    if (restoreBak(f) || tryRm(f)) cleared.push(f)
  }
  return cleared
}

function clearClaudeSettings() {
  const dir = process.env.CLAUDE_CONFIG_DIR || path.join(os.homedir(), '.claude')
  const file = path.join(dir, 'settings.json')
  if (!fs.existsSync(file)) return []
  try {
    const cur = JSON.parse(fs.readFileSync(file, 'utf8'))
    const env = { ...(cur.env || {}) }
    delete env.ANTHROPIC_BASE_URL
    delete env.ANTHROPIC_AUTH_TOKEN
    delete env.ANTHROPIC_API_KEY
    delete env.ANTHROPIC_MODEL
    const next = { ...cur, env }
    if (!Object.keys(env).length) delete next.env
    fs.copyFileSync(file, `${file}.bak-xianmao-uninstall`)
    fs.writeFileSync(file, JSON.stringify(next, null, 2) + '\n', 'utf8')
    return [file]
  } catch {
    return []
  }
}

function clearClaudeDesktop3p() {
  const root =
    process.platform === 'darwin'
      ? path.join(os.homedir(), 'Library/Application Support/Claude-3p')
      : process.platform === 'win32'
        ? path.join(process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData/Local'), 'Claude-3p')
        : path.join(os.homedir(), '.config/Claude-3p')
  const cleared = []
  const lib = path.join(root, 'configLibrary')
  if (tryRm(lib)) cleared.push(lib)
  const desk = path.join(root, 'claude_desktop_config.json')
  if (restoreBak(desk) || tryRm(desk)) cleared.push(desk)
  return cleared
}

function npmUninstall(pkg) {
  refreshUserPath()
  const npmCmd = whichBin('npm.cmd') || whichBin('npm') || (isWin() ? 'npm.cmd' : 'npm')
  const prefix = npmPrefixDir()
  const r = spawnSync(npmCmd, ['uninstall', '-g', pkg, `--prefix=${prefix}`], {
    encoding: 'utf8',
    timeout: 180000,
    shell: isWin(),
    env: { ...process.env, npm_config_prefix: prefix },
  })
  refreshUserPath()
  return r.status === 0
}

function wingetUninstall(id) {
  if (!isWin()) return false
  const winget = whichBin('winget') || whichBin('winget.exe')
  if (!winget) return false
  const r = spawnSync(
    winget,
    ['uninstall', '--id', id, '--exact', '--disable-interactivity', '--accept-source-agreements'],
    { encoding: 'utf8', timeout: 300000, windowsHide: true },
  )
  return r.status === 0
}

function brewUninstallCask(cask) {
  if (!isMac()) return false
  const brew = whichBin('brew')
  if (!brew) return false
  const r = spawnSync(brew, ['uninstall', '--cask', cask], { encoding: 'utf8', timeout: 300000 })
  return r.status === 0
}

function removeChatGPTWinPackage() {
  if (!isWin()) return false
  try {
    const r = spawnSync(
      'powershell.exe',
      [
        '-NoProfile',
        '-Command',
        [
          "$names = @('OpenAI.Codex','OpenAI.ChatGPT')",
          "$pkgs = Get-AppxPackage | Where-Object { $names -contains $_.Name -or $_.Name -match 'OpenAI\\.(Codex|ChatGPT)' }",
          "if (-not $pkgs) { exit 2 }",
          "$pkgs | Remove-AppxPackage -ErrorAction Stop",
          'exit 0',
        ].join('; '),
      ],
      { encoding: 'utf8', timeout: 180000, windowsHide: true },
    )
    return r.status === 0 && !isChatGPTWinPackageInstalled()
  } catch {
    return false
  }
}

function clearFileList(files) {
  const cleared = []
  for (const f of files) {
    if (restoreBak(f) || tryRm(f)) cleared.push(f)
  }
  return cleared
}

/**
 * @returns {{ ok: boolean, detail: string, cleared?: string[], manualHint?: string, stillInstalled?: boolean }}
 */
function uninstallAgent(clientId) {
  const agent = getAgent(clientId)
  if (!agent) return { ok: false, detail: '未知客户端' }

  const before = probeAgentInstalled(clientId)
  if (!before.installed) {
    return { ok: true, detail: `${agent.name} 本机未检测到安装`, cleared: [] }
  }

  const cleared = []
  const notes = []
  let removedSoft = false
  let manualHint = ''

  switch (clientId) {
    case 'codex': {
      cleared.push(...clearCodexConfig())
      if (npmUninstall('@openai/codex')) {
        removedSoft = true
        notes.push('已卸载 Codex CLI')
      }
      if (wingetUninstall('OpenAI.ChatGPT') || removeChatGPTWinPackage()) {
        removedSoft = true
        notes.push('已卸载 Codex 图形版')
      } else if (brewUninstallCask('chatgpt') || brewUninstallCask('codex')) {
        removedSoft = true
        notes.push('已卸载 Codex 图形版')
      } else if (before.appPath) {
        manualHint = '图形版请在系统「应用」里手动卸载 ChatGPT / Codex'
      }
      break
    }
    case 'claude-code':
    case 'obsidian': {
      cleared.push(...clearClaudeSettings())
      if (clientId === 'claude-code' && npmUninstall('@anthropic-ai/claude-code')) {
        removedSoft = true
        notes.push('已卸载 Claude Code')
      }
      if (clientId === 'obsidian') {
        manualHint = 'Obsidian 本体请在系统里手动卸载；已清除闲猫写入的 Claude 配置'
      }
      break
    }
    case 'claude-desktop': {
      cleared.push(...clearClaudeDesktop3p(), ...clearClaudeSettings())
      if (wingetUninstall('Anthropic.Claude') || brewUninstallCask('claude')) {
        removedSoft = true
        notes.push('已卸载 Claude Desktop')
      } else {
        manualHint = 'Claude Desktop 请在系统里手动卸载；已清除闲猫 3P 网关配置'
      }
      break
    }
    case 'pi': {
      cleared.push(...clearFileList([path.join(os.homedir(), '.pi', 'agent', 'models.json')]))
      if (npmUninstall('@earendil-works/pi-coding-agent') || npmUninstall('pi')) {
        removedSoft = true
        notes.push('已卸载 Pi')
      }
      break
    }
    case 'hermes': {
      cleared.push(...clearFileList([path.join(hermesHomeDir(), 'config.yaml')]))
      manualHint = 'Hermes 请按官方方式卸载；已清除闲猫写入的 config.yaml'
      break
    }
    case 'openclaw': {
      cleared.push(
        ...clearFileList([
          process.env.OPENCLAW_CONFIG_PATH || path.join(os.homedir(), '.openclaw', 'openclaw.json'),
        ]),
      )
      if (npmUninstall('openclaw')) {
        removedSoft = true
        notes.push('已卸载 OpenClaw')
      }
      break
    }
    case 'opencode': {
      cleared.push(...clearFileList([path.join(os.homedir(), '.config', 'opencode', 'opencode.json')]))
      if (npmUninstall('@opencode/cli') || npmUninstall('opencode')) {
        removedSoft = true
        notes.push('已卸载 OpenCode')
      }
      break
    }
    case 'cline': {
      cleared.push(...clearFileList([path.join(os.homedir(), '.cline', 'xianmao.json')]))
      if (npmUninstall('cline') || npmUninstall('@cline/cli')) {
        removedSoft = true
        notes.push('已卸载 Cline')
      }
      break
    }
    case 'aider': {
      cleared.push(
        ...clearFileList([
          path.join(os.homedir(), '.aider.conf.yml'),
          path.join(os.homedir(), '.aider', 'xianmao.env'),
        ]),
      )
      const pip = whichBin('pip3') || whichBin('pip') || whichBin('pip3.exe') || whichBin('pip.exe')
      if (pip) {
        const r = spawnSync(pip, ['uninstall', '-y', 'aider-chat'], {
          encoding: 'utf8',
          timeout: 180000,
          shell: isWin(),
        })
        if (r.status === 0) {
          removedSoft = true
          notes.push('已卸载 Aider')
        }
      }
      break
    }
    case 'kimi-code': {
      cleared.push(...clearFileList([path.join(os.homedir(), '.kimi', '.env')]))
      if (npmUninstall('@moonshot-ai/kimi-code')) {
        removedSoft = true
        notes.push('已卸载 Kimi Code')
      }
      break
    }
    case 'qwen-code': {
      cleared.push(...clearFileList([path.join(os.homedir(), '.qwen', '.env')]))
      if (npmUninstall('@qwen-code/qwen-code') || npmUninstall('qwen-code')) {
        removedSoft = true
        notes.push('已卸载 Qwen Code')
      }
      break
    }
    case 'gemini': {
      cleared.push(
        ...clearFileList([
          path.join(os.homedir(), '.gemini', '.env'),
          path.join(os.homedir(), '.gemini', 'settings.json'),
        ]),
      )
      if (npmUninstall('@google/gemini-cli')) {
        removedSoft = true
        notes.push('已卸载 Gemini CLI')
      }
      break
    }
    case 'goose': {
      cleared.push(
        ...clearFileList([
          path.join(os.homedir(), '.config', 'goose', 'config.yaml'),
          path.join(os.homedir(), '.config', 'goose', 'secrets.yaml'),
        ]),
      )
      manualHint = 'Goose 请按官方方式卸载；已清除闲猫写入的配置'
      break
    }
    case 'crush': {
      cleared.push(...clearFileList([path.join(os.homedir(), '.config', 'crush', 'crush.json')]))
      if (npmUninstall('@charmland/crush')) {
        removedSoft = true
        notes.push('已卸载 Crush')
      }
      break
    }
    case 'kilo-code': {
      cleared.push(...clearFileList([path.join(os.homedir(), '.kilocode', 'config.json')]))
      if (npmUninstall('@kilocode/cli')) {
        removedSoft = true
        notes.push('已卸载 Kilo Code')
      }
      break
    }
    case 'deepseek-harness': {
      const home = dshHomeDir()
      cleared.push(
        ...clearFileList([
          path.join(home, '.credentials.yaml'),
          path.join(home, 'profiles', 'web', 'cordis.patch.yml'),
        ]),
      )
      if (npmUninstall('@deepseek-ai/dsh') || npmUninstall('dsh')) {
        removedSoft = true
        notes.push('已卸载 DeepSeek Harness')
      }
      break
    }
    case 'workbuddy':
    case 'codebuddy':
    case 'trae':
    case 'cherry-studio':
    case 'chatbox':
    case 'paseo':
    case 'zcode': {
      const confMap = {
        workbuddy: [
          path.join(os.homedir(), '.workbuddy-ai', 'models.json'),
          path.join(os.homedir(), '.codebuddy', 'models.json'),
          path.join(os.homedir(), 'workbuddy', 'models.json'),
        ],
        codebuddy: [
          path.join(os.homedir(), '.workbuddy-ai', 'models.json'),
          path.join(os.homedir(), '.codebuddy', 'models.json'),
        ],
        trae: [path.join(os.homedir(), '.trae', 'xianmao-provider.json')],
        zcode: [path.join(os.homedir(), 'Downloads', 'xianmao-helper', 'zcode-provider.json')],
      }
      if (confMap[clientId]) cleared.push(...clearFileList(confMap[clientId]))
      manualHint = `${agent.name} 请在系统「应用」里手动卸载；已清除助手写入的闲猫配置`
      break
    }
    default:
      manualHint = `暂不支持自动卸载 ${agent.name}，请在系统里手动卸`
  }

  refreshUserPath()
  const after = probeAgentInstalled(clientId)
  const stillInstalled = !!after.installed
  const detailParts = [`已清除闲猫配置 ${cleared.length} 项`]
  if (notes.length) detailParts.push(notes.join('；'))
  if (stillInstalled && manualHint) detailParts.push(manualHint)
  else if (stillInstalled) detailParts.push('软件仍在，请到系统里手动卸完')
  else if (!removedSoft && !manualHint) detailParts.push('本机已无此客户端')

  return {
    ok: true,
    detail: detailParts.filter(Boolean).join('。'),
    cleared,
    manualHint: stillInstalled ? manualHint || '请到系统设置里完成卸载' : '',
    stillInstalled,
  }
}

module.exports = { uninstallAgent }
