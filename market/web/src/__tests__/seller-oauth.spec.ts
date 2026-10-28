import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { seller } from '@/api'
import type { AuthorizationStart, Upstream } from '@/api/types'
import { useSellerOAuth } from '@/composables/useSellerOAuth'
import SellerOAuthPanel from '@/components/SellerOAuthPanel.vue'
import { copyText } from '@/utils/clipboard'
import { authTypeText } from '@/utils/labels'

vi.mock('@/api', () => ({ seller: { startOAuth: vi.fn(), reauthorize: vi.fn(), oauthPoll: vi.fn(), completeOAuth: vi.fn() } }))
vi.mock('@/utils/clipboard', () => ({ copyText: vi.fn().mockResolvedValue(true) }))

const upstream = { id: 7, platform: 'kimi', auth_type: 'oauth', status: 'selecting_models' } as Upstream
const session = (overrides: Partial<AuthorizationStart> = {}): AuthorizationStart => ({
  auth_url: '', flow: 'device-code', expires_at: new Date(Date.now() + 60000).toISOString(),
  user_code: 'ABCD-EFGH', verification_uri: 'https://auth.kimi.com/device', verification_uri_complete: 'https://auth.kimi.com/device?user_code=ABCD-EFGH',
  ...overrides,
})
const cleanups: (() => void)[] = []

function harness(reauthorize = false) {
  const scope = effectScope()
  const onReady = vi.fn()
  const flow = scope.run(() => useSellerOAuth(() => 7, () => reauthorize, onReady))!
  cleanups.push(() => scope.stop())
  return { flow, scope, onReady }
}

function panel(platform = 'kimi', authType = 'oauth', reauthorize = false) {
  const wrapper = mount(SellerOAuthPanel, {
    props: { upstreamId: 7, platform, authType, reauthorize }, global: { plugins: [createPinia()] },
  })
  cleanups.push(() => wrapper.unmount())
  return wrapper
}

async function click(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  expect(button, text).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-28T10:00:00Z'))
  vi.clearAllMocks()
  vi.mocked(seller.startOAuth).mockResolvedValue(session())
  vi.mocked(seller.reauthorize).mockResolvedValue(session())
  vi.mocked(seller.oauthPoll).mockResolvedValue({ status: 'pending', interval: 5 })
  vi.mocked(seller.completeOAuth).mockResolvedValue(upstream)
})

afterEach(() => {
  cleanups.splice(0).forEach((cleanup) => cleanup())
  vi.useRealTimers()
})

describe('卖家 OAuth 轮询生命周期', () => {
  it('默认每五秒查询，pending 后继续，ready interval=0 时只完成一次并停止', async () => {
    vi.mocked(seller.oauthPoll).mockResolvedValueOnce({ status: 'pending', interval: 5 }).mockResolvedValueOnce({ status: 'ready', interval: 0, upstream })
    const { flow, onReady } = harness()
    await flow.start()
    expect(seller.oauthPoll).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(5000)
    expect(seller.oauthPoll).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(onReady).toHaveBeenCalledTimes(1)
    expect(onReady).toHaveBeenCalledWith(upstream)
    expect(flow.active.value).toBe(false)
    await vi.advanceTimersByTimeAsync(60000)
    expect(seller.oauthPoll).toHaveBeenCalledTimes(2)
    expect(flow.error.value).toBe('')
  })

  it('使用 start 的 interval；slow_down 完全采用后端间隔，不再额外增加五秒', async () => {
    vi.mocked(seller.startOAuth).mockResolvedValue(session({ interval: 2 }))
    vi.mocked(seller.oauthPoll).mockResolvedValue({ status: 'slow_down', interval: 12 })
    const { flow } = harness()
    await flow.start()
    await vi.advanceTimersByTimeAsync(1999)
    expect(seller.oauthPoll).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(seller.oauthPoll).toHaveBeenCalledTimes(1)
    expect(flow.status.value).toContain('降低查询频率')
    await vi.advanceTimersByTimeAsync(11999)
    expect(seller.oauthPoll).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(seller.oauthPoll).toHaveBeenCalledTimes(2)
  })

  it.each(['expired', 'denied'] as const)('%s interval=0 终止，不会对已删除的会话继续查询', async (status) => {
    vi.mocked(seller.oauthPoll).mockResolvedValue({ status, interval: 0 })
    const { flow, onReady } = harness()
    await flow.start()
    await vi.advanceTimersByTimeAsync(5000)
    expect(flow.error.value).toContain(status === 'expired' ? '已过期' : '拒绝')
    expect(flow.error.value).toContain('重新发起')
    await vi.advanceTimersByTimeAsync(60000)
    expect(seller.oauthPoll).toHaveBeenCalledTimes(1)
    expect(onReady).not.toHaveBeenCalled()
  })

  it('按 RFC3339 时区计算到期时间，等待中的请求也在到期时中止', async () => {
    vi.mocked(seller.startOAuth).mockResolvedValue(session({ expires_at: '2026-10-28T18:00:03+08:00', interval: 1 }))
    vi.mocked(seller.oauthPoll).mockReturnValue(new Promise(() => {}))
    const { flow } = harness()
    await flow.start()
    expect(flow.countdown.value).toBe('0:03')
    await vi.advanceTimersByTimeAsync(1000)
    const signal = vi.mocked(seller.oauthPoll).mock.calls[0]![1]!
    await vi.advanceTimersByTimeAsync(2000)
    expect(flow.countdown.value).toBe('0:00')
    expect(signal.aborted).toBe(true)
    expect(flow.error.value).toContain('已过期')
    await vi.advanceTimersByTimeAsync(10000)
    expect(seller.oauthPoll).toHaveBeenCalledTimes(1)
  })

  it('防重复启动和查询重入；卸载中止在途请求，忽略迟到结果', async () => {
    let resolvePoll!: (value: { status: 'ready'; interval: number; upstream: Upstream }) => void
    vi.mocked(seller.oauthPoll).mockImplementation(() => new Promise((resolve) => { resolvePoll = resolve }))
    const { flow, scope, onReady } = harness()
    await Promise.all([flow.start(), flow.start()])
    await flow.start()
    expect(seller.startOAuth).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(20000)
    expect(seller.oauthPoll).toHaveBeenCalledTimes(1)
    const signal = vi.mocked(seller.oauthPoll).mock.calls[0]![1]!
    scope.stop()
    expect(signal.aborted).toBe(true)
    resolvePoll({ status: 'ready', interval: 0, upstream })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(60000)
    expect(onReady).not.toHaveBeenCalled()
    expect(seller.oauthPoll).toHaveBeenCalledTimes(1)
  })

  it('组件销毁前 start 尚未返回时，不得重新开启定时器', async () => {
    let resolveStart!: (value: AuthorizationStart) => void
    vi.mocked(seller.startOAuth).mockImplementation(() => new Promise((resolve) => { resolveStart = resolve }))
    const { flow, scope } = harness()
    const pending = flow.start()
    const signal = vi.mocked(seller.startOAuth).mock.calls[0]![2]!
    scope.stop()
    resolveStart(session())
    await pending
    expect(signal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(60000)
    expect(seller.oauthPoll).not.toHaveBeenCalled()
  })

  it('手动完成防重复提交并取消轮询；失败展示可读原因', async () => {
    vi.mocked(seller.startOAuth).mockResolvedValue(session({ flow: 'init-poll', auth_url: 'https://bigmodel.cn/oauth' }))
    let rejectComplete!: (reason: Error) => void
    vi.mocked(seller.completeOAuth).mockImplementation(() => new Promise((_, reject) => { rejectComplete = reject }))
    const { flow } = harness()
    await flow.start()
    const pending = flow.complete({ callback_url: 'authCode=abc&state=xyz' })
    await flow.complete({ callback_url: 'authCode=abc&state=xyz' })
    expect(seller.completeOAuth).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(10000)
    expect(seller.oauthPoll).not.toHaveBeenCalled()
    rejectComplete(new Error('订阅状态无法验证，请确认已开通 Coding Plan'))
    await pending
    expect(flow.error.value).toContain('订阅状态无法验证')
  })

  it('Kimi 不会通过 complete 提交', async () => {
    const { flow } = harness()
    await flow.start()
    await flow.complete({ code: 'abc' })
    expect(seller.completeOAuth).not.toHaveBeenCalled()
  })
})

describe('卖家 OAuth 面板', () => {
  it('Kimi 显示设备码而非授权码输入；空 auth_url 正常；可复制设备码和链接', async () => {
    const wrapper = panel()
    expect(wrapper.find('textarea').exists()).toBe(false)
    await click(wrapper, '获取设备码')
    expect(wrapper.get('[data-testid="user-code"]').text()).toBe('ABCD-EFGH')
    expect(wrapper.text()).toContain('剩余时间：1:00')
    expect(wrapper.get('a[target="_blank"]').attributes('href')).toBe(session().verification_uri_complete)
    await click(wrapper, '复制设备码')
    expect(copyText).toHaveBeenLastCalledWith('ABCD-EFGH')
    await click(wrapper, '复制授权链接')
    expect(copyText).toHaveBeenLastCalledWith(session().verification_uri_complete)
    await click(wrapper, '取消本次授权')
    await vi.advanceTimersByTimeAsync(60000)
    expect(seller.oauthPoll).not.toHaveBeenCalled()
  })

  it.each(['oauth', 'oauth-intl'])('智谱 %s 使用 init-poll 链接与可选手动回调', async (authType) => {
    vi.mocked(seller.startOAuth).mockResolvedValue(session({ flow: 'init-poll', auth_url: 'https://z.ai/oauth' }))
    const wrapper = panel('zhipu', authType)
    await click(wrapper, '获取授权链接')
    expect(wrapper.get('summary').text()).toBe('手动粘贴回调地址')
    expect(wrapper.find('[data-testid="user-code"]').exists()).toBe(false)
    expect(wrapper.get('a').attributes('href')).toBe('https://z.ai/oauth')
  })

  it.each([
    'zcode://oauth/callback?code=abc%2B123&state=xyz',
    'zcode://oauth/callback?authCode=abc%2B123&state=xyz',
    'code=abc%2B123&state=xyz',
    'authCode=abc%2B123&state=xyz',
    '{"authCode":"abc+123","state":"xyz"}',
  ])('完整回调原文交给后端解析：%s', async (callback) => {
    vi.mocked(seller.startOAuth).mockResolvedValue(session({ flow: 'init-poll', auth_url: 'https://bigmodel.cn/oauth' }))
    const wrapper = panel('zhipu')
    await click(wrapper, '获取授权链接')
    await wrapper.get('textarea').setValue(callback)
    await click(wrapper, '提交回调地址')
    expect(seller.completeOAuth).toHaveBeenCalledWith(7, { callback_url: callback }, expect.any(AbortSignal))
    expect(wrapper.emitted('ready')?.[0]).toEqual([upstream])
    await vi.advanceTimersByTimeAsync(60000)
    expect(seller.oauthPoll).not.toHaveBeenCalled()
  })

  it('保留 link-code 授权码输入及 GCP 项目参数，不自动轮询', async () => {
    vi.mocked(seller.startOAuth).mockResolvedValue(session({ flow: 'link-code', auth_url: 'https://accounts.google.com/oauth' }))
    const wrapper = panel('gemini')
    await wrapper.get('input').setValue('my-project')
    await click(wrapper, '获取授权链接')
    expect(seller.startOAuth).toHaveBeenCalledWith(7, { project_id: 'my-project' }, expect.any(AbortSignal))
    await vi.advanceTimersByTimeAsync(5000)
    expect(seller.oauthPoll).not.toHaveBeenCalled()
    await wrapper.get('textarea').setValue('https://localhost/callback?code=abc&state=xyz')
    await click(wrapper, '提交')
    expect(seller.completeOAuth).toHaveBeenCalledWith(7, { code: 'https://localhost/callback?code=abc&state=xyz' }, expect.any(AbortSignal))
  })

  it('重新授权调用 reauthorize；组件卸载中止请求并停止自动查询', async () => {
    const wrapper = panel('kimi', 'oauth', true)
    await click(wrapper, '获取设备码')
    expect(seller.reauthorize).toHaveBeenCalledWith(7, undefined, expect.any(AbortSignal))
    expect(seller.startOAuth).not.toHaveBeenCalled()
    const signal = vi.mocked(seller.reauthorize).mock.calls[0]![2]!
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(60000)
    expect(seller.oauthPoll).not.toHaveBeenCalled()
  })

  it('切换上游或 authType 时中止旧会话，切回 link-code UI', async () => {
    const wrapper = panel()
    await click(wrapper, '获取设备码')
    const signal = vi.mocked(seller.startOAuth).mock.calls[0]![2]!
    await wrapper.setProps({ upstreamId: 8, platform: 'openai' })
    expect(signal.aborted).toBe(true)
    expect(wrapper.find('textarea').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(60000)
    expect(seller.oauthPoll).not.toHaveBeenCalled()
  })

  it('新接入方式的标签准确区分 Kimi、BigModel 与 Z.ai', () => {
    expect(authTypeText('kimi', 'oauth')).toBe('OAuth 授权（Coding Plan）')
    expect(authTypeText('zhipu', 'oauth')).toBe('OAuth 授权 · 国内站（BigModel）')
    expect(authTypeText('zhipu', 'oauth-intl')).toBe('OAuth 授权 · 国际站（Z.ai）')
  })
})
