import type { Proxy } from '@/api/types'

type Bindings = Partial<Pick<Proxy, 'assigned_upstream_id' | 'assigned_upstream_ids' | 'assigned_upstream_count'>>

export function assignedUpstreamIDs(proxy: Bindings): number[] {
  if (proxy.assigned_upstream_ids != null) return proxy.assigned_upstream_ids
  if (proxy.assigned_upstream_count != null) return []
  return proxy.assigned_upstream_id ? [proxy.assigned_upstream_id] : []
}

export function assignedUpstreamCount(proxy: Bindings): number {
  return Math.max(proxy.assigned_upstream_count ?? 0, assignedUpstreamIDs(proxy).length)
}

export function canSelectSellerProxy(proxy: Proxy): boolean {
  return proxy.seller_id != null && ['available', 'assigned'].includes(proxy.status) && !!proxy.sub2api_proxy_id
}
