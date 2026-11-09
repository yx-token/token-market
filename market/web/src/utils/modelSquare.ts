import type { MarketItem, PoolItem, Price } from '@/api/types'
import { brandIconFor } from '@/market/config'

/** 市场挂单粗匹配用的能力族（与卖家 platform 对应） */
export type ModelFamilyId =
  | 'claude'
  | 'openai'
  | 'gemini'
  | 'minimax'
  | 'zhipu'
  | 'kimi'
  | 'grok'
  | 'deepseek'
  | 'qwen'
  | 'other'

export type ModelCapability = 'text' | 'image' | 'video' | 'other'

export type ModelChannelMeta = {
  id: string
  title: string
  hint: string
  plat: string
  accent: string
  tint: string
  icon?: string
}

export type ModelRateRow = {
  model: string
  channel: string
  family: ModelFamilyId
  capability: ModelCapability
  official: Price
  /** 市场在售最低倍率；无货为 null */
  bestMult: number | null
  listings: number
}

export type ModelChannelGroup = ModelChannelMeta & {
  rows: ModelRateRow[]
  bestMult: number | null
}

/** @deprecated 兼容旧名 */
export type ModelFamilyGroup = ModelChannelGroup
export type ModelFamilyMeta = ModelChannelMeta

const ACCENTS = ['#c45c26', '#1a7f5a', '#3b6fd9', '#2f8f6b', '#b8860b', '#5b6b8c', '#444', '#4a6fa5', '#6b5b95', '#8b4513', '#2a9d8f', '#e76f51']

/** LiteLLM provider → 广场渠道展示名（全球主流 API 渠道） */
const CHANNEL_LABELS: Record<string, { title: string; hint: string; plat?: string }> = {
  openai: { title: 'OpenAI', hint: 'GPT / Codex / Images', plat: 'ChatGPT' },
  chatgpt: { title: 'ChatGPT', hint: 'OpenAI ChatGPT', plat: 'ChatGPT' },
  'text-completion-openai': { title: 'OpenAI Completions', hint: 'Legacy completions' },
  anthropic: { title: 'Anthropic', hint: 'Claude Messages', plat: 'Claude' },
  gemini: { title: 'Google Gemini', hint: 'Gemini API', plat: 'Gemini' },
  vertex_ai: { title: 'Vertex AI', hint: 'Google Cloud', plat: 'Gemini' },
  'vertex_ai-language-models': { title: 'Vertex Language', hint: 'Google Cloud LLM', plat: 'Gemini' },
  'vertex_ai-embedding-models': { title: 'Vertex Embedding', hint: 'Google embeddings' },
  'vertex_ai-anthropic_models': { title: 'Vertex Claude', hint: 'Claude on Vertex', plat: 'Claude' },
  'vertex_ai-mistral_models': { title: 'Vertex Mistral', hint: 'Mistral on Vertex' },
  'vertex_ai-llama_models': { title: 'Vertex Llama', hint: 'Llama on Vertex' },
  'vertex_ai-ai21_models': { title: 'Vertex AI21', hint: 'AI21 on Vertex' },
  'vertex_ai-video-models': { title: 'Vertex Video', hint: 'Google video models' },
  xai: { title: 'xAI Grok', hint: 'Grok API', plat: 'Grok' },
  deepseek: { title: 'DeepSeek', hint: '深度求索', plat: 'DeepSeek' },
  moonshot: { title: 'Moonshot Kimi', hint: '月之暗面', plat: 'Kimi' },
  zhipu: { title: '智谱 GLM', hint: 'Zhipu AI', plat: '智谱' },
  zai: { title: '智谱 Z.ai', hint: 'Z.ai', plat: '智谱' },
  minimax: { title: 'MiniMax', hint: '海螺 / Coding', plat: 'MiniMax' },
  mistral: { title: 'Mistral', hint: 'Mistral AI' },
  cohere: { title: 'Cohere', hint: 'Command / Embed' },
  cohere_chat: { title: 'Cohere Chat', hint: 'Cohere chat' },
  groq: { title: 'Groq', hint: 'Low-latency inference' },
  together_ai: { title: 'Together AI', hint: 'Open models platform' },
  fireworks_ai: { title: 'Fireworks', hint: 'Fast open-model inference' },
  'fireworks_ai-embedding-models': { title: 'Fireworks Embed', hint: 'Fireworks embeddings' },
  perplexity: { title: 'Perplexity', hint: 'Search-augmented LLM' },
  deepinfra: { title: 'DeepInfra', hint: 'Open-model hosting' },
  replicate: { title: 'Replicate', hint: 'Model hosting' },
  cloudflare: { title: 'Cloudflare AI', hint: 'Workers AI' },
  dashscope: { title: 'DashScope 通义', hint: '阿里云百炼' },
  qwen_ai_platform: { title: 'Qwen Platform', hint: '通义千问' },
  qwencloud: { title: 'Qwen Cloud', hint: 'Qwen Cloud' },
  volcengine: { title: '火山方舟', hint: '字节跳动' },
  tencent: { title: '腾讯混元', hint: 'Tencent Hunyuan' },
  meta: { title: 'Meta Llama', hint: 'Llama API' },
  ai21: { title: 'AI21', hint: 'Jamba / Jurassic' },
  stability: { title: 'Stability', hint: 'Stable Diffusion' },
  runwayml: { title: 'Runway', hint: 'Gen video' },
  elevenlabs: { title: 'ElevenLabs', hint: 'Voice / TTS' },
  voyage: { title: 'Voyage', hint: 'Embeddings' },
  cerebras: { title: 'Cerebras', hint: 'High throughput' },
  sambanova: { title: 'SambaNova', hint: 'Enterprise inference' },
  hyperbolic: { title: 'Hyperbolic', hint: 'GPU inference' },
  novita: { title: 'Novita', hint: 'Open-model API' },
  fal_ai: { title: 'fal.ai', hint: 'Media models' },
  bedrock: { title: 'AWS Bedrock', hint: 'Amazon Bedrock' },
  bedrock_converse: { title: 'Bedrock Converse', hint: 'Bedrock Converse API' },
  bedrock_mantle: { title: 'Bedrock Mantle', hint: 'Bedrock Mantle' },
  azure: { title: 'Azure OpenAI', hint: 'Microsoft Azure' },
  azure_ai: { title: 'Azure AI', hint: 'Azure AI Foundry' },
  ollama: { title: 'Ollama', hint: 'Local models' },
  friendliai: { title: 'Friendli', hint: 'FriendliAI' },
  databricks: { title: 'Databricks', hint: 'Mosaic / Foundation' },
  watsonx: { title: 'IBM watsonx', hint: 'IBM watsonx.ai' },
  snowflake: { title: 'Snowflake', hint: 'Cortex AI' },
  lambda_ai: { title: 'Lambda', hint: 'Lambda Inference' },
  anyscale: { title: 'Anyscale', hint: 'Ray / Endpoints' },
  aihubmix: { title: 'AIHubMix', hint: 'Multi-provider hub' },
  github_copilot: { title: 'GitHub Copilot', hint: 'Copilot models' },
  deepgram: { title: 'Deepgram', hint: 'Speech / ASR' },
  black_forest_labs: { title: 'Black Forest Labs', hint: 'FLUX' },
  nebius: { title: 'Nebius', hint: 'Nebius AI' },
  nscale: { title: 'nscale', hint: 'nscale inference' },
  ovhcloud: { title: 'OVHcloud', hint: 'OVH AI Endpoints' },
  scaleway: { title: 'Scaleway', hint: 'Scaleway Generative' },
  vercel_ai_gateway: { title: 'Vercel AI', hint: 'AI Gateway' },
  opencode_go: { title: 'OpenCode Go', hint: 'OpenCode' },
  gigachat: { title: 'GigaChat', hint: 'Sber GigaChat' },
  other: { title: '其他渠道', hint: '未归类' },
}

function tintOf(accent: string) {
  return accent.startsWith('#') ? `${accent}33` : 'rgba(220, 220, 220, 0.4)'
}

export function channelIdOf(provider?: string | null, model?: string): string {
  const p = (provider ?? '').trim().toLowerCase()
  if (p && CHANNEL_LABELS[p]) return p
  if (p) return p
  const family = familyOfModel(model ?? '')
  if (family === 'claude') return 'anthropic'
  if (family === 'openai') return 'openai'
  if (family === 'gemini') return 'gemini'
  if (family === 'minimax') return 'minimax'
  if (family === 'zhipu') return 'zhipu'
  if (family === 'kimi') return 'moonshot'
  if (family === 'grok') return 'xai'
  if (family === 'deepseek') return 'deepseek'
  if (family === 'qwen') return 'dashscope'
  return 'other'
}

export function channelMeta(id: string, index = 0): ModelChannelMeta {
  const known = CHANNEL_LABELS[id]
  const accent = ACCENTS[Math.abs(index) % ACCENTS.length]!
  if (known) {
    return {
      id,
      title: known.title,
      hint: known.hint,
      plat: known.plat ?? '',
      accent,
      tint: tintOf(accent),
      icon: known.plat ? brandIconFor(known.plat) : undefined,
    }
  }
  const title = id
    .split(/[-_]/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
  return { id, title: title || '其他渠道', hint: id, plat: '', accent, tint: tintOf(accent) }
}

export function familyOfProvider(provider?: string | null): ModelFamilyId | null {
  const p = (provider ?? '').trim().toLowerCase()
  if (!p) return null
  if (p === 'anthropic' || p.includes('anthropic')) return 'claude'
  if (p === 'openai' || p === 'azure' || p === 'chatgpt' || p.includes('openai')) return 'openai'
  if (p === 'gemini' || p === 'google' || p.startsWith('vertex_ai')) return 'gemini'
  if (p === 'minimax') return 'minimax'
  if (p === 'zhipu' || p === 'zai') return 'zhipu'
  if (p === 'moonshot' || p === 'kimi') return 'kimi'
  if (p === 'xai' || p === 'grok') return 'grok'
  if (p === 'deepseek') return 'deepseek'
  if (p.includes('qwen') || p === 'dashscope' || p === 'alibaba') return 'qwen'
  return null
}

export function familyOfModel(model: string, provider?: string | null): ModelFamilyId {
  const fromProvider = familyOfProvider(provider)
  if (fromProvider) return fromProvider
  const m = model.trim().toLowerCase()
  if (m.startsWith('claude') || m.includes('anthropic')) return 'claude'
  if (
    m.startsWith('gpt') ||
    m.startsWith('o1') ||
    m.startsWith('o3') ||
    m.startsWith('o4') ||
    m.includes('chatgpt') ||
    m.startsWith('codex') ||
    m.includes('dall-e') ||
    m.startsWith('gpt-image')
  ) {
    return 'openai'
  }
  if (m.startsWith('gemini') || m.startsWith('gemma') || m.startsWith('veo')) return 'gemini'
  if (m.includes('minimax') || m.startsWith('abab') || m.startsWith('hailuo')) return 'minimax'
  if (m.startsWith('glm') || m.includes('chatglm') || m.includes('zhipu')) return 'zhipu'
  if (m.startsWith('kimi') || m.includes('moonshot')) return 'kimi'
  if (m.startsWith('grok')) return 'grok'
  if (m.includes('deepseek')) return 'deepseek'
  if (m.includes('qwen') || m.startsWith('qwq') || m.startsWith('wan')) return 'qwen'
  return 'other'
}

export function familyMeta(id: ModelFamilyId): ModelChannelMeta {
  return channelMeta(
    id === 'claude'
      ? 'anthropic'
      : id === 'kimi'
        ? 'moonshot'
        : id === 'grok'
          ? 'xai'
          : id === 'qwen'
            ? 'dashscope'
            : id,
  )
}

export function capabilityOfModel(model: string, mode?: string | null): ModelCapability {
  const md = (mode ?? '').trim().toLowerCase()
  const m = model.trim().toLowerCase()
  if (md.includes('image') || md === 'img') return 'image'
  if (md.includes('video')) return 'video'
  if (
    m.includes('image') ||
    m.includes('dall-e') ||
    m.includes('flux') ||
    m.includes('ideogram') ||
    m.includes('seedream') ||
    m.includes('recraft') ||
    m.includes('imagen') ||
    m.includes('nano-banana') ||
    m.includes('gpt-image') ||
    m.includes('stable-diffusion')
  ) {
    return 'image'
  }
  if (
    m.includes('video') ||
    m.includes('veo') ||
    m.includes('kling') ||
    m.includes('seedance') ||
    m.includes('hailuo') ||
    m.includes('wan-') ||
    m.startsWith('wan') ||
    m.includes('ltxv') ||
    m.includes('runway') ||
    m.includes('sora')
  ) {
    return 'video'
  }
  if (md === 'chat' || md === 'completion' || md === 'responses' || md === 'embedding' || md === 'moderations' || !md) {
    return 'text'
  }
  if (md.includes('audio') || md.includes('speech') || md.includes('tts') || md.includes('asr')) return 'other'
  return 'text'
}

type RateOffer = { mult: number; models: string[] | null | undefined; platform: string }

function collectOffers(listings: MarketItem[], pools: PoolItem[]): RateOffer[] {
  const out: RateOffer[] = []
  for (const it of listings) {
    if (!Number.isFinite(it.price) || it.price <= 0) continue
    if (it.sku_kind === 'pack' || it.mode === 'pack') continue
    out.push({ mult: it.price, models: it.models, platform: it.platform })
  }
  for (const p of pools) {
    if (!p.market_published) continue
    if (!Number.isFinite(p.price) || p.price <= 0) continue
    out.push({ mult: p.price, models: null, platform: p.platform })
  }
  return out
}

function offerCovers(offer: RateOffer, model: string, family: ModelFamilyId): boolean {
  const models = offer.models
  if (models && models.length > 0) {
    return models.some((m) => m.toLowerCase() === model.toLowerCase())
  }
  const plat = offer.platform.toLowerCase()
  if (family === 'claude') return plat === 'anthropic' || plat === 'antigravity' || plat === 'composite'
  if (family === 'openai') return plat === 'openai' || plat === 'composite'
  if (family === 'gemini') return plat === 'gemini' || plat === 'composite'
  if (family === 'minimax') return plat === 'minimax' || plat === 'composite'
  if (family === 'zhipu') return plat === 'zhipu' || plat === 'composite'
  if (family === 'kimi') return plat === 'kimi' || plat === 'composite'
  if (family === 'grok') return plat === 'grok' || plat === 'composite'
  return plat === 'composite'
}

/** 按官方平台渠道分组：官方参考价 × 市场最低在售倍率 */
export function buildModelSquare(prices: Price[], listings: MarketItem[], pools: PoolItem[]): ModelChannelGroup[] {
  const offers = collectOffers(listings, pools)
  const byChannel = new Map<string, ModelRateRow[]>()

  for (const official of prices) {
    if (!official?.model) continue
    const channel = channelIdOf(official.provider, official.model)
    const family = familyOfModel(official.model, official.provider)
    const capability = capabilityOfModel(official.model, official.mode)
    const matched = offers.filter((o) => offerCovers(o, official.model, family))
    const bestMult = matched.length ? Math.min(...matched.map((o) => o.mult)) : null
    const row: ModelRateRow = {
      model: official.model,
      channel,
      family,
      capability,
      official,
      bestMult,
      listings: matched.length,
    }
    const list = byChannel.get(channel) ?? []
    list.push(row)
    byChannel.set(channel, list)
  }

  const ids = [...byChannel.keys()].sort((a, b) => {
    const ta = channelMeta(a).title
    const tb = channelMeta(b).title
    return ta.localeCompare(tb, 'zh-Hans')
  })

  return ids.map((id, index) => {
    const rows = (byChannel.get(id) ?? []).sort((a, b) => a.model.localeCompare(b.model))
    const mults = rows.map((r) => r.bestMult).filter((v): v is number => v !== null)
    return {
      ...channelMeta(id, index),
      rows,
      bestMult: mults.length ? Math.min(...mults) : null,
    }
  })
}

export function applyMult(official: number, mult: number | null): number | null {
  if (mult === null || !Number.isFinite(official)) return null
  return official * mult
}
