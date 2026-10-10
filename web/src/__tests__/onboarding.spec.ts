import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import OnboardingWizard from '@/components/OnboardingWizard.vue'
import { market, seller } from '@/api'
import type { MarketConfig, Proxy } from '@/api/types'

vi.mock('@/api', () => ({ market: { config: vi.fn() }, seller: { proxies: vi.fn(), createUpstream: vi.fn(), importAccount: vi.fn(), upstream: vi.fn() } }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const connected = { success: true, message: 'ok', latency_ms: 120 }
const proxy = (id: number, extra: Partial<Proxy> = {}): Proxy => ({ id, name: `proxy-${id}`, protocol: 'socks5', host: '203.0.113.10', port: 1080, status: 'available', sub2api_proxy_id: id, created_at: '', connection_report: connected, ...extra })
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
    vi.mocked(seller.createUpstream).mockResolvedValue({ id: 10, platform: 'anthropic', auth_type: 'oauth', proxy_id: 1 } as Awaited<ReturnType<typeof seller.createUpstream>>)
  })

  it('requires explicit selection and excludes occupied, disabled and pending proxies', async () => {
    const wrapper = await openWizard()
    const select = wrapper.get('#onboarding-proxy')
    expect(select.findAll('option').map((o) => o.attributes('value'))).toEqual(['', 'import', '1'])
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

  it('only offers proxies whose latest connection test succeeded', async () => {
    vi.mocked(seller.proxies).mockResolvedValue([
      proxy(1, { connection_report: undefined }),
      proxy(2, { connection_report: { success: false, message: 'timeout', latency_ms: 0 } }),
      proxy(3, { host: '203.0.113.11' }),
    ])
    const wrapper = await openWizard()
    expect(wrapper.get('#onboarding-proxy').findAll('option').map((o) => o.attributes('value'))).toEqual(['', 'import', '3'])
    expect(wrapper.text()).toContain('另有 2 个空闲代理尚未检测或检测未通过')
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

  it('imports one selected account from a sub2api export without an OAuth login', async () => {
    const wrapper = await openWizard()
    await wrapper.get('#onboarding-proxy').setValue('1')
    await wrapper.findAll('button').find((b) => b.text() === '下一步')!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === '导入 sub2api 账号')!.trigger('click')
    const submit = () => wrapper.findAll('button').find((b) => b.text() === '导入并检测')!
    expect(submit().attributes('disabled')).toBeDefined()
    await wrapper.get('#account-import-json').setValue('{bad json')
    expect(wrapper.text()).toContain('请输入有效的 JSON')
    const data = { type: 'sub2api-data', version: 1, accounts: [
      { name: 'OpenAI account', platform: 'openai', type: 'oauth', credentials: { access_token: 'test-oa-token' } },
      { name: 'Claude account', platform: 'anthropic', type: 'oauth', credentials: { access_token: 'test-claude-token' } },
    ] }
    await wrapper.get('#account-import-json').setValue(JSON.stringify(data))
    expect(wrapper.text()).toContain('与当前上游不一致')
    expect(submit().attributes('disabled')).toBeDefined()
    await wrapper.get('#account-import-selection').setValue(1)
    expect(submit().attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('导入账号沿用本上游已绑定的代理')
    vi.mocked(seller.importAccount).mockResolvedValue({ id: 10, status: 'verifying', platform: 'anthropic', auth_type: 'oauth' } as Awaited<ReturnType<typeof seller.importAccount>>)
    await submit().trigger('click')
    await flushPromises()
    expect(seller.importAccount).toHaveBeenCalledWith(10, data, 1)
    expect(wrapper.text()).toContain('正在进行上架检测')
    wrapper.unmount()
  })

  it('defers the proxy to the import file and blocks accounts without one', async () => {
    vi.mocked(seller.createUpstream).mockResolvedValue({ id: 11, platform: 'anthropic', auth_type: 'oauth' } as Awaited<ReturnType<typeof seller.createUpstream>>)
    const wrapper = await openWizard()
    await wrapper.get('#onboarding-proxy').setValue('import')
    expect(wrapper.text()).toContain('连通性检测通过才会导入')
    await wrapper.findAll('button').find((b) => b.text() === '下一步')!.trigger('click')
    await flushPromises()
    expect(seller.createUpstream).toHaveBeenCalledWith('anthropic', 'oauth', undefined, true)
    expect(wrapper.findAll('button').find((b) => b.text() === '官方授权')!.attributes('disabled')).toBeDefined()
    const submit = () => wrapper.findAll('button').find((b) => b.text() === '导入并检测')!
    const key = 'socks5|198.51.100.7|1080|user|secret-pass'
    const data = { type: 'sub2api-data', version: 1,
      proxies: [{ proxy_key: key, name: 'exported ip', protocol: 'socks5', host: '198.51.100.7', port: 1080, username: 'user', password: 'secret-pass', status: 'active' }],
      accounts: [
        { name: 'no proxy', platform: 'anthropic', type: 'oauth', credentials: { access_token: 'a' } },
        { name: 'with proxy', platform: 'anthropic', type: 'oauth', credentials: { access_token: 'b' }, proxy_key: key },
        { name: 'dangling', platform: 'anthropic', type: 'oauth', credentials: { access_token: 'c' }, proxy_key: 'http|missing|1|x|y' },
      ] }
    await wrapper.get('#account-import-json').setValue(JSON.stringify(data))
    expect(wrapper.text()).toContain('所选账号在导入文件中没有代理，无法导入')
    expect(submit().attributes('disabled')).toBeDefined()
    await wrapper.get('#account-import-selection').setValue(2)
    expect(wrapper.text()).toContain('导入文件中缺少该账号引用的代理')
    expect(submit().attributes('disabled')).toBeDefined()
    await wrapper.get('#account-import-selection').setValue(1)
    expect(wrapper.text()).toContain('exported ip · socks5://198.51.100.7:1080')
    expect(wrapper.text()).not.toContain('secret-pass')
    expect(submit().attributes('disabled')).toBeUndefined()
    vi.mocked(seller.importAccount).mockResolvedValue({ id: 11, status: 'verifying', platform: 'anthropic', auth_type: 'oauth', proxy_id: 5 } as Awaited<ReturnType<typeof seller.importAccount>>)
    await submit().trigger('click')
    await flushPromises()
    expect(seller.importAccount).toHaveBeenCalledWith(11, data, 1)
    expect(wrapper.text()).toContain('正在进行上架检测')
    wrapper.unmount()
  })
})
