import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import OnboardingWizard from '@/components/OnboardingWizard.vue'
import { market, seller } from '@/api'
import type { MarketConfig, Proxy } from '@/api/types'

vi.mock('@/api', () => ({ market: { config: vi.fn() }, seller: { proxies: vi.fn(), createUpstream: vi.fn() } }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const proxy = (id: number, extra: Partial<Proxy> = {}): Proxy => ({ id, name: `proxy-${id}`, protocol: 'socks5', host: '203.0.113.10', port: 1080, status: 'available', sub2api_proxy_id: id, created_at: '', ...extra })
const dialog = defineComponent({ template: '<div><slot /><slot name="footer" /></div>' })

async function openWizard(required = true) {
  vi.mocked(market.config).mockResolvedValue({ require_seller_proxy: required } as MarketConfig)
  const wrapper = mount(OnboardingWizard, {
    props: { open: false, vendors: [{ platform: 'anthropic', auth_types: ['oauth'], consignment_min: 0.1, consignment_max: 2, self_reserve_supported: true }] },
    global: { plugins: [createPinia()], stubs: { ModalDialog: dialog, RouterLink: { template: '<a><slot /></a>' } } },
  })
  await wrapper.setProps({ open: true })
  await flushPromises()
  return wrapper
}

describe('onboarding proxy selection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(seller.proxies).mockResolvedValue([proxy(1), proxy(2, { status: 'assigned', assigned_upstream_id: 7 }), proxy(3, { status: 'disabled' }), proxy(4, { sub2api_proxy_id: undefined })])
    vi.mocked(seller.createUpstream).mockResolvedValue({ id: 10, platform: 'anthropic', auth_type: 'oauth' } as Awaited<ReturnType<typeof seller.createUpstream>>)
  })

  it('requires explicit selection and excludes occupied, disabled and pending proxies', async () => {
    const wrapper = await openWizard()
    const select = wrapper.get('#onboarding-proxy')
    expect(select.findAll('option').map((o) => o.attributes('value'))).toEqual(['', '1'])
    const next = wrapper.findAll('button').find((b) => b.text() === '下一步')!
    expect(next.attributes('disabled')).toBeDefined()
    await select.setValue('1')
    expect(next.attributes('disabled')).toBeUndefined()
    await next.trigger('click')
    await flushPromises()
    expect(seller.createUpstream).toHaveBeenCalledWith('anthropic', 'oauth', 1)
    expect(wrapper.text()).toContain('上游 #10')
    wrapper.unmount()
  })

  it('offers platform allocation when seller proxies are optional', async () => {
    vi.mocked(seller.proxies).mockResolvedValue([])
    const wrapper = await openWizard(false)
    expect(wrapper.get('#onboarding-proxy').text()).toContain('由平台自动关联专用代理')
    const next = wrapper.findAll('button').find((b) => b.text() === '下一步')!
    expect(next.attributes('disabled')).toBeUndefined()
    await next.trigger('click')
    await flushPromises()
    expect(seller.createUpstream).toHaveBeenCalledWith('anthropic', 'oauth', undefined)
    wrapper.unmount()
  })

  it('blocks creation until a failed proxy list request is successfully retried', async () => {
    vi.mocked(seller.proxies).mockRejectedValueOnce(new Error('offline'))
    const wrapper = await openWizard()
    expect(wrapper.text()).toContain('代理列表加载失败')
    const next = wrapper.findAll('button').find((b) => b.text() === '下一步')!
    expect(next.attributes('disabled')).toBeDefined()
    await wrapper.findAll('button').find((b) => b.text() === '刷新列表')!.trigger('click')
    await flushPromises()
    await wrapper.get('#onboarding-proxy').setValue('1')
    expect(next.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
