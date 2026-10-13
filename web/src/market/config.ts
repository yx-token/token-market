/** 市场发现配置（对齐 docs/prototypes/market-config.js） */
import type { InspectionCheckResult } from '@/api/types'

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

export type Slot = {
  id: string
  name: string
  enabled?: boolean
  title: string
  desc?: string
  action: MarketAction
  eng?: string
  theme?: string
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
  category_groups: CategoryGroup[]
  slots: Slot[]
  hot_searches: HotSearch[]
  hot_bar: { enabled: boolean }
  feed: { pin_pool_first: boolean }
}

export const marketConfigDefaults: MarketDiscoveryConfig = {
  version: 2,
  category_groups: [
    {
      id: 'grp_intl',
      enabled: true,
      sort: 10,
      icon: 'chat',
      labels: [
        { text: 'Claude', action: { plat: 'Claude' } },
        { text: 'ChatGPT', action: { plat: 'ChatGPT' } },
        { text: 'Gemini', action: { plat: 'Gemini' } },
      ],
      sections: [
        {
          title: 'Claude',
          items: [
            { text: '全部 Claude', action: { plat: 'Claude' } },
            { text: 'Max 5x', action: { q: 'Max 5x' } },
            { text: 'Max 20x', action: { q: 'Max 20x' } },
            { text: 'Pro', action: { q: 'Claude Pro' } },
            { text: '低倍率', action: { plat: 'Claude', tag: 'low_rate' } },
            { text: '额度包', action: { plat: 'Claude', kindTab: 'pack' } },
            { text: '保障池', action: { plat: '保障池', q: 'Claude' } },
          ],
        },
        {
          title: 'ChatGPT',
          items: [
            { text: '全部 ChatGPT', action: { plat: 'ChatGPT' } },
            { text: 'Plus', action: { q: 'Plus' } },
            { text: 'Team', action: { q: 'Team' } },
            { text: 'Codex', action: { q: 'Codex' } },
            { text: '额度包', action: { plat: 'ChatGPT', kindTab: 'pack' } },
            { text: '保障池', action: { plat: '保障池', q: 'ChatGPT' } },
          ],
        },
        {
          title: 'Gemini',
          items: [
            { text: '全部 Gemini', action: { plat: 'Gemini' } },
            { text: 'Ultra', action: { q: 'Ultra' } },
            { text: 'Pro', action: { q: 'Gemini Pro' } },
            { text: '额度包', action: { plat: 'Gemini', kindTab: 'pack' } },
          ],
        },
      ],
    },
    {
      id: 'grp_coding',
      enabled: true,
      sort: 20,
      icon: 'code',
      labels: [
        { text: 'Kimi', action: { plat: 'Kimi' } },
        { text: '智谱', action: { plat: '智谱' } },
        { text: 'MiniMax', action: { plat: 'MiniMax' } },
      ],
      sections: [
        {
          title: '写代码专场',
          items: [
            { text: '全部写代码', action: { tag: 'coding' } },
            { text: 'Kimi Coding', action: { plat: 'Kimi' } },
            { text: '智谱 GLM', action: { plat: '智谱' } },
            { text: 'MiniMax', action: { plat: 'MiniMax' } },
            { text: '低倍率', action: { tag: 'low_rate' } },
          ],
        },
        {
          title: '额度包',
          items: [
            { text: 'Kimi 额度包', action: { plat: 'Kimi', kindTab: 'pack' } },
            { text: '智谱额度包', action: { plat: '智谱', kindTab: 'pack' } },
            { text: 'MiniMax 额度包', action: { plat: 'MiniMax', kindTab: 'pack' } },
          ],
        },
      ],
    },
    {
      id: 'grp_deal',
      enabled: true,
      sort: 30,
      icon: 'tag',
      labels: [
        { text: '按量', action: { kindTab: 'channel' } },
        { text: '额度包', action: { kindTab: 'pack' } },
        { text: '保障池', action: { plat: '保障池' } },
      ],
      sections: [
        {
          title: '闲置TOKEN类型',
          items: [
            { text: '按量通道', action: { kindTab: 'channel' } },
            { text: '额度包', action: { kindTab: 'pack' } },
            { text: '保障池', action: { plat: '保障池' } },
            { text: '全部闲置TOKEN', action: { kindTab: 'all' } },
          ],
        },
        {
          title: '精选标签',
          items: [
            { text: '低倍率', action: { tag: 'low_rate' } },
            { text: 'S 级', action: { tag: 'grade_s' } },
            { text: '写代码', action: { tag: 'coding' } },
            { text: '可退货', action: { tag: 'refundable' } },
            { text: '寄售', action: { tag: 'consignment' } },
          ],
        },
      ],
    },
    {
      id: 'grp_more',
      enabled: true,
      sort: 40,
      icon: 'more',
      labels: [
        { text: 'Grok', action: { plat: 'Grok' } },
        { text: '其他', action: { q: 'Antigravity' } },
        { text: '全部', action: { plat: '' } },
      ],
      sections: [
        {
          title: '其他厂商',
          items: [
            { text: 'Grok', action: { plat: 'Grok' } },
            { text: 'Antigravity', action: { q: 'Antigravity' } },
            { text: '全部厂商', action: { plat: '' } },
          ],
        },
        {
          title: '快捷入口',
          items: [
            { text: '发布闲置TOKEN', action: { href: '/seller/publish' } },
            { text: '我的 Key', action: { href: '/buyer/keys' } },
          ],
        },
      ],
    },
  ],
  slots: [
    {
      id: 'banner',
      name: '主横幅',
      enabled: true,
      title: '按量通道\n选用即开通',
      desc: '起步金可 0 · 用多少扣多少',
      action: { kindTab: 'channel' },
    },
    {
      id: 'tile_deal',
      name: '四宫格·低倍率',
      enabled: true,
      title: '低倍率通道',
      desc: '按量 · 官方价打折',
      action: { tag: 'low_rate' },
      eng: 'DEAL',
      theme: 'deal',
    },
    {
      id: 'tile_code',
      name: '四宫格·写代码',
      enabled: true,
      title: '写代码专场',
      desc: 'Coding Plan 热挂',
      action: { tag: 'coding' },
      eng: 'CODE',
      theme: 'code',
    },
    {
      id: 'tile_pool',
      name: '四宫格·保障池',
      enabled: true,
      title: '保障池',
      desc: '不挑卖家 · 自动派',
      action: { plat: '保障池' },
      eng: 'POOL',
      theme: 'pool',
    },
    {
      id: 'tile_pack',
      name: '四宫格·额度包',
      enabled: true,
      title: '额度包',
      desc: '先买额度 · 可退货',
      action: { kindTab: 'pack' },
      eng: 'PACK',
      theme: 'buyer',
    },
  ],
  hot_searches: [
    { id: 'hs_claude', label: 'Claude Max', enabled: true, sort: 10, action: { q: 'Claude' } },
    { id: 'hs_gpt', label: 'ChatGPT Plus', enabled: true, sort: 20, action: { q: 'ChatGPT' } },
    { id: 'hs_kimi', label: 'Kimi Coding', enabled: true, sort: 30, action: { q: 'Kimi' } },
    { id: 'hs_low', label: '低倍率', enabled: true, sort: 40, action: { tag: 'low_rate' } },
    { id: 'hs_pool', label: '保障池', enabled: true, sort: 50, action: { plat: '保障池' } },
    { id: 'hs_pack', label: '额度包', enabled: true, sort: 60, action: { kindTab: 'pack' } },
    { id: 'hs_code', label: '写代码', enabled: true, sort: 70, action: { tag: 'coding' } },
  ],
  hot_bar: { enabled: true },
  feed: { pin_pool_first: true },
}

export function enabledSorted<T extends { enabled?: boolean; sort?: number }>(list: T[]): T[] {
  return (list || []).filter((x) => x.enabled !== false).sort((a, b) => (a.sort || 0) - (b.sort || 0))
}

export const catIconSvg: Record<string, string> = {
  chat: '<svg viewBox="0 0 24 24" fill="none"><rect x="3.5" y="5" width="17" height="12" rx="2.5" stroke="#1f1f1f" stroke-width="1.5"/><path d="M8 17.5L6 20.5V17.5" stroke="#1f1f1f" stroke-width="1.5" stroke-linejoin="round"/><circle cx="9" cy="11" r="1" fill="#FFE60F"/><circle cx="12" cy="11" r="1" fill="#1f1f1f"/><circle cx="15" cy="11" r="1" fill="#FF6A3D"/></svg>',
  code: '<svg viewBox="0 0 24 24" fill="none"><path d="M9 8.5L5.5 12 9 15.5M15 8.5L18.5 12 15 15.5" stroke="#1f1f1f" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/><path d="M13 7l-2 10" stroke="#3D8BFF" stroke-width="1.5" stroke-linecap="round"/></svg>',
  tag: '<svg viewBox="0 0 24 24" fill="none"><path d="M4 12.5V5.8A1.8 1.8 0 0 1 5.8 4H12l8 8-7.2 7.2a1.8 1.8 0 0 1-2.5 0L4 12.5z" stroke="#1f1f1f" stroke-width="1.5" stroke-linejoin="round"/><circle cx="9" cy="9" r="1.2" fill="#FFE60F" stroke="#1f1f1f" stroke-width="1"/></svg>',
  more: '<svg viewBox="0 0 24 24" fill="none"><circle cx="12" cy="12" r="8" stroke="#1f1f1f" stroke-width="1.5"/><circle cx="8.5" cy="12" r="1.1" fill="#1f1f1f"/><circle cx="12" cy="12" r="1.1" fill="#FFE60F"/><circle cx="15.5" cy="12" r="1.1" fill="#1f1f1f"/></svg>',
}

export const tileThemeIcon: Record<string, string> = {
  deal: '/assets/tile-icon-deal.png',
  code: '/assets/tile-icon-code.png',
  pool: '/assets/tile-icon-pool.png',
  buyer: '/assets/tile-icon-buyer.png',
}

/** 四宫格无真实挂单时的分主题占位图（避免四格同一套 thumb） */
export const tileFallbackThumbs: Record<string, { img: string; price: string }[]> = {
  deal: [
    { img: '/assets/card1.jpg', price: '0.55×' },
    { img: '/assets/card2.jpg', price: '0.48×' },
    { img: '/assets/card8.jpg', price: '0.52×' },
  ],
  code: [
    { img: '/assets/card3.jpg', price: 'Kimi' },
    { img: '/assets/card5.jpg', price: '智谱' },
    { img: '/assets/card7.jpg', price: 'MiniMax' },
  ],
  pool: [
    { img: '/assets/card15.jpg', price: 'Claude' },
    { img: '/assets/hero2.jpg', price: 'GPT' },
    { img: '/assets/card4.jpg', price: 'Gemini' },
  ],
  buyer: [
    { img: '/assets/card14.jpg', price: '¥100' },
    { img: '/assets/thumb5.jpg', price: '¥50' },
    { img: '/assets/thumb11.jpg', price: '¥200' },
  ],
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

export const apiToPlat: Record<string, string> = Object.fromEntries(
  Object.entries(platToApi).map(([k, v]) => [v, k]),
)

export type FeedSku = {
  models?: string[]
  id: string
  listingId?: number
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
}

/** 合并服务端运营配置与本地默认（服务端缺字段时保留默认） */
export function mergeDiscovery(
  base: MarketDiscoveryConfig,
  over: Record<string, unknown> | null | undefined,
): MarketDiscoveryConfig {
  const out = JSON.parse(JSON.stringify(base)) as MarketDiscoveryConfig
  if (!over || typeof over !== 'object') return out
  if (Array.isArray(over.category_groups)) out.category_groups = over.category_groups as CategoryGroup[]
  if (Array.isArray(over.slots)) out.slots = over.slots as Slot[]
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

/** 原型演示货架：API 无数据时本地预览用，结构与 market-home.html 一致 */
export const demoSkus: FeedSku[] = [
  { id: 'c-1001', kind: 'channel', plat: 'Claude', tier: 'Max 5x', title: 'Claude · Max 5x · 按量通道', img: 'card1.jpg', mult: 0.55, min: 0, grade: 'S', success: 0.98, ttft: 420, badge: '低倍率' },
  { id: 'c-1002', kind: 'channel', plat: 'ChatGPT', tier: 'Plus', title: 'ChatGPT · Plus · Codex 可用', img: 'card2.jpg', mult: 0.62, min: 10, grade: 'A', success: 0.96, ttft: 380 },
  { id: 'c-1003', kind: 'channel', plat: 'Kimi', tier: 'Coding', title: 'Kimi · Coding Plan · 按量', img: 'card3.jpg', mult: 0.48, min: 0, grade: 'A', success: 0.97, ttft: 510, badge: '写代码' },
  { id: 'c-1004', kind: 'channel', plat: 'Gemini', tier: 'Ultra', title: 'Gemini · Google One Ultra', img: 'card4.jpg', mult: 0.7, min: 20, grade: 'B', success: 0.94, ttft: 450 },
  { id: 'c-1005', kind: 'channel', plat: '智谱', tier: 'Coding', title: '智谱 · GLM Coding · 国内站', img: 'card5.jpg', mult: 0.5, min: 0, grade: 'A', success: 0.95, ttft: 480 },
  { id: 'c-1006', kind: 'channel', plat: 'Grok', tier: 'Super', title: 'Grok · SuperGrok · 刚过检', img: 'card6.jpg', mult: 0.68, min: 5, grade: 'A', success: 0.93, ttft: 390 },
  { id: 'c-1011', kind: 'channel', plat: '保障池', tier: 'Claude', title: 'Claude 保障池 · 自动派号', img: 'card15.jpg', mult: 0.8, min: 0, grade: 'A', success: 0.99, ttft: 400, badge: '保障', pool: true },
  { id: 'c-1012', kind: 'channel', plat: '保障池', tier: 'ChatGPT', title: 'ChatGPT 保障池 · 自动派', img: 'hero2.jpg', mult: 0.7, min: 0, grade: 'A', success: 0.98, ttft: 390, badge: '保障', pool: true },
  { id: 'p-2001', kind: 'pack', plat: 'Claude', tier: 'Max', title: 'Claude · Max · 100 元额度包', img: 'card14.jpg', mult: 0.55, face: 100, stock: 3, grade: 'S', success: 0.98, ttft: 420, badge: '可退货' },
  { id: 'p-2002', kind: 'pack', plat: 'Kimi', tier: 'Coding', title: 'Kimi Coding · 50 元额度包', img: 'thumb5.jpg', mult: 0.48, face: 50, stock: 8, grade: 'A', success: 0.97, ttft: 510, badge: '写代码' },
  { id: 'p-2003', kind: 'pack', plat: 'ChatGPT', tier: 'Plus', title: 'ChatGPT · 200 元额度包', img: 'thumb11.jpg', mult: 0.62, face: 200, stock: 2, grade: 'A', success: 0.96, ttft: 380, badge: '可退货' },
  { id: 'p-2004', kind: 'pack', plat: '智谱', tier: 'Coding', title: '智谱 · 80 元额度包', img: 'thumb10.jpg', mult: 0.52, face: 80, stock: 5, grade: 'B', success: 0.93, ttft: 530 },
]

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
