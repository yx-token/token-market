import { coverFor, platBrands, type FeedSku } from '@/market/config'

const DEMO_PLATS = [
  'ChatGPT',
  'Claude',
  'Gemini',
  'DeepSeek',
  '智谱',
  'Kimi',
  'MiniMax',
  'Grok',
  'Mistral',
  'Llama',
] as const

const TIERS = ['按量通道', 'Pro 通道', '高速通道', '经济通道', '企业通道'] as const
const GRADES = ['S', 'A', 'B', 'A', 'S'] as const
const MODELS = [
  'gpt-4o',
  'gpt-4.1',
  'claude-sonnet-4-5',
  'claude-opus-4',
  'gemini-2.5-pro',
  'deepseek-v3',
  'glm-4.5',
  'kimi-k2',
  'grok-3',
  'mistral-large',
] as const

/** 首页栅格演示用假数据（不可下单） */
export function buildDemoFeedSkus(count = 40): FeedSku[] {
  const out: FeedSku[] = []
  for (let i = 1; i <= count; i++) {
    const plat = DEMO_PLATS[(i - 1) % DEMO_PLATS.length]
    const isPack = i % 7 === 0
    const mult = Number((0.42 + ((i * 17) % 55) / 100).toFixed(2))
    const face = 50 + (i % 8) * 50
    const grade = GRADES[(i - 1) % GRADES.length]
    const success = Number((0.9 + ((i * 3) % 10) / 100).toFixed(2))
    const ttft = 180 + (i * 37) % 420
    const model = MODELS[(i - 1) % MODELS.length]
    const seller = 10 + (i % 12)
    out.push({
      id: `demo-${i}`,
      listingId: undefined,
      sellerId: seller,
      kind: isPack ? 'pack' : 'channel',
      plat,
      tier: TIERS[(i - 1) % TIERS.length],
      title: isPack
        ? `${plat} · 演示套餐 ¥${face}`
        : `${plat} · ${TIERS[(i - 1) % TIERS.length]} #${i}`,
      desc: `${model} · 演示数据`,
      img: coverFor(plat, i),
      mult,
      min: isPack ? undefined : i % 5 === 0 ? 0 : 10,
      face: isPack ? face : undefined,
      days: isPack ? 30 : undefined,
      stock: isPack ? 3 + (i % 9) : undefined,
      grade,
      success,
      ttft,
      pool: false,
      badge: i % 11 === 0 ? '热销' : i % 13 === 0 ? '性价比' : undefined,
      categories: i % 4 === 0 ? ['low_rate'] : undefined,
      models: [model],
      sellerAvgRating: Number((4.2 + (i % 8) / 10).toFixed(1)),
      sellerRatingCount: 1 + (i % 40),
      apiPlatform: platBrands.find((b) => b.plat === plat)?.plat || plat,
    })
  }
  return out
}

export function isDemoFeedId(id: string) {
  return id.startsWith('demo-')
}
