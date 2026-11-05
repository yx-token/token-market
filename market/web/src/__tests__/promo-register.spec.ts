import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { auth } from '@/api'

// 用真实 api 模块 + 桩 fetch，验证注册请求体里真的带上了渠道字段
describe('注册请求体带上推广渠道字段', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    localStorage.clear()
    fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      text: async () => JSON.stringify({ data: { id: 1, email: 'buyer@market.test' } }),
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  function sentBody(index = 0) {
    const [url, init] = fetchMock.mock.calls[index] as [string, RequestInit]
    return { url, body: JSON.parse(String(init.body)) as Record<string, unknown> }
  }

  it('有渠道时合并进 body', async () => {
    await auth.register('buyer@market.test', 'pw12345678', {
      channel_code: 'v2ex-post',
      visitor_id: 'visitor-abc123',
      channel_touched_at: '2026-11-05T08:00:00.000Z',
    })
    const { url, body } = sentBody()
    expect(url).toBe('/api/auth/register')
    expect(body).toEqual({
      email: 'buyer@market.test',
      password: 'pw12345678',
      channel_code: 'v2ex-post',
      visitor_id: 'visitor-abc123',
      channel_touched_at: '2026-11-05T08:00:00.000Z',
    })
  })

  it('没有渠道时请求体与现在完全一致', async () => {
    await auth.register('buyer@market.test', 'pw12345678')
    expect(sentBody().body).toEqual({ email: 'buyer@market.test', password: 'pw12345678' })

    await auth.register('buyer@market.test', 'pw12345678', {})
    expect(sentBody(1).body).toEqual({ email: 'buyer@market.test', password: 'pw12345678' })
  })
})
