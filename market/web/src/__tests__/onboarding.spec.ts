import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import OnboardingWizard from '@/components/OnboardingWizard.vue'
import { market, seller } from '@/api'
import type { MarketConfig, Proxy, Upstream } from '@/api/types'

vi.mock('@/api', () => ({ market: { config: vi.fn() }, seller: { proxies: vi.fn(), createUpstream: vi.fn(), importAccount: vi.fn(), upstream: vi.fn(), completeOAuth: vi.fn(), submitCodingKey: vi.fn(), upstreamModels: vi.fn(), refreshUpstreamModels: vi.fn(), setUpstreamModels: vi.fn(), retryVerification: vi.fn() } }))
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
    vi.mocked(seller.upstreamModels).mockResolvedValue({ catalog: ['claude-sonnet-4-5', 'claude-opus-4-1'], whitelist: null, fetched_at: null })
    vi.mocked(seller.setUpstreamModels).mockResolvedValue({ id: 10, status: 'ready', platform: 'anthropic', auth_type: 'oauth' } as Upstream)
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
    const submit = () => wrapper.findAll('button').find((b) => b.text() === '导入账号')!
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
    vi.mocked(seller.importAccount).mockResolvedValue({ id: 10, status: 'selecting_models', platform: 'anthropic', auth_type: 'oauth' } as Awaited<ReturnType<typeof seller.importAccount>>)
    await submit().trigger('click')
    await flushPromises()
    expect(seller.importAccount).toHaveBeenCalledWith(10, data, 1)
    expect(wrapper.text()).toContain('选择挂售模型')
    await wrapper.findAll('button').find((b) => b.text() === '保存挂售模型')!.trigger('click')
    await flushPromises()
    expect(seller.setUpstreamModels).toHaveBeenCalledWith(10, ['claude-sonnet-4-5', 'claude-opus-4-1'])
    expect(wrapper.text()).toContain('可以直接发布挂单')
    expect(wrapper.text()).toContain('未检测')
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
    const submit = () => wrapper.findAll('button').find((b) => b.text() === '导入账号')!
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
    vi.mocked(seller.importAccount).mockResolvedValue({ id: 11, status: 'selecting_models', platform: 'anthropic', auth_type: 'oauth', proxy_id: 5 } as Awaited<ReturnType<typeof seller.importAccount>>)
    await submit().trigger('click')
    await flushPromises()
    expect(seller.importAccount).toHaveBeenCalledWith(11, data, 1)
    expect(wrapper.text()).toContain('选择挂售模型')
    await wrapper.findAll('button').find((b) => b.text() === '保存挂售模型')!.trigger('click')
    await flushPromises()
    expect(seller.setUpstreamModels).toHaveBeenCalledWith(11, ['claude-sonnet-4-5', 'claude-opus-4-1'])
    expect(wrapper.text()).toContain('可以直接发布挂单')
    expect(wrapper.text()).toContain('未检测')
    wrapper.unmount()
  })

  it('OAuth 授权后先选择模型，提交白名单后即可挂单且明确未检测', async () => {
    vi.mocked(seller.completeOAuth).mockResolvedValue({ id: 10, status: 'selecting_models', platform: 'anthropic', auth_type: 'oauth' } as Upstream)
    const wrapper = await openWizard()
    await wrapper.get('#onboarding-proxy').setValue('1')
    await wrapper.findAll('button').find((b) => b.text() === '下一步')!.trigger('click')
    await flushPromises()
    await wrapper.get('#code').setValue('授权码')
    await wrapper.findAll('button').find((b) => b.text() === '提交')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('选择挂售模型')
    expect(seller.setUpstreamModels).not.toHaveBeenCalled()
    await wrapper.findAll('input[type="checkbox"]')[1]!.setValue(false)
    await wrapper.findAll('button').find((b) => b.text() === '保存挂售模型')!.trigger('click')
    await flushPromises()
    expect(seller.setUpstreamModels).toHaveBeenCalledWith(10, ['claude-sonnet-4-5'])
    expect(wrapper.text()).toContain('可以直接发布挂单')
    expect(wrapper.text()).toContain('未检测')
    wrapper.unmount()
  })

  it('Coding Key 接入后进入选择模型，并可从该状态继续接入', async () => {
    vi.mocked(seller.submitCodingKey).mockResolvedValue({ id: 10, status: 'selecting_models', platform: 'zhipu', auth_type: 'coding' } as Upstream)
    const wrapper = await openWizard()
    await wrapper.setProps({ open: false, resume: { id: 10, platform: 'zhipu', auth_type: 'coding', proxy_id: 1, status: 'draft' } as Upstream })
    await wrapper.setProps({ open: true })
    await flushPromises()
    await wrapper.get('#key').setValue('订阅密钥')
    await wrapper.findAll('button').find((b) => b.text() === '提交')!.trigger('click')
    await flushPromises()
    expect(seller.submitCodingKey).toHaveBeenCalledWith(10, { api_key: '订阅密钥' })
    expect(wrapper.text()).toContain('选择挂售模型')
    await wrapper.setProps({ open: false, resume: { id: 12, platform: 'zhipu', auth_type: 'coding', status: 'selecting_models' } as Upstream })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(wrapper.text()).toContain('选择挂售模型')
    expect(seller.upstreamModels).toHaveBeenLastCalledWith(12)
    expect(wrapper.find('#key').exists()).toBe(false)
    wrapper.unmount()
  })

  it('非空白名单保存后可直接挂单，不自动检测，卖家可主动检测', async () => {
    const wrapper = await openWizard()
    await wrapper.setProps({ open: false, resume: { id: 10, platform: 'anthropic', auth_type: 'oauth', status: 'selecting_models' } as Upstream })
    await wrapper.setProps({ open: true })
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === '清空')!.trigger('click')
    const save = () => wrapper.findAll('button').find((b) => b.text() === '保存挂售模型')!
    expect(save().attributes('disabled')).toBeDefined()
    expect(seller.setUpstreamModels).not.toHaveBeenCalled()
    await wrapper.findAll('input[type="checkbox"]')[0]!.setValue(true)
    await save().trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('可以直接发布挂单')
    expect(wrapper.text()).toContain('未检测')
    expect(seller.retryVerification).not.toHaveBeenCalled()
    expect(wrapper.findAll('button').some((b) => b.text() === '去发布挂单')).toBe(true)
    vi.mocked(seller.retryVerification).mockResolvedValue({ id: 10, platform: 'anthropic', auth_type: 'oauth', status: 'verifying' } as Upstream)
    await wrapper.findAll('button').find((b) => b.text() === '主动发起检测')!.trigger('click')
    await flushPromises()
    expect(seller.retryVerification).toHaveBeenCalledWith(10)
    expect(wrapper.text()).toContain('正在进行上架检测')
    wrapper.unmount()
  })

  it('基线账号仍按后端返回的 verifying 状态展示检测中', async () => {
    vi.mocked(seller.setUpstreamModels).mockResolvedValue({ id: 10, platform: 'anthropic', auth_type: 'oauth', status: 'verifying' } as Upstream)
    const wrapper = await openWizard()
    await wrapper.setProps({ open: false, resume: { id: 10, platform: 'anthropic', auth_type: 'oauth', status: 'selecting_models' } as Upstream })
    await wrapper.setProps({ open: true })
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === '保存挂售模型')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('正在进行上架检测')
    expect(wrapper.findAll('button').some((b) => b.text() === '去发布挂单')).toBe(false)
    wrapper.unmount()
  })

  it.each(['suspended', 'removing', 'removed'])('%s 上游不能从向导跳过检测恢复或挂单', async (status) => {
    const wrapper = await openWizard()
    await wrapper.setProps({ open: false, resume: { id: 10, platform: 'anthropic', auth_type: 'oauth', status } as Upstream })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(wrapper.findAll('button').some((b) => ['去发布挂单', '主动发起检测', '保存挂售模型'].includes(b.text()))).toBe(false)
    expect(seller.retryVerification).not.toHaveBeenCalled()
    expect(seller.setUpstreamModels).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
