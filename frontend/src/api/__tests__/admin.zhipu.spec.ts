import { beforeEach, describe, expect, it, vi } from 'vitest'
import { zhipuResult, zhipuSession, zhipuCallback } from '@/__tests__/fixtures/zhipuOAuth'
const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post } }))
import zhipuAPI from '@/api/admin/zhipu'
import { adminAPI } from '@/api/admin'

describe('admin Zhipu OAuth API', () => {
  beforeEach(() => post.mockReset())
  it.each(['bigmodel', 'zai'] as const)('initializes %s with proxy and abort signal', async provider => {
    post.mockResolvedValue({ data: { ...zhipuSession, provider } })
    const signal = new AbortController().signal
    expect(adminAPI.zhipu).toBe(zhipuAPI)
    expect(await zhipuAPI.initOAuth({ provider, proxy_id: 8 }, signal)).toEqual({ ...zhipuSession, provider })
    expect(post).toHaveBeenCalledWith('/admin/zhipu/oauth/init', { provider, proxy_id: 8 }, { signal })
  })
  it.each(['pending', 'success', 'expired', 'failed'] as const)('preserves HTTP 200 %s response', async status => {
    const response = { status, interval: 4, result: status === 'success' ? zhipuResult : undefined }
    post.mockResolvedValue({ data: response })
    expect(await zhipuAPI.pollOAuth({ session_id: 'glm-session', proxy_id: 8 })).toEqual(response)
    expect(post).toHaveBeenCalledWith('/admin/zhipu/oauth/poll', { session_id: 'glm-session', proxy_id: 8 }, { signal: undefined })
  })
  it.each([{ callback_url: zhipuCallback }, { code: 'code', state: 'state' }])('passes manual exchange unchanged: %o', async input => {
    post.mockResolvedValue({ data: zhipuResult })
    const signal = new AbortController().signal
    const payload = { session_id: 'glm-session', proxy_id: 8, ...input }
    expect(await zhipuAPI.exchangeCode(payload, signal)).toEqual(zhipuResult)
    expect(post).toHaveBeenCalledWith('/admin/zhipu/oauth/exchange-code', payload, { signal })
  })
  it.each([false, true])('refreshes an existing account (wrapped=%s)', async wrapped => {
    const account = { id: 7, platform: 'zhipu', type: 'oauth' }
    post.mockResolvedValue({ data: wrapped ? { account } : account })
    expect(await zhipuAPI.refreshAccount(7)).toEqual(account)
    expect(post).toHaveBeenCalledWith('/admin/zhipu/accounts/7/refresh')
  })
})
