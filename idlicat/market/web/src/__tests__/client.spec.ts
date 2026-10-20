import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, http, setToken, setUnauthorizedHandler } from '@/api/client'

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
})
