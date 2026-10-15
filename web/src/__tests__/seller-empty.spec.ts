import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { seller } from '@/api'
import ListingsView from '@/views/seller/ListingsView.vue'

vi.mock('@/api', () => ({
  market: { config: vi.fn().mockResolvedValue({ commission_rate: 0.2, freeze_hours: 24, freeze_days: 1 }) },
  seller: {
    listings: vi.fn(),
    listingCategories: vi.fn().mockResolvedValue(null),
    upstreams: vi.fn().mockResolvedValue(null),
  },
}))

async function openListings() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
  await router.push('/seller/listings')
  const wrapper = mount(ListingsView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()
  return wrapper
}

describe('卖家空列表不是载入失败', () => {
  beforeEach(() => vi.clearAllMocks())

  it('listings 返回 200 null 时显示还没有商品', async () => {
    vi.mocked(seller.listings).mockResolvedValue(null)
    const wrapper = await openListings()
    expect(wrapper.text()).not.toContain('挂单没有载入')
    expect(wrapper.text()).toContain('还没有商品')
    wrapper.unmount()
  })

  it('listings 请求失败才显示载入失败', async () => {
    vi.mocked(seller.listings).mockRejectedValue(new Error('network'))
    const wrapper = await openListings()
    expect(wrapper.text()).toContain('挂单没有载入')
    wrapper.unmount()
  })
})
