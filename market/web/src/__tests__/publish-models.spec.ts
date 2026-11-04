import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { seller } from '@/api'
import type { Proxy, Upstream, UpstreamDetail } from '@/api/types'
import PublishView from '@/views/seller/PublishView.vue'

vi.mock('@/api', () => ({
  market: { config: vi.fn().mockResolvedValue({ commission_rate: 0.2 }) },
  seller: {
    vendors: vi.fn().mockResolvedValue([{ platform: 'zhipu', auth_types: ['coding'], consignment_min: 0.1, consignment_max: 2, self_reserve_supported: true, require_seller_proxy: true }]),
    proxies: vi.fn().mockResolvedValue([]), sharedProxy: vi.fn().mockResolvedValue({ available: false, count: 0 }), upstreams: vi.fn().mockResolvedValue([]), listings: vi.fn().mockResolvedValue([]), listingCategories: vi.fn().mockResolvedValue([]),
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

async function advanceToProxy(wrapper: Awaited<ReturnType<typeof openPage>>) {
  await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
  await flushPromises()
}

async function advanceToAuthorize(wrapper: Awaited<ReturnType<typeof openPage>>) {
  await advanceToProxy(wrapper)
  await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
  await flushPromises()
}

describe('发布向导的模型选择步骤', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(seller.proxies).mockResolvedValue([])
    vi.mocked(seller.sharedProxy).mockResolvedValue({ available: false, count: 0 })
    vi.mocked(seller.vendors).mockResolvedValue([{ platform: 'zhipu', auth_types: ['coding'], consignment_min: 0.1, consignment_max: 2, self_reserve_supported: true, require_seller_proxy: false }])
    vi.mocked(seller.createUpstream).mockResolvedValue({ ...base, status: 'draft' })
    vi.mocked(seller.submitCodingKey).mockResolvedValue(base)
    vi.mocked(seller.upstream).mockResolvedValue({ ...base, status: 'verifying' } as UpstreamDetail)
    vi.mocked(seller.upstreamModels).mockResolvedValue({ catalog: ['glm-5', 'glm-4.7'], whitelist: null, fetched_at: null })
    vi.mocked(seller.setUpstreamModels).mockResolvedValue({ ...base, status: 'verifying' })
  })

  it('only offers synchronized connected seller proxies, including already assigned ones', async () => {
    const proxy: Proxy = { id: 1, seller_id: 1, name: 'shared', protocol: 'http', host: 'proxy.test', port: 8080, status: 'assigned', sub2api_proxy_id: 1, created_at: '', assigned_upstream_ids: [71, 72], assigned_upstream_count: 2, connection_report: { success: true, message: 'ok', latency_ms: 1 } }
    vi.mocked(seller.vendors).mockResolvedValue([{ platform: 'zhipu', auth_types: ['coding'], consignment_min: 0.1, consignment_max: 2, self_reserve_supported: true, require_seller_proxy: true }])
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
    await advanceToProxy(wrapper)
    const select = wrapper.get('select.xm-input')
    expect(select.findAll('option').map((option) => option.attributes('value'))).toEqual(['', '1', '2'])
    expect(select.get('option[value="1"]').text()).toContain('已绑定 2 个账号')
    await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
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
    await advanceToAuthorize(wrapper)
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

  it('先选择厂商再选择代理，非必须厂商默认分配共享代理且不显示平台地址', async () => {
    vi.mocked(seller.sharedProxy).mockResolvedValue({ available: true, count: 2 })
    vi.mocked(seller.proxies).mockResolvedValue([{ id: 88, seller_id: null, is_shared: true, name: 'private-platform-address', host: 'secret-proxy.internal', port: 9080, protocol: 'http', status: 'available', sub2api_proxy_id: 88, assigned_upstream_ids: [], assigned_upstream_count: 0, created_at: '', connection_report: { success: true, message: 'ok', latency_ms: 1 } }])
    const wrapper = await openPage()
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('选择模型厂商')
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.findAll('.xm-stepper__label').map((item) => item.text())).toEqual(['厂商', '代理', '接入', '模型', '检测', '商品', '定价', '完成'])
    await advanceToProxy(wrapper)
    expect(wrapper.get('select').element.value).toBe('')
    expect(wrapper.get('select').text()).toContain('平台共享代理（自动分配）')
    expect(wrapper.text()).not.toContain('secret-proxy.internal')
    expect(wrapper.text()).not.toContain('private-platform-address')
    await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
    await wrapper.get('input[type="password"]').setValue('订阅密钥')
    await wrapper.findAll('button').find((button) => button.text() === '完成接入，选择模型')!.trigger('click')
    await flushPromises()
    expect(seller.createUpstream).toHaveBeenCalledWith('zhipu', 'coding', undefined)
    wrapper.unmount()
  })

  it('没有共享代理时显示直连并允许继续授权', async () => {
    const wrapper = await openPage()
    await advanceToProxy(wrapper)
    expect(wrapper.get('select').text()).toContain('不使用代理（服务器直连）')
    expect(wrapper.text()).toContain('将使用服务器出口直连')
    await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('接入上游账号')
    wrapper.unmount()
  })

  it('OAuth 草稿创建后返回不能切换厂商或授权方式', async () => {
    vi.mocked(seller.vendors).mockResolvedValue([
      { platform: 'openai', auth_types: ['oauth'], consignment_min: 0.1, consignment_max: 2, self_reserve_supported: true, require_seller_proxy: false },
      { platform: 'zhipu', auth_types: ['coding'], consignment_min: 0.1, consignment_max: 2, self_reserve_supported: true, require_seller_proxy: true },
    ])
    vi.mocked(seller.createUpstream).mockResolvedValue({ id: 55, platform: 'openai', auth_type: 'oauth', status: 'draft' } as Upstream)
    const wrapper = await openPage()
    await advanceToProxy(wrapper)
    await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
    await wrapper.findAll('button').find((button) => button.text() === '开始授权')!.trigger('click')
    await flushPromises()
    expect(seller.createUpstream).toHaveBeenCalledWith('openai', 'oauth', undefined)
    await wrapper.findAll('button').find((button) => button.text() === '上一步')!.trigger('click')
    await wrapper.findAll('button').find((button) => button.text() === '上一步')!.trigger('click')
    const vendorButton = wrapper.findAll('.xm-pub-panel button.xm-choice').find((button) => button.text().includes('ChatGPT'))!
    expect(vendorButton.attributes('disabled')).toBeDefined()
    expect(wrapper.find('input[type="radio"]').attributes('disabled')).toBeDefined()
    await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('配置出口代理')
    await wrapper.findAll('button').find((button) => button.text() === '下一步')!.trigger('click')
    expect(wrapper.get('.xm-pub-panel h2').text()).toBe('接入上游账号')
    expect(seller.createUpstream).toHaveBeenCalledTimes(1)
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
