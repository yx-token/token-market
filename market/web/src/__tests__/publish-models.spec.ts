import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { market, seller } from '@/api'
import type { MarketConfig, Proxy, Upstream, UpstreamDetail } from '@/api/types'
import PublishView from '@/views/seller/PublishView.vue'

vi.mock('@/api', () => ({
  market: { config: vi.fn().mockResolvedValue({ require_seller_proxy: false, commission_rate: 0.2 }) },
  seller: {
    vendors: vi.fn().mockResolvedValue([{ platform: 'zhipu', auth_types: ['coding'], consignment_min: 0.1, consignment_max: 2 }]),
    proxies: vi.fn().mockResolvedValue([]), upstreams: vi.fn().mockResolvedValue([]), listings: vi.fn().mockResolvedValue([]), listingCategories: vi.fn().mockResolvedValue([]),
    createUpstream: vi.fn(), submitCodingKey: vi.fn(), setSelfReserve: vi.fn().mockResolvedValue({}), upstream: vi.fn(), retryVerification: vi.fn(),
    upstreamModels: vi.fn(), refreshUpstreamModels: vi.fn(), setUpstreamModels: vi.fn(),
  },
}))
const base = { id: 5, platform: 'zhipu', auth_type: 'coding', status: 'selecting_models' } as Upstream

async function openPage(path = '/seller/publish') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
  await router.push(path)
  const wrapper = mount(PublishView, { global: { plugins: [createPinia(), router], stubs: { InspectionAppraisal: true } } })
  await flushPromises()
  return wrapper
}

describe('发布向导的模型选择步骤', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(market.config).mockResolvedValue({ require_seller_proxy: false, commission_rate: 0.2 } as MarketConfig)
    vi.mocked(seller.proxies).mockResolvedValue([])
    vi.mocked(seller.createUpstream).mockResolvedValue({ ...base, status: 'draft' })
    vi.mocked(seller.submitCodingKey).mockResolvedValue(base)
    vi.mocked(seller.upstream).mockResolvedValue({ ...base, status: 'verifying' } as UpstreamDetail)
    vi.mocked(seller.upstreamModels).mockResolvedValue({ catalog: ['glm-5', 'glm-4.7'], whitelist: null, fetched_at: null })
    vi.mocked(seller.setUpstreamModels).mockResolvedValue({ ...base, status: 'verifying' })
  })

  it('only offers synchronized connected seller proxies, including already assigned ones', async () => {
    const proxy: Proxy = { id: 1, seller_id: 1, name: 'shared', protocol: 'http', host: 'proxy.test', port: 8080, status: 'assigned', sub2api_proxy_id: 1, created_at: '', assigned_upstream_ids: [71, 72], assigned_upstream_count: 2, connection_report: { success: true, message: 'ok', latency_ms: 1 } }
    vi.mocked(market.config).mockResolvedValue({ require_seller_proxy: true, commission_rate: 0.2 } as MarketConfig)
    vi.mocked(seller.proxies).mockResolvedValue([
      proxy,
      { ...proxy, id: 2, status: 'available', assigned_upstream_ids: [], assigned_upstream_count: 0 },
      { ...proxy, id: 3, seller_id: null },
      { ...proxy, id: 4, seller_id: undefined },
      { ...proxy, id: 5, status: 'disabled' },
      { ...proxy, id: 6, sub2api_proxy_id: undefined },
      { ...proxy, id: 7, connection_report: undefined },
      { ...proxy, id: 8, connection_report: { success: false, message: 'failed', latency_ms: 0 } },
      { ...proxy, id: 9, status: 'pending' },
    ])
    const wrapper = await openPage()
    const select = wrapper.get('select.xm-input')
    expect(select.findAll('option').map((option) => option.attributes('value'))).toEqual(['', '1', '2'])
    expect(select.get('option[value="1"]').text()).toContain('已绑定 2 个账号')
    await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('配置出口代理')
    await select.setValue('1')
    await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
    await wrapper.get('input[type="password"]').setValue('订阅密钥')
    await wrapper.findAll('button').find((button) => button.text() === '完成接入，选择模型')!.trigger('click')
    await flushPromises()
    expect(seller.createUpstream).toHaveBeenCalledWith('zhipu', 'coding', 1)
    wrapper.unmount()
  })

  it('接入完成后选择模型，确认后启动检测轮询', async () => {
    const wrapper = await openPage()
    await wrapper.findAll('button').find((b) => b.text() === '下一步')!.trigger('click')
    await wrapper.get('input[type="password"]').setValue('订阅密钥')
    await wrapper.findAll('button').find((b) => b.text() === '完成接入，选择模型')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('选择挂售模型')
    expect(seller.upstream).not.toHaveBeenCalled()
    await wrapper.findAll('input[type="checkbox"]')[1]!.setValue(false)
    await wrapper.findAll('button').find((b) => b.text() === '确认模型并开始检测')!.trigger('click')
    await flushPromises()
    expect(seller.setUpstreamModels).toHaveBeenCalledWith(5, ['glm-5'])
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('上架检测')
    expect(seller.upstream).toHaveBeenCalledWith(5)
    wrapper.unmount()
  })

  it('通过带上游编号的链接恢复待选择模型状态', async () => {
    vi.mocked(seller.upstream).mockResolvedValue(base as UpstreamDetail)
    const wrapper = await openPage('/seller/publish?upstream=5')
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('选择挂售模型')
    expect(seller.upstreamModels).toHaveBeenCalledWith(5)
    expect(seller.createUpstream).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('重试检测返回待选择状态时进入模型选择，避免一直显示检测中', async () => {
    vi.mocked(seller.upstream).mockResolvedValue({ ...base, status: 'verify_failed' } as UpstreamDetail)
    vi.mocked(seller.retryVerification).mockResolvedValue(base)
    const wrapper = await openPage('/seller/publish?upstream=5')
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('上架检测')
    await wrapper.findAll('button').find((b) => b.text() === '重试检测')!.trigger('click')
    await flushPromises()
    expect(seller.retryVerification).toHaveBeenCalledWith(5)
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('选择挂售模型')
    expect(seller.upstreamModels).toHaveBeenCalledWith(5)
    wrapper.unmount()
  })
})
