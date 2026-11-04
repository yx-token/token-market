import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getEngineProxySharing, marketGet, setEngineProxySharing, testEngineProxy } from '../api'

function respond(body: unknown, status = 200) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })),
  )
}

describe('marketGet', () => {
  beforeEach(() => localStorage.setItem('auth_token', 'admin-token'))
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('returns null when the market API reports an empty list as data: null', async () => {
    respond({ data: null })
    const rows = (await marketGet<unknown[] | null>('/api/v1/admin/market/proxies')) ?? []
    expect(rows).toEqual([])
  })

  it('unwraps data arrays and plain payloads', async () => {
    respond({ data: [{ id: 1 }] })
    expect(await marketGet('/api/v1/admin/market/proxies')).toEqual([{ id: 1 }])
    respond({ code: 0, data: { ok: true } })
    expect(await marketGet('/api/v1/admin/market/status')).toEqual({ ok: true })
    respond({ ok: true })
    expect(await marketGet('/api/v1/admin/market/status')).toEqual({ ok: true })
  })

  it('reads and writes engine proxy sharing through the market relay contract', async () => {
    const response = { proxy_id: 17, is_shared: true, connection_report: null, connection_checked_at: null }
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) =>
      new Response(JSON.stringify({ data: [response] }), { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)

    expect(await getEngineProxySharing()).toEqual([response])
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/admin/market/engine-proxies/sharing')

    fetchMock.mockImplementationOnce(async () =>
      new Response(JSON.stringify({ data: response }), { status: 200 }),
    )
    await setEngineProxySharing(17, true)
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/v1/admin/market/engine-proxies/17/sharing')
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({ is_shared: true })

    fetchMock.mockImplementationOnce(async () =>
      new Response(JSON.stringify({ data: response }), { status: 200 }),
    )
    await testEngineProxy(17)
    expect(fetchMock.mock.calls[2]?.[0]).toBe('/api/v1/admin/market/engine-proxies/17/test')
    expect(fetchMock.mock.calls[2]?.[1]?.method).toBe('POST')
  })
})
