// 买家 Key 路由的前端校验，规则与后端 internal/buyer/keys.go validateRoute 一致。
import type { HopInput, RouteInput } from '@/api/types'

export interface HopChoice {
  listingId?: number
  pool?: string
  platform: string
}

export function validateChain(hops: HopChoice[], maxHops = 5): string | null {
  if (hops.length === 0) return '至少需要一跳'
  if (hops.length > maxHops) return `最多 ${maxHops} 跳`
  const seen = new Set<number>()
  let platform = ''
  for (let i = 0; i < hops.length; i++) {
    const h = hops[i]
    if (h.pool) {
      if (i !== hops.length - 1) return '默认分组只能放在最后一跳'
      if (platform && h.platform !== platform && h.platform !== 'composite') return '默认分组必须与链内平台一致，或选择全模型分组'
      continue
    }
    if (!h.listingId) return `第 ${i + 1} 跳没有选择挂单`
    if (seen.has(h.listingId)) return '同一挂单不能重复出现'
    seen.add(h.listingId)
    if (platform && h.platform !== platform) return '链内各跳必须是同一平台'
    platform = h.platform
  }
  return null
}

export function toRoute(type: 'default' | 'chain', pool: string, hops: HopChoice[]): RouteInput {
  if (type === 'default') return { type, pool }
  return {
    type,
    hops: hops.map((h): HopInput => (h.pool ? { pool: h.pool } : { listing_id: h.listingId })),
  }
}
