/** 市场发现配置（对齐 docs/prototypes/market-config.js） */
import type { InspectionCheckResult } from '@/api/types'
import type { DetectionStatus, ModelCheck } from '@/api/market-detection'

export type MarketAction = {
  plat?: string
  q?: string
  tag?: string
  kindTab?: string
  href?: string
}

export type CategoryGroup = {
  id: string
  enabled?: boolean
  sort?: number
  icon: string
  labels: { text: string; action: MarketAction }[]
  sections: { title: string; items: { text: string; action: MarketAction }[] }[]
}

export type HotSearch = {
  id: string
  label: string
  enabled?: boolean
  sort?: number
  action: MarketAction
}

export type MarketDiscoveryConfig = {
  version: number
  hot_searches: HotSearch[]
  hot_bar: { enabled: boolean }
  feed: { pin_pool_first: boolean }
}

export const marketConfigDefaults: MarketDiscoveryConfig = {
  version: 2,
  hot_searches: [
    { id: 'hs_gpt', label: 'ChatGPT Plus', enabled: true, sort: 10, action: { q: 'ChatGPT' } },
    { id: 'hs_claude', label: 'Claude Max', enabled: true, sort: 20, action: { q: 'Claude' } },
    { id: 'hs_kimi', label: 'Kimi Coding', enabled: true, sort: 30, action: { q: 'Kimi' } },
    { id: 'hs_low', label: '低倍率', enabled: true, sort: 40, action: { tag: 'low_rate' } },
    { id: 'hs_pool', label: '保障池', enabled: true, sort: 50, action: { plat: '保障池' } },
    { id: 'hs_pack', label: '订阅套餐', enabled: true, sort: 60, action: { kindTab: 'pack' } },
  ],
  hot_bar: { enabled: true },
  feed: { pin_pool_first: true },
}

export function enabledSorted<T extends { enabled?: boolean; sort?: number }>(list: T[]): T[] {
  return (list || []).filter((x) => x.enabled !== false).sort((a, b) => (a.sort || 0) - (b.sort || 0))
}

/** 展示名 → API platform */
export const platToApi: Record<string, string> = {
  Claude: 'anthropic',
  ChatGPT: 'openai',
  Gemini: 'gemini',
  Kimi: 'kimi',
  智谱: 'zhipu',
  MiniMax: 'minimax',
  Grok: 'grok',
  Antigravity: 'antigravity',
  OpenCode: 'opencode_go',
}

/** 市场信息条：全球主流 AI / 模型品牌图标（本地 SVG，来自 lobe-icons） */
export type PlatBrand = {
  /** 过滤用展示名；空则用 q 搜 label */
  plat: string
  label: string
  icon: string
  /** 可选：覆盖默认过滤动作 */
  q?: string
}

const brand = (file: string, label: string, plat = '', q = ''): PlatBrand => ({
  icon: `/assets/brands/${file}.svg`,
  label,
  plat,
  q: q || undefined,
})

/** 图标墙完整清单（按常见认知分组排序） */
export const platBrands: PlatBrand[] = [
  // 对话大模型
  brand('claude', 'Claude', 'Claude'),
  brand('openai', 'OpenAI', 'ChatGPT'),
  brand('gemini', 'Gemini', 'Gemini'),
  brand('deepseek', 'DeepSeek', 'DeepSeek'),
  brand('qwen', 'Qwen', '', 'Qwen'),
  brand('kimi', 'Kimi', 'Kimi'),
  brand('moonshot', 'Moonshot', 'Kimi'),
  brand('zhipu', '智谱', '智谱'),
  brand('chatglm', 'ChatGLM', '智谱'),
  brand('minimax', 'MiniMax', 'MiniMax'),
  brand('grok', 'Grok', 'Grok'),
  brand('xai', 'xAI', 'Grok'),
  brand('meta', 'Meta', '', 'Llama'),
  brand('mistral', 'Mistral', 'Mistral'),
  brand('yi', '零一万物', '', 'Yi'),
  brand('baichuan', '百川', '', '百川'),
  brand('doubao', '豆包', '', '豆包'),
  brand('hunyuan', '混元', '', '混元'),
  brand('wenxin', '文心', '', '文心'),
  brand('spark', '星火', '', '星火'),
  brand('stepfun', '阶跃', '', 'Step'),
  brand('internlm', '书生', '', 'InternLM'),
  brand('sensenova', '日日新', '', 'SenseNova'),
  brand('tiangong', '天工', '', '天工'),
  brand('skywork', 'Skywork', '', 'Skywork'),
  brand('hailuo', '海螺', '', '海螺'),
  brand('sophnet', 'SophNet', '', 'SophNet'),
  // 海外厂商 / 云
  brand('cohere', 'Cohere', '', 'Cohere'),
  brand('ai21', 'AI21', '', 'AI21'),
  brand('inflection', 'Inflection', '', 'Inflection'),
  brand('perplexity', 'Perplexity', '', 'Perplexity'),
  brand('alephalpha', 'Aleph Alpha', '', 'Aleph'),
  brand('upstage', 'Upstage', '', 'Upstage'),
  brand('huggingface', 'Hugging Face', '', 'HuggingFace'),
  brand('ollama', 'Ollama', '', 'Ollama'),
  brand('openrouter', 'OpenRouter', '', 'OpenRouter'),
  brand('groq', 'Groq', '', 'Groq'),
  brand('together', 'Together', '', 'Together'),
  brand('fireworks', 'Fireworks', '', 'Fireworks'),
  brand('replicate', 'Replicate', '', 'Replicate'),
  brand('deepinfra', 'DeepInfra', '', 'DeepInfra'),
  brand('siliconcloud', 'SiliconCloud', '', 'SiliconCloud'),
  brand('modelscope', 'ModelScope', '', 'ModelScope'),
  brand('fal', 'fal', '', 'fal'),
  brand('cerebras', 'Cerebras', '', 'Cerebras'),
  brand('sambanova', 'SambaNova', '', 'SambaNova'),
  brand('anyscale', 'Anyscale', '', 'Anyscale'),
  // 云与基建
  brand('google', 'Google', 'Gemini'),
  brand('deepmind', 'DeepMind', 'Gemini'),
  brand('nvidia', 'NVIDIA', '', 'NVIDIA'),
  brand('microsoft', 'Microsoft', '', 'Microsoft'),
  brand('azure', 'Azure', '', 'Azure'),
  brand('aws', 'AWS', '', 'Bedrock'),
  brand('bedrock', 'Bedrock', '', 'Bedrock'),
  brand('vertexai', 'Vertex AI', '', 'Vertex'),
  brand('ibm', 'IBM', '', 'IBM'),
  brand('huawei', '华为', '', '华为'),
  brand('tencentcloud', '腾讯云', '', '腾讯云'),
  brand('alibaba', '阿里云', '', '通义'),
  brand('baidu', '百度', '', '文心'),
  brand('bytedance', '字节', '', '豆包'),
  brand('antgroup', '蚂蚁', '', '蚂蚁'),
  brand('cloudflare', 'Cloudflare', '', 'Workers'),
  brand('workersai', 'Workers AI', '', 'Workers'),
  brand('snowflake', 'Snowflake', '', 'Snowflake'),
  // 创作 / 多媒体 / 工具
  brand('stability', 'Stability', '', 'Stability'),
  brand('midjourney', 'Midjourney', '', 'Midjourney'),
  brand('dalle', 'DALL·E', 'ChatGPT'),
  brand('sora', 'Sora', 'ChatGPT'),
  brand('runway', 'Runway', '', 'Runway'),
  brand('luma', 'Luma', '', 'Luma'),
  brand('pika', 'Pika', '', 'Pika'),
  brand('kling', '可灵', '', '可灵'),
  brand('jimeng', '即梦', '', '即梦'),
  brand('suno', 'Suno', '', 'Suno'),
  brand('elevenlabs', 'ElevenLabs', '', 'ElevenLabs'),
  brand('novelai', 'NovelAI', '', 'NovelAI'),
  brand('cursor', 'Cursor', '', 'Cursor'),
  brand('copilot', 'Copilot', '', 'Copilot'),
  brand('githubcopilot', 'GitHub Copilot', '', 'Copilot'),
  brand('poe', 'Poe', '', 'Poe'),
  brand('v0', 'v0', '', 'v0'),
  brand('vercel', 'Vercel', '', 'Vercel'),
  brand('codegeex', 'CodeGeeX', '智谱'),
  brand('colab', 'Colab', '', 'Colab'),
  brand('gradio', 'Gradio', '', 'Gradio'),
  brand('langchain', 'LangChain', '', 'LangChain'),
  brand('llamaindex', 'LlamaIndex', '', 'LlamaIndex'),
  brand('jina', 'Jina', '', 'Jina'),
  brand('voyage', 'Voyage', '', 'Voyage'),
  brand('rwkv', 'RWKV', '', 'RWKV'),
  brand('palm', 'PaLM', 'Gemini'),
  brand('anthropic', 'Anthropic', 'Claude'),
]

export function brandIconFor(plat: string): string | undefined {
  return platBrands.find((b) => b.plat === plat)?.icon
}

export function brandAction(b: Pick<PlatBrand, 'plat' | 'label' | 'q'>): { plat?: string; q?: string } {
  if (b.plat) return { plat: b.plat }
  if (b.q) return { q: b.q }
  return { q: b.label }
}

export const apiToPlat: Record<string, string> = Object.fromEntries(
  Object.entries(platToApi).map(([k, v]) => [v, k]),
)

export type FeedSku = {
  models?: string[]
  id: string
  listingId?: number
  sellerId?: number
  detectionStatus?: DetectionStatus
  modelChecks?: ModelCheck[]
  /** 平台保障池分组名（route.pool） */
  poolName?: string
  /** 上游平台标识，用于调用示例 */
  apiPlatform?: string
  kind: 'channel' | 'pack'
  plat: string
  tier: string
  title: string
  desc?: string
  img: string
  mult: number
  min?: number
  face?: number
  /** 订阅周期（天）；订阅套餐展示用，后端未下发时默认 30 */
  days?: number
  /** 期内额度；未下发时回退 face */
  quota?: number
  stock?: number
  grade: string
  success: number
  ttft: number
  pool?: boolean
  badge?: string
  categories?: string[]
  inspectionMode?: string
  qualityChecks?: Record<string, InspectionCheckResult>
  lastCheckedAt?: string | null
  sellerAvgRating?: number
  sellerRatingCount?: number
}

/** 合并服务端运营配置与本地默认（服务端缺字段时保留默认） */
export function mergeDiscovery(
  base: MarketDiscoveryConfig,
  over: Record<string, unknown> | null | undefined,
): MarketDiscoveryConfig {
  const out = JSON.parse(JSON.stringify(base)) as MarketDiscoveryConfig
  if (!over || typeof over !== 'object') return out
  if (Array.isArray(over.hot_searches)) out.hot_searches = over.hot_searches as HotSearch[]
  if (over.hot_bar && typeof over.hot_bar === 'object') {
    out.hot_bar = { ...out.hot_bar, ...(over.hot_bar as object) }
  }
  if (over.feed && typeof over.feed === 'object') {
    out.feed = { ...out.feed, ...(over.feed as object) }
  }
  if (typeof over.version === 'number') out.version = over.version
  return out
}


const coverByPlat: Record<string, string> = {
  Claude: 'card1.jpg',
  ChatGPT: 'card2.jpg',
  Kimi: 'card3.jpg',
  Gemini: 'card4.jpg',
  智谱: 'card5.jpg',
  Grok: 'card6.jpg',
  MiniMax: 'card7.jpg',
  Antigravity: 'thumb7.jpg',
  保障池: 'card15.jpg',
}

export function coverFor(plat: string, id: number | string): string {
  if (coverByPlat[plat]) return coverByPlat[plat]
  const n = (typeof id === 'number' ? id : Number(String(id).replace(/\D/g, '')) || 1) % 15 || 1
  return `card${n}.jpg`
}

/** 市场分类节点 → 首页侧栏点击动作（按根类语义） */
const vendorPlatById: Record<string, string> = {
  claude: 'Claude',
  chatgpt: 'ChatGPT',
  gemini: 'Gemini',
  kimi: 'Kimi',
  zhipu: '智谱',
  minimax: 'MiniMax',
  grok: 'Grok',
  deepseek: 'DeepSeek',
  qwen: '通义千问',
  mistral: 'Mistral',
  meta_llama: 'Llama',
  cohere: 'Cohere',
  perplexity: 'Perplexity',
  amazon: 'Amazon',
  microsoft: 'Microsoft',
  nvidia: 'NVIDIA',
  baidu: '百度',
  bytedance: '字节跳动',
  tencent: '腾讯',
  ai21: 'AI21',
  inflection: 'Inflection',
}

export function categoryToAction(
  cat: { id: string; name: string },
  rootId: string,
): MarketAction {
  if (rootId === 'vendor') {
    if (cat.id === 'other_vendor') return { q: 'Antigravity' }
    return { plat: vendorPlatById[cat.id] || cat.name }
  }
  if (rootId === 'offer') {
    if (cat.id === 'channel') return { kindTab: 'channel' }
    if (cat.id === 'quota_pack') return { kindTab: 'pack' }
    if (cat.id === 'pool') return { plat: '保障池' }
    if (cat.id === 'subscription') return { tag: 'subscription' }
    if (cat.id === 'payg') return { tag: 'payg' }
  }
  return { tag: cat.id }
}

type CatNode = {
  id: string
  name: string
  sort: number
  enabled?: boolean
  icon?: string
  children?: CatNode[]
}

type NavItem = { text: string; action: MarketAction }

/** 飞层分组：把过长子类切成多栏，避免一整坨挤在一起 */
const SECTION_SPLITS: Record<string, { title: string; ids: string[] }[]> = {
  vendor: [
    { title: '国际头部', ids: ['claude', 'chatgpt', 'gemini', 'grok', 'mistral', 'cohere', 'perplexity'] },
    { title: '国内头部', ids: ['kimi', 'zhipu', 'minimax', 'deepseek', 'qwen', 'baidu', 'bytedance', 'tencent'] },
    { title: '云与开源', ids: ['meta_llama', 'amazon', 'microsoft', 'nvidia', 'ai21', 'inflection', 'other_vendor'] },
  ],
  usecase: [
    { title: '开发与对话', ids: ['uc_programming', 'uc_agents', 'uc_chat', 'uc_technology', 'uc_data'] },
    { title: '内容与增长', ids: ['uc_content', 'uc_marketing', 'uc_seo', 'uc_translation', 'uc_roleplay', 'uc_customer'] },
    { title: '专业领域', ids: ['uc_research', 'uc_documents', 'uc_math', 'uc_science', 'uc_legal', 'uc_finance', 'uc_health', 'uc_academia', 'uc_trivia'] },
  ],
  capability: [
    { title: '理解与推理', ids: ['cap_vision', 'cap_reasoning', 'cap_file', 'cap_tools', 'cap_structured', 'cap_web'] },
    { title: '生成与媒体', ids: ['cap_image_gen', 'cap_video', 'cap_audio', 'cap_realtime'] },
    { title: '检索与效率', ids: ['cap_embedding', 'cap_rerank', 'cap_caching', 'cap_fast', 'cap_batch'] },
  ],
  region: [
    { title: '中文区', ids: ['intl', 'cn'] },
    { title: '亚太', ids: ['jp', 'kr', 'sg', 'in', 'au'] },
    { title: '欧美及其他', ids: ['us', 'eu', 'uk', 'br', 'me', 'global'] },
  ],
  scene: [
    { title: '价格与品质', ids: ['low_rate', 'cheap', 'featured', 'grade_s', 'stable', 'refundable'] },
    { title: '场景与规格', ids: ['coding', 'long_ctx', 'multilingual', 'open_weight', 'enterprise', 'newbie', 'high_tpm'] },
  ],
  offer: [
    { title: '计费形态', ids: ['channel', 'quota_pack', 'pool', 'subscription', 'payg'] },
  ],
}

/** 侧栏快捷入口：每行 3 个，覆盖主入口且不过长 */
const LABEL_IDS: Record<string, string[]> = {
  vendor: ['claude', 'chatgpt', 'gemini'],
  offer: ['channel', 'quota_pack', 'pool'],
  scene: ['coding', 'featured', 'low_rate'],
  region: ['intl', 'cn', 'us'],
  usecase: ['uc_programming', 'uc_agents', 'uc_chat'],
  capability: ['cap_vision', 'cap_tools', 'cap_reasoning'],
}

const ROOT_ICON: Record<string, string> = {
  vendor: 'chat',
  offer: 'tag',
  scene: 'code',
  region: 'more',
  usecase: 'code',
  capability: 'tag',
}

function sortedEnabled(nodes: CatNode[] | undefined): CatNode[] {
  return [...(nodes ?? [])]
    .filter((c) => c.enabled !== false)
    .sort((a, b) => a.sort - b.sort || a.id.localeCompare(b.id))
}

function toItem(cat: CatNode, rootId: string): NavItem {
  return { text: cat.name, action: categoryToAction(cat, rootId) }
}

function splitSections(rootId: string, children: CatNode[]): { title: string; items: NavItem[] }[] {
  const byId = new Map(children.map((c) => [c.id, c]))
  const splits = SECTION_SPLITS[rootId]
  if (!splits?.length) {
    if (!children.length) return []
    // 未知根：按每组最多 6 个切开
    const out: { title: string; items: NavItem[] }[] = []
    for (let i = 0; i < children.length; i += 6) {
      const chunk = children.slice(i, i + 6)
      out.push({
        title: i === 0 ? '全部' : `更多 ${Math.floor(i / 6) + 1}`,
        items: chunk.map((c) => toItem(c, rootId)),
      })
    }
    return out
  }
  const used = new Set<string>()
  const sections: { title: string; items: NavItem[] }[] = []
  for (const s of splits) {
    const items: NavItem[] = []
    for (const id of s.ids) {
      const c = byId.get(id)
      if (!c) continue
      used.add(id)
      items.push(toItem(c, rootId))
    }
    if (items.length) sections.push({ title: s.title, items })
  }
  const rest = children.filter((c) => !used.has(c.id)).map((c) => toItem(c, rootId))
  if (rest.length) sections.push({ title: '其他', items: rest })
  return sections
}

/**
 * 市场分类树 → 首页侧栏导航：飞层按主题分栏铺满，侧栏每行 3 个快捷入口；子类全量收录。
 */
export function buildCategoryNavGroups(tree: CatNode[] | null | undefined): CategoryGroup[] {
  const roots = sortedEnabled(tree ?? [])
  return roots.map((root) => {
    const children = sortedEnabled(root.children)
    const sections = splitSections(root.id, children)
    const want = LABEL_IDS[root.id] ?? children.slice(0, 3).map((c) => c.id)
    const labels: NavItem[] = []
    const byId = new Map(children.map((c) => [c.id, c]))
    for (const id of want) {
      const c = byId.get(id)
      if (c) labels.push(toItem(c, root.id))
      if (labels.length >= 3) break
    }
    if (!labels.length) {
      labels.push({ text: root.name, action: categoryToAction(root, root.id) })
    }
    return {
      id: root.id,
      enabled: true,
      sort: root.sort,
      icon: root.icon || ROOT_ICON[root.id] || 'chat',
      labels,
      sections,
    }
  })
}
