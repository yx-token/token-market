import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { market } from '@/api'
import type { MarketItem } from '@/api/types'
import MarketView from '@/views/market/MarketView.vue'
import ListingView from '@/views/market/ListingView.vue'

vi.mock('@/api', () => ({
  auth: {}, buyer: {},
  market: {
    listings: vi.fn(), listing: vi.fn(), pools: vi.fn().mockResolvedValue([]),
    categories: vi.fn().mockResolvedValue([]), discovery: vi.fn().mockResolvedValue({}),
  },
}))
const base = { listing_id: 1, platform: 'openai', auth_type: 'oauth', price: 0.6, health: 'healthy', title: '模型通道', models: ['gpt-6-astra', 'gpt-6.1-sol'] } as MarketItem

describe('市场挂售模型展示与筛选', () => {
  beforeEach(() => vi.clearAllMocks())

  it('筛选参数写入 URL 和列表请求，保留存量未声明商品', async () => {
    const legacy = { ...base, listing_id: 2, title: '存量通道', models: [] }
    vi.mocked(market.listings).mockImplementation(async (q) => q?.model ? [legacy] : [base, legacy])
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
    await router.push('/')
    const wrapper = mount(MarketView, { global: { plugins: [createPinia(), router], stubs: { ModalDialog: true } } })
    await flushPromises()
    expect(wrapper.findAll('.sku-card')).toHaveLength(2)
    expect(wrapper.text()).toContain('模型通道')
    await wrapper.get('#f-model').setValue('gpt-custom')
    await flushPromises()
    expect(router.currentRoute.value.query.model).toBe('gpt-custom')
    expect(market.listings).toHaveBeenLastCalledWith({ sort: 'price', limit: '200', model: 'gpt-custom' })
    expect(wrapper.findAll('.sku-card')).toHaveLength(1)
    expect(wrapper.find('.sku-card').text()).toContain('存量通道')
    expect(wrapper.findAll('#market-models option')).toHaveLength(2)
    wrapper.unmount()
  })

  it('挂单详情列出白名单', async () => {
    vi.mocked(market.listing).mockResolvedValue(base)
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
    await router.push('/market/listings/1')
    const wrapper = mount(ListingView, { props: { id: '1' }, global: { plugins: [createPinia(), router], stubs: { ModalDialog: true, QrCode: true } } })
    await flushPromises()
    expect(wrapper.get('.specs').text()).toContain('挂售模型gpt-6-astra、gpt-6.1-sol')
    wrapper.unmount()
  })

  it('快速切换模型时旧请求返回不会覆盖当前筛选结果', async () => {
    let resolveFirst!: (items: MarketItem[]) => void
    let resolveSecond!: (items: MarketItem[]) => void
    const first = new Promise<MarketItem[]>((resolve) => { resolveFirst = resolve })
    const second = new Promise<MarketItem[]>((resolve) => { resolveSecond = resolve })
    vi.mocked(market.listings).mockImplementation((q) => q?.model === 'first' ? first : q?.model === 'second' ? second : Promise.resolve([base]))
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
    await router.push('/')
    const wrapper = mount(MarketView, { global: { plugins: [createPinia(), router], stubs: { ModalDialog: true } } })
    await flushPromises()
    await wrapper.get('#f-model').setValue('first')
    await flushPromises()
    await wrapper.get('#f-model').setValue('second')
    await flushPromises()
    resolveSecond([{ ...base, title: '第二个模型通道', models: ['second'] }])
    await flushPromises()
    resolveFirst([{ ...base, title: '第一个模型通道', models: ['first'] }])
    await flushPromises()
    expect(wrapper.findAll('.sku-card')).toHaveLength(1)
    expect(wrapper.get('.sku-card').text()).toContain('第二个模型通道')
    wrapper.unmount()
  })
})
