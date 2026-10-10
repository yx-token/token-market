/** 市场发现配置（对齐 docs/prototypes/market-config.js） */

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
          title: '供给类型',
          items: [
            { text: '按量通道', action: { kindTab: 'channel' } },
            { text: '额度包', action: { kindTab: 'pack' } },
            { text: '保障池', action: { plat: '保障池' } },
            { text: '全部供给', action: { kindTab: 'all' } },
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
            { text: '发布供给', action: { href: '/seller/publish' } },
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
  id: string
  listingId?: number
  kind: 'channel' | 'pack'
  plat: string
  tier: string
  title: string
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
  }
  return { tag: cat.id }
}
