import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { market } from '@/api'
import type { MarketItem, PoolItem, Price } from '@/api/types'

export interface BoardRow {
  platform: string
  best: number
  count: number
  successRate: number | null
  ttft: number | null
  requests: number
}

// 行情：公开的挂单、默认分组与官方价目，供顶部滚动行情条与市场页的行情板使用，每分钟刷新。
export const useMarketBoard = defineStore('marketBoard', () => {
  const listings = ref<MarketItem[]>([])
  const pools = ref<PoolItem[]>([])
  const prices = ref<Price[]>([])
  const updatedAt = ref<Date | null>(null)
  let timer: ReturnType<typeof setInterval> | null = null

  async function load() {
    const [l, p, pr] = await Promise.all([
      market.listings({ limit: 500 }).catch(() => null),
      market.pools().catch(() => null),
      market.prices().catch(() => null),
    ])
    if (l) listings.value = l
    if (p) pools.value = p
    if (pr) prices.value = pr
    updatedAt.value = new Date()
  }

  function start() {
    if (timer) return
    load()
    timer = setInterval(load, 60_000)
  }

  /** 每个平台的最低买家价、在售挂单数、加权成功率、首字延迟中位 */
  const board = computed<BoardRow[]>(() => {
    const by = new Map<string, MarketItem[]>()
    for (const it of listings.value) by.set(it.platform, [...(by.get(it.platform) ?? []), it])
    return [...by.entries()]
      .map(([platform, items]) => {
        const req = items.reduce((s, i) => s + i.requests_24h, 0)
        const weighted = items.filter((i) => i.success_rate_24h !== null)
        const sr = weighted.length ? weighted.reduce((s, i) => s + (i.success_rate_24h ?? 0) * Math.max(1, i.requests_24h), 0) / weighted.reduce((s, i) => s + Math.max(1, i.requests_24h), 0) : null
        const tt = items.map((i) => i.ttft_p50_ms).filter((v): v is number => v !== null).sort((a, b) => a - b)
        return {
          platform,
          best: Math.min(...items.map((i) => i.price)),
          count: items.length,
          successRate: sr,
          ttft: tt.length ? tt[Math.floor(tt.length / 2)] : null,
          requests: req,
        }
      })
      .sort((a, b) => b.count - a.count || a.best - b.best)
  })

  return { listings, pools, prices, updatedAt, board, load, start }
})
