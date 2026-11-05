import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  bindPendingRewardTicket,
  captureRewardTicketFromUrl,
  readRewardTicket,
  resolveRewardLanding,
  restoreRewardSdk,
  rewardSignupFields,
  REWARD_STORAGE_KEY,
  REWARD_TTL_MS,
  SDK_INFO_KEY,
  SDK_TICKET_KEY,
} from '@/utils/rewardTicket'

const TICKET = 'rt_0123456789abcdef0123456789abcdef01234567'
const OTHER = 'rt_fedcba9876543210fedcba9876543210fedcba98'
const SDK = { script_url: 'https://loxo.cc/reward-task-sdk.js', app_key: 'rk_test_key' }

function setUrl(search: string, hash = '') {
  window.history.replaceState(null, '', `/${search}${hash}`)
}

function jsonResponse(status: number, body: unknown) {
  return { ok: status >= 200 && status < 300, status, json: async () => body, text: async () => JSON.stringify(body) }
}

function sdkScripts() {
  return document.querySelectorAll('script[data-tm-reward-sdk]')
}

describe('loxo 任务票据', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setUrl('')
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    sdkScripts().forEach((el) => el.remove())
    localStorage.clear()
    sessionStorage.clear()
    setUrl('')
  })

  it('落地：合法票据留存并从地址栏去掉，其余参数与 hash 保留', () => {
    setUrl(`?from=loxo&reward_ticket=${TICKET}&ch=v2ex`, '#signup')
    expect(captureRewardTicketFromUrl()).toBe(TICKET)
    expect(window.location.search).toBe('?from=loxo&ch=v2ex')
    expect(window.location.hash).toBe('#signup')
    expect(readRewardTicket()?.ticket).toBe(TICKET)
    expect(rewardSignupFields()).toEqual({ reward_ticket: TICKET })
  })

  it('非法票据：地址栏照样清理，但不留存', () => {
    setUrl('?reward_ticket=<script>alert(1)</script>')
    expect(captureRewardTicketFromUrl()).toBeNull()
    expect(window.location.search).toBe('')
    expect(localStorage.getItem(REWARD_STORAGE_KEY)).toBeNull()
    expect(rewardSignupFields()).toEqual({})
  })

  it('没有票据参数时不动地址栏和旧记录', () => {
    localStorage.setItem(REWARD_STORAGE_KEY, JSON.stringify({ ticket: TICKET, touched_at: new Date().toISOString() }))
    setUrl('?q=1')
    expect(captureRewardTicketFromUrl()).toBeNull()
    expect(window.location.search).toBe('?q=1')
    expect(readRewardTicket()?.ticket).toBe(TICKET)
  })

  it('超过 30 天或被篡改的记录不再使用', () => {
    const old = new Date(Date.now() - REWARD_TTL_MS - 1000).toISOString()
    localStorage.setItem(REWARD_STORAGE_KEY, JSON.stringify({ ticket: TICKET, touched_at: old }))
    expect(readRewardTicket()).toBeNull()
    localStorage.setItem(REWARD_STORAGE_KEY, JSON.stringify({ ticket: 'rt_<bad>', touched_at: new Date().toISOString() }))
    expect(rewardSignupFields()).toEqual({})
    localStorage.setItem(REWARD_STORAGE_KEY, '{oops')
    expect(readRewardTicket()).toBeNull()
  })

  it('浏览任务：写入 SDK 会话键并插入一次 SDK 脚本，不再等待绑定', async () => {
    setUrl(`?reward_ticket=${TICKET}`)
    captureRewardTicketFromUrl()
    fetchMock.mockResolvedValue(jsonResponse(200, { data: { valid: true, task_type: 'browse', sdk: SDK } }))
    const landing = await resolveRewardLanding(TICKET)

    expect(landing?.task_type).toBe('browse')
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/market/reward/landing')
    expect(JSON.parse(String(init.body))).toEqual({ ticket: TICKET })
    expect(sessionStorage.getItem(SDK_TICKET_KEY)).toBe(TICKET)
    expect(JSON.parse(sessionStorage.getItem(SDK_INFO_KEY) || '{}')).toEqual(SDK)
    expect(readRewardTicket()).toBeNull()
    const scripts = sdkScripts()
    expect(scripts).toHaveLength(1)
    expect(scripts[0].getAttribute('src')).toBe(SDK.script_url)
    expect(scripts[0].getAttribute('data-app-key')).toBe(SDK.app_key)

    // 同一页面不会重复插入
    expect(restoreRewardSdk()).toBe(true)
    expect(sdkScripts()).toHaveLength(1)
  })

  it('刷新后会话里还有 SDK 票据时重新加载；SDK 已完成（票据被删）时不加载', () => {
    sessionStorage.setItem(SDK_INFO_KEY, JSON.stringify(SDK))
    expect(restoreRewardSdk()).toBe(false)
    sessionStorage.setItem(SDK_TICKET_KEY, TICKET)
    expect(restoreRewardSdk()).toBe(true)
    expect(sdkScripts()).toHaveLength(1)
  })

  it('不接受非 SDK 地址的脚本', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { data: { valid: true, task_type: 'browse', sdk: { script_url: 'javascript:alert(1)', app_key: 'k' } } }))
    await resolveRewardLanding(TICKET)
    expect(sdkScripts()).toHaveLength(0)
    expect(sessionStorage.getItem(SDK_TICKET_KEY)).toBeNull()
  })

  it('无效票据丢弃；注册 / 充值任务与查询失败都保留等待绑定', async () => {
    const keep = () => localStorage.setItem(REWARD_STORAGE_KEY, JSON.stringify({ ticket: TICKET, touched_at: new Date().toISOString() }))

    keep()
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { data: { valid: false, task_type: '', sdk: null } }))
    await resolveRewardLanding(TICKET)
    expect(readRewardTicket()).toBeNull()

    keep()
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { data: { valid: true, task_type: 'recharge', sdk: null } }))
    await resolveRewardLanding(TICKET)
    expect(readRewardTicket()?.ticket).toBe(TICKET)

    fetchMock.mockResolvedValueOnce(jsonResponse(200, { data: { valid: null, task_type: '', sdk: null } }))
    await resolveRewardLanding(TICKET)
    fetchMock.mockRejectedValueOnce(new TypeError('offline'))
    await expect(resolveRewardLanding(TICKET)).resolves.toBeNull()
    fetchMock.mockResolvedValueOnce(jsonResponse(502, {}))
    await resolveRewardLanding(TICKET)
    expect(readRewardTicket()?.ticket).toBe(TICKET)
    expect(sdkScripts()).toHaveLength(0)
  })

  it('异步结果不会清掉用户随后带来的另一张票据', async () => {
    localStorage.setItem(REWARD_STORAGE_KEY, JSON.stringify({ ticket: OTHER, touched_at: new Date().toISOString() }))
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { data: { valid: false, task_type: '', sdk: null } }))
    await resolveRewardLanding(TICKET)
    expect(readRewardTicket()?.ticket).toBe(OTHER)
  })

  it('登录后绑定：成功或被拒都清掉，网络错误保留下次再试', async () => {
    const keep = () => localStorage.setItem(REWARD_STORAGE_KEY, JSON.stringify({ ticket: TICKET, touched_at: new Date().toISOString() }))

    keep()
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { data: { accepted: true } }))
    await bindPendingRewardTicket()
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/me/reward-ticket')
    expect(init.method).toBe('POST')
    expect(JSON.parse(String(init.body))).toEqual({ ticket: TICKET })
    expect(readRewardTicket()).toBeNull()

    keep()
    fetchMock.mockResolvedValueOnce(jsonResponse(400, { error: { code: 'INVALID_REWARD_TICKET', message: 'bad' } }))
    await bindPendingRewardTicket()
    expect(readRewardTicket()).toBeNull()

    keep()
    fetchMock.mockRejectedValueOnce(new TypeError('offline'))
    await bindPendingRewardTicket()
    expect(readRewardTicket()?.ticket).toBe(TICKET)

    fetchMock.mockResolvedValueOnce(jsonResponse(503, { error: { code: 'X', message: 'down' } }))
    await bindPendingRewardTicket()
    expect(readRewardTicket()?.ticket).toBe(TICKET)
  })

  it('没有票据时绑定不发请求', async () => {
    await bindPendingRewardTicket()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
