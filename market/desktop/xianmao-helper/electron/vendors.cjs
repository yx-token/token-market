/**
 * 每个 Agent 可用的保障池厂商；与后端 oneclick_agents.go 对齐。
 * null = 多厂商（任意已上架池），未选时默认 openai / ChatGPT。
 */
const DEFAULT_MULTI = 'openai'

const AGENT_VENDORS = {
  codex: ['openai'],
  'claude-code': ['anthropic'],
  'claude-desktop': ['anthropic'],
  obsidian: ['anthropic'],
  'deepseek-harness': ['deepseek'],
  gemini: ['gemini'],
  'kimi-code': ['kimi', 'openai'],
  opencode: null,
  cline: null,
  aider: null,
  'qwen-code': null,
  workbuddy: null,
  codebuddy: null,
  trae: null,
  goose: null,
  crush: null,
  'kilo-code': null,
  pi: null,
  hermes: null,
  openclaw: null,
  paseo: null,
  zcode: null,
  'cherry-studio': null,
  chatbox: null,
}

const PLATFORM_LABELS = {
  openai: 'ChatGPT',
  anthropic: 'Claude',
  gemini: 'Gemini',
  deepseek: 'DeepSeek',
  kimi: 'Kimi',
  grok: 'Grok',
  minimax: 'MiniMax',
  zhipu: '智谱',
  composite: '综合保障池',
  antigravity: 'Antigravity',
}

function preferredVendor(agentId) {
  const v = Object.prototype.hasOwnProperty.call(AGENT_VENDORS, agentId) ? AGENT_VENDORS[agentId] : null
  if (Array.isArray(v) && v.length === 1) return v[0]
  if (Array.isArray(v) && v.length >= 2) {
    return v.includes(DEFAULT_MULTI) ? DEFAULT_MULTI : v[0]
  }
  return DEFAULT_MULTI
}

function isMultiVendor(agentId) {
  const v = Object.prototype.hasOwnProperty.call(AGENT_VENDORS, agentId) ? AGENT_VENDORS[agentId] : null
  return v == null || (Array.isArray(v) && v.length >= 2)
}

function platformLabel(platform) {
  return PLATFORM_LABELS[platform] || platform
}

/** 默认模型：写入客户端配置时按厂商挑一个常用 ID。 */
function defaultModelForPlatform(platform) {
  switch (String(platform || '').toLowerCase()) {
    case 'anthropic':
      return 'claude-sonnet-4-5'
    case 'gemini':
      return 'gemini-2.5-pro'
    case 'deepseek':
      return 'deepseek-chat'
    case 'kimi':
      return 'kimi-k2-instruct'
    case 'grok':
      return 'grok-3'
    default:
      return 'gpt-5.5'
  }
}

function modelLooksLikePlatform(modelId, platform) {
  const id = String(modelId || '').toLowerCase()
  if (!id) return false
  switch (String(platform || '').toLowerCase()) {
    case 'openai':
      return /^(gpt-|o[1-9]|chatgpt|codex)/.test(id) || id.includes('gpt')
    case 'anthropic':
      return id.includes('claude')
    case 'gemini':
      return id.includes('gemini')
    case 'deepseek':
      return id.includes('deepseek')
    case 'kimi':
      return id.includes('kimi') || id.includes('moonshot')
    case 'grok':
      return id.includes('grok')
    default:
      return true
  }
}

/** 从 /v1/models 列表里挑一个匹配厂商的模型；没有则返回 null。 */
function pickModelForPlatform(modelIds, platform) {
  const ids = Array.isArray(modelIds) ? modelIds.map(String) : []
  const matched = ids.filter((id) => modelLooksLikePlatform(id, platform))
  if (!matched.length) return null
  const prefer = defaultModelForPlatform(platform)
  if (matched.includes(prefer)) return prefer
  const soft = matched.find((id) => id.toLowerCase().includes(prefer.split('-')[0]))
  return soft || matched[0]
}

module.exports = {
  AGENT_VENDORS,
  PLATFORM_LABELS,
  DEFAULT_MULTI,
  preferredVendor,
  isMultiVendor,
  platformLabel,
  defaultModelForPlatform,
  modelLooksLikePlatform,
  pickModelForPlatform,
}
