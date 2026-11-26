/**
 * 把闲猫密钥真正写进本机客户端，尽量做到打开就能聊。
 * - Codex / Claude Code：直接写配置文件
 * - Cherry Studio：
 *   - 旧版（如 0.9.x）深链只处理 OAuth，不导入供应商 → 直接改 Local Storage
 *   - 新版同时发 data= 深链（可点确认）
 * - Chatbox：官方 deep link
 */
const fs = require('fs')
const os = require('os')
const path = require('path')
const { execFileSync } = require('child_process')
const { shell } = require('electron')
const {
  ensureCherry,
  ensureChatbox,
  ensureClaudeCode,
  ensureClaudeDesktop,
  ensurePi,
  ensureHermes,
  hermesHomeDir,
  ensureOpenClawCLI,
  ensureObsidian,
  ensureCodex,
  ensureDeepSeekHarness,
  ensurePaseo,
  ensureZCode,
  dshHomeDir,
  openInstalledApp,
  spawnDetached,
  waitHttpOk,
  findInstalledApp,
  throwIfCancelled,
  yieldEventLoop,
} = require('./auto-install.cjs')

function withV1(base) {
  const b = String(base || '').replace(/\/$/, '')
  if (!b) return b
  return /\/v1$/i.test(b) ? b : `${b}/v1`
}

function providerId(base, keyId) {
  let host = 'gateway'
  try {
    host = new URL(base).host
  } catch {
    /* ignore */
  }
  return `idlecat-${host.toLowerCase().replace(/[^a-z0-9-]/g, '-')}-${keyId || 0}`
}

function cherryConfig(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  return {
    id: providerId(base, payload.key_id),
    name: payload.provider || '闲猫',
    type: 'openai',
    apiKey: payload.key,
    baseUrl: withV1(base),
  }
}

function chatboxConfig(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  return {
    id: providerId(base, payload.key_id),
    name: payload.provider || '闲猫',
    type: 'openai',
    isCustom: true,
    urls: { website: base },
    settings: {
      apiHost: base,
      apiPath: '/v1/chat/completions',
      apiKey: payload.key,
      models: [{ modelId: 'gpt-4o', type: 'chat' }],
    },
  }
}

/** 与网页 buildDirectImportURL 一致：标准 base64，再把 +/ 换成 _- 后 URL 编码。 */
function buildCherryImportURL(payload) {
  const json = JSON.stringify(cherryConfig(payload))
  const b64 = Buffer.from(json, 'utf8').toString('base64').replace(/\+/g, '_').replace(/\//g, '-')
  return `cherrystudio://providers/api-keys?v=1&data=${encodeURIComponent(b64)}`
}

function buildChatboxImportURL(payload) {
  const json = JSON.stringify(chatboxConfig(payload))
  const b64 = Buffer.from(json, 'utf8').toString('base64')
  return `chatbox://provider/import?config=${encodeURIComponent(b64)}`
}

function appInstalled(candidates) {
  return candidates.some((p) => {
    try {
      return fs.existsSync(p)
    } catch {
      return false
    }
  })
}

/** @deprecated 用 auto-install.appLookupPaths / findInstalledApp */
function macApps(names) {
  const { appLookupPaths } = require('./auto-install.cjs')
  return appLookupPaths(names)
}

function writeCodex(payload) {
  const { defaultModelForPlatform } = require('./vendors.cjs')
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const endpoint = withV1(base)
  const dir = process.env.CODEX_HOME || path.join(os.homedir(), '.codex')
  fs.mkdirSync(dir, { recursive: true })
  const model = String(payload.model || defaultModelForPlatform(payload.platform || 'openai') || 'gpt-5.5').trim()
  const key = String(payload.key || '').trim()
  // Codex 桌面端若 requires_openai_auth=true，会优先发 ChatGPT OAuth 到自定义网关 → 401 invalid api key。
  // 第三方网关应把 key 写进 provider 的 experimental_bearer_token（与 88API 等一键脚本同款），
  // 并 preferred_auth_method=apikey，避免被 ChatGPT 登录态覆盖。
  const toml = [
    `model = "${model.replace(/"/g, '')}"`,
    'model_provider = "gateway"',
    'preferred_auth_method = "apikey"',
    'cli_auth_credentials_store = "file"',
    'web_search = "disabled"',
    '',
    '[model_providers.gateway]',
    `name = "${(payload.provider || '闲猫').replace(/"/g, '')}"`,
    `base_url = "${endpoint}"`,
    'wire_api = "responses"',
    `experimental_bearer_token = "${key.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`,
    '',
  ].join('\n')
  const auth = JSON.stringify({ OPENAI_API_KEY: key }, null, 2) + '\n'
  const configPath = path.join(dir, 'config.toml')
  const authPath = path.join(dir, 'auth.json')
  if (fs.existsSync(configPath)) fs.copyFileSync(configPath, `${configPath}.bak-xianmao`)
  if (fs.existsSync(authPath)) fs.copyFileSync(authPath, `${authPath}.bak-xianmao`)
  fs.writeFileSync(configPath, toml, 'utf8')
  fs.writeFileSync(authPath, auth, 'utf8')
  return { wrote: [configPath, authPath], open: null }
}

function writeClaudeCode(payload) {
  const { defaultModelForPlatform } = require('./vendors.cjs')
  // Claude Code 会在 base 后追加 /v1/messages，这里不要带 /v1。
  const base = String(payload.base_url || '').replace(/\/$/, '').replace(/\/v1$/i, '')
  const dir = process.env.CLAUDE_CONFIG_DIR || path.join(os.homedir(), '.claude')
  fs.mkdirSync(dir, { recursive: true })
  const file = path.join(dir, 'settings.json')
  let current = {}
  if (fs.existsSync(file)) {
    try {
      current = JSON.parse(fs.readFileSync(file, 'utf8'))
    } catch {
      current = {}
    }
    fs.copyFileSync(file, `${file}.bak-xianmao`)
  }
  const key = String(payload.key || '').trim()
  const model = String(
    payload.model || defaultModelForPlatform(payload.platform || 'anthropic') || 'claude-sonnet-4-5',
  ).trim()
  const env = { ...(current.env || {}) }
  env.ANTHROPIC_BASE_URL = base
  // 网关走 Authorization: Bearer。同时设 ANTHROPIC_API_KEY 会发 x-api-key，
  // 且可能被当成直连 Anthropic 凭证覆盖网关 → 401。官方/OpenRouter 都要求清空 API_KEY。
  env.ANTHROPIC_AUTH_TOKEN = key
  env.ANTHROPIC_API_KEY = ''
  env.ANTHROPIC_MODEL = model
  const next = { ...current, env }
  fs.writeFileSync(file, JSON.stringify(next, null, 2) + '\n', 'utf8')
  return { wrote: [file], open: null }
}

function claudeDesktop3pDir() {
  if (process.platform === 'darwin') {
    return path.join(os.homedir(), 'Library/Application Support/Claude-3p')
  }
  if (process.platform === 'win32') {
    return path.join(process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData/Local'), 'Claude-3p')
  }
  return path.join(os.homedir(), '.config/Claude-3p')
}

/** Claude Desktop 不读 ~/.claude/settings.json，要写 3P configLibrary。 */
function writeClaudeDesktop(payload) {
  const { defaultModelForPlatform } = require('./vendors.cjs')
  const base = String(payload.base_url || '').replace(/\/$/, '').replace(/\/v1$/i, '')
  const key = String(payload.key || '').trim()
  const model = String(
    payload.model || defaultModelForPlatform(payload.platform || 'anthropic') || 'claude-sonnet-4-5',
  ).trim()
  const root = claudeDesktop3pDir()
  const lib = path.join(root, 'configLibrary')
  fs.mkdirSync(lib, { recursive: true })
  const id = '00000000-0000-4000-8000-7869616e6d61' // fixed idlecat id
  const cfgPath = path.join(lib, `${id}.json`)
  const metaPath = path.join(lib, '_meta.json')
  const deskCfg = path.join(root, 'claude_desktop_config.json')
  for (const p of [cfgPath, metaPath, deskCfg]) {
    if (fs.existsSync(p)) fs.copyFileSync(p, `${p}.bak-xianmao`)
  }
  const gateway = {
    inferenceProvider: 'gateway',
    inferenceCredentialKind: 'static',
    inferenceGatewayBaseUrl: base,
    inferenceGatewayApiKey: key,
    inferenceGatewayAuthScheme: 'bearer',
    inferenceModels: [{ name: model, labelOverride: model }],
  }
  fs.writeFileSync(cfgPath, JSON.stringify(gateway, null, 2) + '\n', 'utf8')
  fs.writeFileSync(
    metaPath,
    JSON.stringify({ appliedId: id, entries: [{ id, name: payload.provider || '闲猫' }] }, null, 2) + '\n',
    'utf8',
  )
  let desk = {}
  if (fs.existsSync(deskCfg)) {
    try {
      desk = JSON.parse(fs.readFileSync(deskCfg, 'utf8'))
    } catch {
      desk = {}
    }
  }
  desk.deploymentMode = '3p'
  fs.writeFileSync(deskCfg, JSON.stringify(desk, null, 2) + '\n', 'utf8')
  // Claudian / 终端 Claude Code 仍吃 settings.json
  const code = writeClaudeCode(payload)
  return { wrote: [cfgPath, metaPath, deskCfg, ...code.wrote], open: null }
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

function writePi(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const endpoint = withV1(base)
  const file = path.join(os.homedir(), '.pi', 'agent', 'models.json')
  const modelId = payload.model || 'gpt-5.5'
  const wrote = mergeJSONFile(file, (current) => {
    const providers = { ...(current.providers || {}) }
    providers.gateway = {
      baseUrl: endpoint,
      api: 'openai-completions',
      apiKey: payload.key,
      models: [{ id: modelId, name: modelId }],
    }
    return { ...current, providers }
  })
  return { wrote: [wrote], open: null }
}

function writeOpenClaw(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const endpoint = withV1(base)
  const modelId = payload.model || 'gpt-5.5'
  const file =
    process.env.OPENCLAW_CONFIG_PATH || path.join(os.homedir(), '.openclaw', 'openclaw.json')
  const wrote = mergeJSONFile(file, (current) => {
    const models = { ...(current.models || {}), mode: 'merge' }
    const providers = { ...(models.providers || {}) }
    providers.gateway = {
      baseUrl: endpoint,
      apiKey: payload.key,
      api: 'openai-completions',
      models: [{ id: modelId, name: modelId }],
    }
    models.providers = providers
    const agents = { ...(current.agents || {}) }
    const defaults = { ...(agents.defaults || {}) }
    defaults.model = { ...(defaults.model || {}), primary: `gateway/${modelId}` }
    defaults.models = { ...(defaults.models || {}), [`gateway/${modelId}`]: {} }
    agents.defaults = defaults
    return { ...current, models, agents }
  })
  return { wrote: [wrote], open: null }
}

/** 写入 ~/.hermes/config.yaml 的 model 段（custom OpenAI 兼容端点）。 */
function writeHermes(payload) {
  const endpoint = withV1(String(payload.base_url || '').replace(/\/$/, ''))
  const modelId = payload.model || 'gpt-5.5'
  const file = path.join(hermesHomeDir(), 'config.yaml')
  const modelBlock = [
    'model:',
    '  provider: custom',
    `  base_url: ${JSON.stringify(endpoint)}`,
    `  api_key: ${JSON.stringify(String(payload.key || ''))}`,
    `  default: ${JSON.stringify(modelId)}`,
    '',
  ].join('\n')
  let next = modelBlock
  if (fs.existsSync(file)) {
    const prev = fs.readFileSync(file, 'utf8')
    if (/^model:\s*$/m.test(prev) || /^model:\s*\n/m.test(prev)) {
      // 替换已有 model: 块（到下一个顶层 key 或文件末尾）
      next = prev.replace(/^model:\n(?:[ \t].*\n)*/m, modelBlock)
      if (next === prev) next = `${prev.replace(/\s*$/, '')}\n\n${modelBlock}`
    } else {
      next = `${prev.replace(/\s*$/, '')}\n\n${modelBlock}`
    }
  }
  fs.mkdirSync(path.dirname(file), { recursive: true })
  fs.writeFileSync(file, next.endsWith('\n') ? next : `${next}\n`, 'utf8')
  return { wrote: [file], open: null }
}

/** 写入 ZCode 真正读取的 ~/.zcode/v2/provider_config.json（含默认模型）。 */
function writeZCodeRef(payload) {
  const { defaultModelForPlatform } = require('./vendors.cjs')
  const platform = String(payload.platform || '').toLowerCase()
  const wantAnthropic = platform === 'anthropic' || /claude/i.test(String(payload.model || ''))
  const rawBase = String(payload.base_url || '').replace(/\/$/, '')
  const baseNoV1 = rawBase.replace(/\/v1$/i, '')
  const modelId = String(
    payload.model || defaultModelForPlatform(payload.platform || (wantAnthropic ? 'anthropic' : 'openai')) || 'gpt-5.5',
  ).trim()
  const providerId = 'xianmao'
  const providerName = String(payload.provider || '闲猫').trim() || '闲猫'
  const api = wantAnthropic
    ? { type: 'anthropic-messages', baseUrl: baseNoV1 }
    : { type: 'openai-chat-completions', baseUrl: /\/v1$/i.test(rawBase) ? rawBase : `${rawBase}/v1` }

  const file = path.join(os.homedir(), '.zcode', 'v2', 'provider_config.json')
  let current = { schemaVersion: 1, config: {} }
  if (fs.existsSync(file)) {
    try {
      current = JSON.parse(fs.readFileSync(file, 'utf8')) || current
    } catch {
      current = { schemaVersion: 1, config: {} }
    }
    fs.copyFileSync(file, `${file}.bak-xianmao`)
  }
  const cfg = { ...(current.config || {}) }
  const providerRules = Array.isArray(cfg.providerConfigRules?.providerRules)
    ? cfg.providerConfigRules.providerRules.filter((r) => r && r.providerId !== providerId)
    : []
  providerRules.unshift({
    providerId,
    providerName,
    config: {
      group: 'standard-personal',
      access: { type: 'api-key', apiKey: String(payload.key || '') },
      api,
      personalModelIds: [modelId],
    },
  })
  cfg.providerConfigRules = { ...(cfg.providerConfigRules || {}), providerRules }
  cfg.modelConfigRules = {
    providerModelRules: Array.isArray(cfg.modelConfigRules?.providerModelRules)
      ? cfg.modelConfigRules.providerModelRules
      : [],
    manualProviderModelRules: Array.isArray(cfg.modelConfigRules?.manualProviderModelRules)
      ? cfg.modelConfigRules.manualProviderModelRules
      : [],
  }
  cfg.defaultModelSelection = { providerId, modelId }
  const body = { schemaVersion: 1, ...current, config: cfg }
  fs.mkdirSync(path.dirname(file), { recursive: true })
  fs.writeFileSync(file, JSON.stringify(body, null, 2) + '\n', 'utf8')
  try {
    fs.chmodSync(file, 0o600)
  } catch {
    /* win */
  }
  // 参考副本，便于用户核对
  const refDir = path.join(os.homedir(), 'Downloads', 'xianmao-helper')
  fs.mkdirSync(refDir, { recursive: true })
  const ref = path.join(refDir, 'zcode-provider.json')
  fs.writeFileSync(
    ref,
    JSON.stringify(
      {
        name: providerName,
        baseUrl: api.baseUrl,
        apiFormat: api.type,
        apiKey: payload.key,
        models: [{ id: modelId, name: modelId }],
      },
      null,
      2,
    ) + '\n',
    'utf8',
  )
  return { wrote: [file, ref], open: null }
}

function yamlScalar(value) {
  return JSON.stringify(String(value ?? ''))
}

/** 写入 ~/.dsh 凭证 + web 配置，使网关密钥可直接用（不再只丢参考 JSON）。 */
function writeDeepSeekHarness(payload) {
  const home = dshHomeDir()
  const credFile = path.join(home, '.credentials.yaml')
  const profileDir = path.join(home, 'profiles', 'web')
  const patchFile = path.join(profileDir, 'cordis.patch.yml')
  fs.mkdirSync(profileDir, { recursive: true })

  const endpoint = withV1(String(payload.base_url || '').replace(/\/$/, ''))
  const modelId = String(payload.model || 'deepseek-chat').trim() || 'deepseek-chat'
  const displayName = String(payload.provider || '闲猫').trim() || '闲猫'
  const envName = 'XIANMAO_API_KEY'
  const keyLine = `${envName}: ${yamlScalar(payload.key)}`

  let cred = ''
  try {
    cred = fs.readFileSync(credFile, 'utf8')
  } catch {
    cred = ''
  }
  if (new RegExp(`^${envName}:`, 'm').test(cred)) {
    cred = cred.replace(new RegExp(`^${envName}:.*$`, 'm'), keyLine)
  } else {
    cred = `${cred.replace(/\s*$/, '')}${cred ? '\n' : ''}${keyLine}\n`
  }
  fs.writeFileSync(credFile, cred.endsWith('\n') ? cred : `${cred}\n`, { encoding: 'utf8', mode: 0o600 })
  try {
    fs.chmodSync(credFile, 0o600)
  } catch {
    /* win */
  }

  const markerBegin = '# --- xianmao-helper begin ---'
  const markerEnd = '# --- xianmao-helper end ---'
  const block = `${markerBegin}
- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: xianmao
    model: ${yamlScalar(modelId)}
- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      xianmao:
        displayName: ${yamlScalar(displayName)}
        apiKeyEnv: ${envName}
        api: openai-completions
        baseURL: ${yamlScalar(endpoint)}
        models:
          - id: ${yamlScalar(modelId)}
            name: ${yamlScalar(modelId)}
${markerEnd}
`
  let patch = ''
  try {
    patch = fs.readFileSync(patchFile, 'utf8')
  } catch {
    patch = ''
  }
  if (patch.includes(markerBegin) && patch.includes(markerEnd)) {
    patch = patch.replace(new RegExp(`${markerBegin}[\\s\\S]*?${markerEnd}\\n?`), `${block}`)
  } else {
    patch = `${patch.replace(/\s*$/, '')}${patch ? '\n' : ''}${block}`
  }
  fs.writeFileSync(patchFile, patch.endsWith('\n') ? patch : `${patch}\n`, 'utf8')
  return { wrote: [credFile, patchFile], endpoint, modelId }
}

function cherryDataDir() {
  if (process.platform === 'darwin') {
    return path.join(os.homedir(), 'Library/Application Support/CherryStudio')
  }
  if (process.platform === 'win32') {
    return path.join(process.env.APPDATA || path.join(os.homedir(), 'AppData/Roaming'), 'CherryStudio')
  }
  return path.join(os.homedir(), '.config/CherryStudio')
}

function cherryProcessRunning() {
  try {
    if (process.platform === 'darwin' || process.platform === 'linux') {
      execFileSync('pgrep', ['-f', 'Cherry Studio'], { stdio: 'ignore' })
      return true
    }
    if (process.platform === 'win32') {
      // tasklist 找不到进程也常 exit 0，必须看输出
      const out = execFileSync('tasklist', ['/FI', 'IMAGENAME eq Cherry Studio.exe', '/NH'], {
        encoding: 'utf8',
        stdio: ['ignore', 'pipe', 'ignore'],
      })
      return /Cherry Studio\.exe/i.test(out)
    }
    return false
  } catch {
    return false
  }
}

/**
 * 必须强杀：优雅退出时旧进程常还在，随后 open 只会前置内存里的旧状态（仍是 gpt-5.4），
 * 磁盘上的闲猫会被下次退出再盖掉。
 */
function quitCherryStudio() {
  try {
    if (process.platform === 'darwin') {
      try {
        execFileSync('osascript', ['-e', 'tell application "Cherry Studio" to quit'], { stdio: 'ignore' })
      } catch {
        /* ignore */
      }
      try {
        execFileSync('killall', ['-9', 'Cherry Studio'], { stdio: 'ignore' })
      } catch {
        /* ignore */
      }
      try {
        execFileSync('pkill', ['-9', '-f', 'Cherry Studio'], { stdio: 'ignore' })
      } catch {
        /* ignore */
      }
    } else if (process.platform === 'win32') {
      try {
        execFileSync('taskkill', ['/IM', 'Cherry Studio.exe', '/F'], { stdio: 'ignore' })
      } catch {
        /* ignore */
      }
    } else {
      try {
        execFileSync('pkill', ['-9', '-f', 'Cherry Studio'], { stdio: 'ignore' })
      } catch {
        /* ignore */
      }
    }
  } catch {
    /* not running */
  }
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms))
}

function decodeChromiumLSValue(buf) {
  const b = Buffer.isBuffer(buf) ? buf : Buffer.from(buf)
  if (b.length === 0) return ''
  // Chromium Local Storage: version byte + UTF-16LE
  if (b[0] === 0x00 || b[0] === 0x01) {
    const rest = b.slice(1)
    if (rest.length >= 2 && rest[1] === 0) return rest.toString('utf16le')
    return rest.toString('utf8')
  }
  if (b.length >= 2 && b[1] === 0) return b.toString('utf16le')
  return b.toString('utf8')
}

function encodeChromiumLSValue(text) {
  return Buffer.concat([Buffer.from([0x00]), Buffer.from(String(text), 'utf16le')])
}

async function fetchGatewayModels(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const key = payload.key
  if (!base || !key) return []
  try {
    const res = await fetch(`${withV1(base)}/models`, {
      headers: { Authorization: `Bearer ${key}`, Accept: 'application/json' },
    })
    if (!res.ok) return []
    const body = await res.json().catch(() => null)
    const list = Array.isArray(body?.data) ? body.data : Array.isArray(body) ? body : []
    return list.map((m) => String(m?.id || '').trim()).filter(Boolean).slice(0, 40)
  } catch {
    return []
  }
}

async function buildCherryProvider(payload) {
  const base = String(payload.base_url || '').replace(/\/$/, '')
  const id = providerId(base, payload.key_id)
  const host = withV1(base).replace(/\/?$/, '/')
  const fetched = await fetchGatewayModels(payload)
  const ids =
    fetched.length > 0
      ? fetched
      : ['claude-sonnet-4-5-20250929', 'claude-haiku-4-5-20251001', 'gpt-4o']
  const models = ids.map((mid) => ({
    id: mid,
    provider: id,
    name: mid,
    group: '闲猫',
  }))
  return {
    id,
    name: payload.provider || '闲猫',
    type: 'openai',
    apiKey: payload.key,
    apiHost: host,
    baseUrl: withV1(base),
    models,
    enabled: true,
    isSystem: false,
  }
}

/**
 * 直接改 Cherry 的 redux-persist（旧版无导入深链时的唯一可靠路径）。
 * 必须先退出 Cherry，否则 leveldb 被锁。
 */
async function writeCherryLocalStorage(payload) {
  const dataDir = cherryDataDir()
  const dbPath = path.join(dataDir, 'Local Storage', 'leveldb')
  if (!fs.existsSync(dbPath)) {
    throw new Error('未找到 Cherry Studio 本地数据，请先打开一次 Cherry 再试')
  }

  quitCherryStudio()
  // 等到进程真正消失，再动 leveldb（否则 open 会前置旧进程 / 写库会被盖掉）
  for (let i = 0; i < 40; i++) {
    if (!cherryProcessRunning()) break
    quitCherryStudio()
    await sleep(150)
  }
  await sleep(500)
  if (cherryProcessRunning()) {
    throw new Error('无法关闭 Cherry Studio，请先手动退出后再点「一键使用」')
  }

  let Level
  try {
    ;({ Level } = require('level'))
  } catch (err) {
    throw new Error(`缺少 level 依赖: ${err.message}`)
  }

  const db = new Level(dbPath, { valueEncoding: 'buffer', keyEncoding: 'buffer' })
  try {
    let targetKey = null
    let rawVal = null
    for await (const [key, value] of db.iterator()) {
      const k = key.toString('binary')
      if (k.includes('persist:cherry-studio')) {
        targetKey = key
        rawVal = value
        break
      }
    }
    if (!targetKey || !rawVal) {
      throw new Error('Cherry 配置里没有 persist:cherry-studio，请先打开一次应用')
    }

    const text = decodeChromiumLSValue(rawVal)
    const root = JSON.parse(text)
    if (!root.llm) throw new Error('Cherry 配置缺少 llm 分段')
    const llm = typeof root.llm === 'string' ? JSON.parse(root.llm) : root.llm
    const providers = Array.isArray(llm.providers) ? llm.providers : []
    const nextProvider = await buildCherryProvider(payload)
    const idx = providers.findIndex((p) => p && p.id === nextProvider.id)
    if (idx >= 0) providers[idx] = { ...providers[idx], ...nextProvider }
    else providers.unshift(nextProvider)

    // 关掉其它自定义供应商，避免一键后又手滑切到 IcDeepSeek 之类
    for (const p of providers) {
      if (!p || p.id === nextProvider.id) continue
      if (p.isSystem) continue
      p.enabled = false
    }

    const model = {
      id: nextProvider.models[0].id,
      name: nextProvider.models[0].name,
      provider: nextProvider.id,
      group: nextProvider.models[0].group,
    }
    llm.providers = providers
    llm.defaultModel = model
    // 话题命名 / 翻译也切到闲猫，避免仍走无密钥的旧供应商
    if (llm.topicNamingModel) llm.topicNamingModel = { ...model }
    if (llm.translateModel) llm.translateModel = { ...model }

    root.llm = JSON.stringify(llm)

    // UI 实际用的是「助手.model」，首页默认选 assistants[0]
    if (root.assistants) {
      const assistantsRoot = typeof root.assistants === 'string' ? JSON.parse(root.assistants) : root.assistants
      const list = Array.isArray(assistantsRoot.assistants) ? assistantsRoot.assistants : []
      const now = new Date().toISOString()
      const topicId = `xm-${Date.now().toString(36)}`
      const xianmaoAssistant = {
        id: nextProvider.id,
        name: payload.provider || '闲猫',
        prompt: '',
        topics: [
          {
            id: topicId,
            assistantId: nextProvider.id,
            createdAt: now,
            updatedAt: now,
            name: '闲猫会话',
            messages: [],
          },
        ],
        messages: [],
        type: 'assistant',
        model: { ...model },
      }
      // 去掉旧闲猫助手，插到最前 → 启动即选中
      const rest = list.filter((a) => a && a.id !== nextProvider.id && a.id !== 'idlecat')
      for (const a of rest) {
        if (!a || typeof a !== 'object') continue
        a.model = { ...model }
        if (a.knowledge_base) delete a.knowledge_base
        if (a.knowledgeBase) delete a.knowledgeBase
      }
      assistantsRoot.assistants = [xianmaoAssistant, ...rest]
      if (assistantsRoot.defaultAssistant && typeof assistantsRoot.defaultAssistant === 'object') {
        assistantsRoot.defaultAssistant.model = { ...model }
        if (assistantsRoot.defaultAssistant.knowledge_base) delete assistantsRoot.defaultAssistant.knowledge_base
      }
      root.assistants = JSON.stringify(assistantsRoot)
    }

    await db.put(targetKey, encodeChromiumLSValue(JSON.stringify(root)))
    return { wrote: [dbPath], providerId: nextProvider.id, modelId: model.id }
  } finally {
    await db.close().catch(() => {})
  }
}

/**
 * urls：仅可自动拉起的协议/本地路径（chatbox://、file://…），禁止放 http(s)。
 * helpUrl：安装失败时的说明页，由 UI 按钮手动打开，绝不自动弹浏览器。
 * 打开策略（由 main 执行）：有 openApp → 只开 App；否则有 urls → 开深链；否则才开 CLI。
 *
 * @param {object} payload
 * @param {string} client
 * @param {{ onProgress?: (p: { message?: string, percent?: number }) => void }} [opts]
 * @returns {Promise<{ mode: string, detail: string, urls: string[], helpUrl?: string, wrote?: string[], openApp?: string, openCli?: string, openedCli?: boolean }>}
 */
async function applyClientConfig(payload, client, opts = {}) {
  const onProgress = (p) => {
    throwIfCancelled()
    if (typeof opts.onProgress === 'function') opts.onProgress(p)
  }
  if (!payload?.key) {
    throw new Error('还没有拿到闲猫连接信息，请回网站再点一次「一键使用」')
  }
  const win = process.platform === 'win32'
  const installed = {
    cherry: !!findInstalledApp(
      win
        ? ['Cherry Studio', path.join('Cherry Studio', 'Cherry Studio.exe')]
        : ['Cherry Studio.app', 'CherryStudio.app'],
    ),
    chatbox: !!findInstalledApp(win ? ['Chatbox', path.join('Chatbox', 'Chatbox.exe')] : ['Chatbox.app', 'chatbox.app']),
    codex: !!findInstalledApp(
      win
        ? [path.join('ChatGPT', 'ChatGPT.exe'), 'ChatGPT', path.join('Programs', 'ChatGPT', 'ChatGPT.exe')]
        : ['ChatGPT.app', 'Codex.app', 'ChatGPT Codex.app'],
    ),
  }

  if (client === 'codex') {
    const r = writeCodex(payload)
    let codexPack
    try {
      codexPack = await ensureCodex(onProgress)
    } catch (err) {
      codexPack = { cli: { error: String(err) }, desktop: { fallbackUrl: 'https://chatgpt.com/download/' } }
    }
    const appPath = codexPack.desktop?.appPath || null
    const cliBin = codexPack.cli?.bin || null
    const cliOk = !!(codexPack.cli?.already || codexPack.cli?.installed || cliBin)
    const deskOk = !!(codexPack.desktop?.already || appPath)
    // 桌面或 CLI 任一可用就不弹安装说明
    const helpUrl =
      deskOk || cliOk
        ? undefined
        : codexPack.desktop?.fallbackUrl || codexPack.cli?.fallbackUrl || 'https://chatgpt.com/download/'
    return {
      mode: 'files',
      detail: deskOk
        ? 'Codex 已配好，即将打开图形版'
        : cliOk
          ? 'Codex 已配好，即将打开（图形版未装上，先用终端）'
          : 'Codex 还没装好，请点「打开安装说明」',
      urls: [],
      helpUrl,
      wrote: r.wrote,
      openApp: appPath || undefined,
      // 有桌面端就不自动开终端
      openCli: appPath ? undefined : cliBin || undefined,
      openedCli: false,
    }
  }

  if (client === 'claude-code') {
    let claudeOk = false
    let claudeDetail = ''
    let bin = null
    let helpUrl
    try {
      const claude = await ensureClaudeCode(onProgress)
      bin = claude.bin || null
      claudeOk = !!(claude.already || claude.installed || bin)
      claudeDetail = claudeOk ? 'Claude Code 已配好，即将打开终端' : 'Claude Code 还没准备好'
    } catch (err) {
      claudeDetail = 'Claude Code 还没装好'
      helpUrl = err?.fallbackUrl || 'https://docs.anthropic.com/en/docs/claude-code/overview'
    }
    throwIfCancelled()
    const r = writeClaudeCode(payload)
    return {
      mode: 'files',
      detail: bin
        ? `${claudeDetail}，即将打开`
        : `${claudeDetail}，请点「打开安装说明」`,
      urls: [],
      helpUrl,
      wrote: r.wrote,
      openCli: bin || undefined,
      openedCli: false,
    }
  }

  if (client === 'obsidian') {
    // Claudian 读 ~/.claude/settings.json；同时装 Claude Code CLI + Obsidian
    let helpUrl
    try {
      await ensureClaudeCode(onProgress)
    } catch {
      /* continue — settings 仍写入 */
    }
    await yieldEventLoop()
    let ob
    try {
      ob = await ensureObsidian(onProgress)
    } catch (err) {
      helpUrl = 'https://obsidian.md/download'
      ob = { error: err instanceof Error ? err.message : String(err) }
    }
    throwIfCancelled()
    if (!payload.key) throw new Error('还没有拿到闲猫连接信息，请回网站再点一次「一键使用」')
    const r = writeClaudeCode(payload)
    const ok = !!(ob.appPath || ob.already)
    if (!ok && !helpUrl) helpUrl = 'https://obsidian.md/download'
    return {
      mode: 'files',
      detail: ok
        ? 'Obsidian 已写入闲猫（Claude settings）；请在库里启用 Claudian 并打开「加载用户 Claude 设置」'
        : '请先安装 Obsidian，并装上 Claudian 插件',
      urls: [],
      helpUrl: ok ? undefined : helpUrl || 'https://github.com/YishenTu/claudian',
      wrote: r.wrote,
      openApp: ob.appPath || undefined,
    }
  }

  if (client === 'claude-desktop') {
    let desk
    let helpUrl
    try {
      desk = await ensureClaudeDesktop(onProgress)
    } catch (err) {
      helpUrl = 'https://claude.ai/download'
      desk = { error: err instanceof Error ? err.message : String(err) }
    }
    throwIfCancelled()
    if (!payload.key) throw new Error('还没有拿到闲猫连接信息，请回网站再点一次「一键使用」')
    const r = writeClaudeDesktop(payload)
    const ok = !!(desk.appPath || desk.already)
    return {
      mode: 'files',
      detail: ok
        ? 'Claude Desktop 已配好闲猫（3P 网关），即将打开；若仍走官方登录请完全退出后重开'
        : 'Claude Desktop 还没装好，请点「打开安装说明」',
      urls: [],
      helpUrl: ok ? undefined : helpUrl || 'https://claude.ai/download',
      wrote: r.wrote,
      openApp: desk.appPath || undefined,
    }
  }

  if (client === 'pi') {
    let pi
    try {
      pi = await ensurePi(onProgress)
    } catch (err) {
      pi = { error: err instanceof Error ? err.message : String(err), fallbackUrl: err?.fallbackUrl || 'https://pi.dev' }
    }
    const r = writePi(payload)
    const bin = pi.bin || null
    return {
      mode: 'files',
      detail: bin ? 'Pi 已配好，即将打开终端' : 'Pi 还没装好，请点「打开安装说明」',
      urls: [],
      helpUrl: bin ? undefined : pi.fallbackUrl || 'https://pi.dev',
      wrote: r.wrote,
      openCli: bin || undefined,
      openedCli: false,
    }
  }

  if (client === 'hermes') {
    let hermes
    try {
      hermes = await ensureHermes(onProgress)
    } catch (err) {
      hermes = {
        error: err instanceof Error ? err.message : String(err),
        fallbackUrl: err?.fallbackUrl || 'https://hermes-agent.nousresearch.com/docs/getting-started/installation',
      }
    }
    const r = writeHermes(payload)
    const bin = hermes.bin || null
    return {
      mode: 'files',
      detail: bin ? 'Hermes 已配好，即将打开终端' : 'Hermes 还没装好，请点「打开安装说明」',
      urls: [],
      helpUrl: bin
        ? undefined
        : hermes.fallbackUrl || 'https://hermes-agent.nousresearch.com/docs/getting-started/installation',
      wrote: r.wrote,
      openCli: bin || undefined,
      openedCli: false,
    }
  }

  if (client === 'openclaw') {
    let claw
    try {
      claw = await ensureOpenClawCLI(onProgress)
    } catch (err) {
      claw = { error: err instanceof Error ? err.message : String(err), fallbackUrl: err?.fallbackUrl }
    }
    const r = writeOpenClaw(payload)
    const bin = claw.bin || null
    return {
      mode: 'files',
      detail: bin || claw.already ? 'OpenClaw 已配好，即将打开终端' : 'OpenClaw 还没装好，请点「打开安装说明」',
      urls: [],
      helpUrl: bin ? undefined : claw.fallbackUrl || 'https://github.com/openclaw/openclaw',
      wrote: r.wrote,
      openCli: bin || undefined,
      openedCli: false,
    }
  }

  if (client === 'paseo') {
    // Paseo 调度本机 Agent：先装/配 Claude Code、Codex、Pi，再装并打开 Paseo 桌面端
    const wrote = []
    try {
      await ensureClaudeCode(onProgress)
    } catch {
      /* continue */
    }
    await yieldEventLoop()
    wrote.push(...writeClaudeCode(payload).wrote)
    try {
      await ensureCodex(onProgress)
    } catch {
      /* continue */
    }
    await yieldEventLoop()
    wrote.push(...writeCodex(payload).wrote)
    try {
      await ensurePi(onProgress)
    } catch {
      /* continue */
    }
    await yieldEventLoop()
    wrote.push(...writePi(payload).wrote)

    let paseo
    try {
      paseo = await ensurePaseo(onProgress)
    } catch (err) {
      paseo = {
        error: err instanceof Error ? err.message : String(err),
        fallbackUrl: err?.fallbackUrl || 'https://paseo.sh/download',
      }
    }
    const ok = !!(paseo.appPath || paseo.already)
    return {
      mode: 'files',
      detail: ok ? 'Paseo 已配好闲猫，即将打开' : 'Paseo 还没装好，请点「打开安装说明」',
      urls: [],
      helpUrl: ok ? undefined : paseo.fallbackUrl || 'https://paseo.sh/download',
      wrote,
      openApp: paseo.appPath || undefined,
    }
  }

  if (client === 'zcode') {
    let zc
    try {
      zc = await ensureZCode(onProgress)
    } catch (err) {
      zc = {
        error: err instanceof Error ? err.message : String(err),
        fallbackUrl: err?.fallbackUrl || 'https://zcode.z.ai/en/docs/install',
      }
    }
    throwIfCancelled()
    const r = writeZCodeRef(payload)
    const ok = !!(zc.appPath || zc.already)
    return {
      mode: 'files',
      detail: ok
        ? 'ZCode 已写入闲猫并设为默认模型，即将打开'
        : 'ZCode 还没装好，配置已写入；请点「打开安装说明」',
      urls: [],
      helpUrl: ok ? undefined : zc.fallbackUrl || 'https://zcode.z.ai/en/docs/install',
      wrote: r.wrote,
      openApp: zc.appPath || undefined,
    }
  }

  if (client === 'deepseek-harness') {
    const webUrl = 'http://127.0.0.1:3080'
    let pack
    try {
      pack = await ensureDeepSeekHarness(onProgress)
    } catch (err) {
      return {
        mode: 'download',
        detail: 'DeepSeek Harness 还没装好，请点「打开安装说明」',
        urls: [],
        helpUrl: err?.fallbackUrl || 'https://github.com/deepseek-ai/deepseek-harness',
      }
    }
    const r = writeDeepSeekHarness(payload)
    const args = [...(pack.args || []), 'web', '--no-open']
    const started = spawnDetached(pack.bin, args, { env: { DSH_HOME: dshHomeDir() } })
    let ready = false
    if (started) {
      if (typeof onProgress === 'function') onProgress({ message: '正在打开 DeepSeek Harness…' })
      ready = await waitHttpOk(webUrl, 60000)
    }
    return {
      mode: 'files',
      detail: ready
        ? 'DeepSeek Harness 已配好闲猫，即将打开'
        : started
          ? 'DeepSeek Harness 已配好，若没自动打开请点下方按钮'
          : 'DeepSeek Harness 启动失败，请点「打开安装说明」',
      urls: [],
      helpUrl: ready || started ? undefined : 'https://github.com/deepseek-ai/deepseek-harness',
      wrote: r.wrote,
      openWeb: ready || started ? webUrl : undefined,
    }
  }

  if (client === 'cherry-studio') {
    if (!installed.cherry) {
      try {
        const cherry = await ensureCherry(onProgress)
        if (cherry.fallbackUrl && !cherry.appPath) {
          return {
            mode: 'download',
            detail: 'Cherry Studio 还没装好，请点「打开安装说明」',
            urls: [],
            helpUrl: cherry.fallbackUrl,
          }
        }
      } catch (err) {
        return {
          mode: 'download',
          detail: 'Cherry Studio 安装失败，请点「打开安装说明」',
          urls: [],
          helpUrl: 'https://github.com/CherryHQ/cherry-studio/releases/latest',
        }
      }
    }

    // 旧版（0.9.x）深链不会导入；先写 Local Storage，再由 main 打开 App。
    try {
      const r = await writeCherryLocalStorage(payload)
      const appPath = findInstalledApp(
        process.platform === 'win32'
          ? ['Cherry Studio', path.join('Cherry Studio', 'Cherry Studio.exe')]
          : ['Cherry Studio.app', 'CherryStudio.app'],
      )
      return {
        mode: 'files',
        detail: 'Cherry Studio 已配好闲猫，即将打开',
        urls: [],
        helpUrl: appPath ? undefined : 'https://github.com/CherryHQ/cherry-studio/releases/latest',
        wrote: r.wrote,
        openApp: appPath || undefined,
      }
    } catch (err) {
      return {
        mode: 'deeplink',
        detail: '正在用另一种方式导入闲猫…',
        urls: [buildCherryImportURL(payload)],
      }
    }
  }

  if (client === 'chatbox') {
    const link = buildChatboxImportURL(payload)
    if (!installed.chatbox) {
      try {
        await ensureChatbox(onProgress)
      } catch (err) {
        return {
          mode: 'download',
          detail: 'Chatbox 还没装好，请点「打开安装说明」',
          urls: [],
          helpUrl: 'https://chatboxai.app/',
        }
      }
    }
    return {
      mode: 'deeplink',
      detail: 'Chatbox 已配好闲猫，请在弹出窗口里确认一下',
      urls: [link],
    }
  }

  const { applyExtraAgent } = require('./extra-agents.cjs')
  const extra = await applyExtraAgent(payload, client, onProgress)
  if (extra) return extra

  return { mode: 'download', detail: '未知客户端', urls: [], helpUrl: undefined }
}

async function openUrls(urls) {
  for (const url of urls) {
    if (!url) continue
    if (url.startsWith('file://')) {
      let filePath = decodeURIComponent(url.replace(/^file:\/\//, ''))
      // Windows file:///C:/... → C:/...
      if (process.platform === 'win32' && /^\/[A-Za-z]:\//.test(filePath)) {
        filePath = filePath.slice(1)
      }
      try {
        if (process.platform === 'darwin' && /\.app\/?$/i.test(filePath)) {
          execFileSync('open', ['-a', filePath], { stdio: 'ignore' })
          continue
        }
        if (process.platform === 'win32' && /\.(exe|msi)$/i.test(filePath)) {
          openInstalledApp(filePath)
          continue
        }
        await shell.openPath(filePath)
        continue
      } catch {
        /* fall through */
      }
    }
    await shell.openExternal(url)
  }
}

module.exports = {
  applyClientConfig,
  openUrls,
  buildCherryImportURL,
  buildChatboxImportURL,
  writeCherryLocalStorage,
  withV1,
}
