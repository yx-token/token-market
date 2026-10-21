import { describe, expect, it } from 'vitest'
import { toRoute, validateChain } from '@/utils/route'

describe('validateChain', () => {
  it('accepts a seller chain ending with a same-platform or composite pool', () => {
    expect(validateChain([{ listingId: 1, platform: 'anthropic' }, { listingId: 2, platform: 'anthropic' }, { pool: 'tm-pool-claude', platform: 'anthropic' }])).toBeNull()
    expect(validateChain([{ listingId: 1, platform: 'anthropic' }, { pool: 'tm-pool-all', platform: 'composite' }])).toBeNull()
  })
  it('rejects the same rules as the backend', () => {
    expect(validateChain([])).toMatch('至少')
    expect(validateChain(Array.from({ length: 6 }, (_, i) => ({ listingId: i + 1, platform: 'anthropic' })))).toMatch('最多 5 跳')
    expect(validateChain([{ pool: 'tm-pool-claude', platform: 'anthropic' }, { listingId: 1, platform: 'anthropic' }])).toMatch('最后一跳')
    expect(validateChain([{ listingId: 1, platform: 'anthropic' }, { listingId: 1, platform: 'anthropic' }])).toMatch('重复')
    expect(validateChain([{ listingId: 1, platform: 'anthropic' }, { listingId: 2, platform: 'openai' }])).toMatch('同一平台')
    expect(validateChain([{ listingId: 1, platform: 'anthropic' }, { pool: 'tm-pool-gpt', platform: 'openai' }])).toMatch('全模型')
    expect(validateChain([{ platform: '' }])).toMatch('没有选择')
  })
  it('builds the request body', () => {
    expect(toRoute('default', 'tm-pool-claude', [])).toEqual({ type: 'default', pool: 'tm-pool-claude' })
    expect(toRoute('chain', '', [{ listingId: 3, platform: 'anthropic' }, { pool: 'tm-pool-all', platform: 'composite' }])).toEqual({
      type: 'chain',
      hops: [{ listing_id: 3 }, { pool: 'tm-pool-all' }],
    })
  })
})
