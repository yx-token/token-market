import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, http, setToken, setUnauthorizedHandler } from '@/api/client'
import { seller } from '@/api'

function mockFetch(status: number, body: unknown) {
  return vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
}

describe('api client', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    setToken(null)
  })

  it('unwraps data and sends the bearer token and query', async () => {
    const f = mockFetch(200, { data: { id: 1 } })
    vi.stubGlobal('fetch', f)
    setToken('tok')
    const res = await http.get<{ id: number }>('/api/me', { limit: 10, empty: '', none: undefined })
    expect(res).toEqual({ id: 1 })
    expect(f.mock.calls[0][0]).toBe('/api/me?limit=10')
    expect(f.mock.calls[0][1].headers.Authorization).toBe('Bearer tok')
  })

  it('maps error codes to Chinese text and reports expired sessions', async () => {
    vi.stubGlobal('fetch', mockFetch(401, { error: { code: 'UNAUTHORIZED', message: 'token expired' } }))
    setToken('tok')
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    const err = (await http.get("/api/me").catch((e: unknown) => e)) as ApiError
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('UNAUTHORIZED')
    expect(err.text).toBe('请先登录')
    expect(onUnauthorized).toHaveBeenCalled()
  })

  it('keeps the backend message for unknown codes', async () => {
    vi.stubGlobal('fetch', mockFetch(400, { error: { code: 'SOMETHING_NEW', message: 'raw message' } }))
    const err = (await http.post("/api/x", {}).catch((e: unknown) => e)) as ApiError
    expect(err.text).toBe('raw message')
  })

  it('OAuth poll 仅发送空对象与上游路径，传递 AbortSignal，不发送会话或代理字段', async () => {
    const fetchMock = mockFetch(200, { data: { status: 'pending', interval: 5 } })
    vi.stubGlobal('fetch', fetchMock)
    const controller = new AbortController()
    await expect(seller.oauthPoll(7, controller.signal)).resolves.toEqual({ status: 'pending', interval: 5 })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/seller/upstreams/7/oauth/poll')
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'POST', body: '{}', signal: controller.signal })
  })

  it('complete 保留旧 code 调用，并将智谱回调原文放入 callback_url', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ data: { id: 7 } }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ data: { id: 7 } }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    await seller.completeOAuth(7, 'code#state')
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ code: 'code#state' })
    const callback = 'zcode://oauth/callback?authCode=abc%2B123&state=xyz'
    await seller.completeOAuth(7, { callback_url: callback })
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ callback_url: callback })
  })

  it('取消请求保留 AbortError，不误报为网络错误', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((_path, options) => new Promise((_resolve, reject) => {
      options.signal.addEventListener('abort', () => reject(new DOMException('cancelled', 'AbortError')), { once: true })
    })))
    const controller = new AbortController()
    const pending = seller.oauthPoll(7, controller.signal)
    controller.abort()
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  })
})
