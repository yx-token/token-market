import { describe, expect, it } from 'vitest'
import { assignedUpstreamCount, assignedUpstreamIDs } from '@/utils/proxyBindings'

describe('proxy bindings compatibility', () => {
  it('falls back to the first binding only when both new fields are absent', () => {
    expect(assignedUpstreamIDs({ assigned_upstream_id: 71 })).toEqual([71])
    expect(assignedUpstreamCount({ assigned_upstream_id: 71 })).toBe(1)
    expect(assignedUpstreamCount({})).toBe(0)
  })

  it.each([
    { assigned_upstream_ids: [], assigned_upstream_count: 0 },
    { assigned_upstream_ids: [] },
    { assigned_upstream_count: 0 },
  ])('uses new empty bindings instead of a stale legacy ID: %j', (bindings) => {
    const proxy = { assigned_upstream_id: 71, ...bindings }
    expect(assignedUpstreamIDs(proxy)).toEqual([])
    expect(assignedUpstreamCount(proxy)).toBe(0)
  })
})
