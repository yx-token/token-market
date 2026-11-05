import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createPromoChannel,
  deletePromoChannel,
  getPromoChannelDaily,
  getPromoChannels,
  getPromoChannelUsers,
  MarketApiError,
  marketGet,
  setPromoChannelStatus,
  updatePromoChannel,
} from '../api'

const zeroStats = {
  pv: 0,
  uv: 0,
  signups: 0,
  paying_users: 0,
  recharge_amount: '0.00',
  buyer_spend: '0.00',
  seller_users: 0,
}

const channel = {
  id: 17,
  code: 'v2ex-post',
  name: 'V2EX 帖子',
  landing_path: '/market?category=all',
  note: '推广测试',
  status: 'active',
  created_at: '2026-11-05T08:00:00Z',
  updated_at: '2026-11-05T08:00:00Z',
}

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) =>
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }),
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('promo channel API', () => {
  beforeEach(() => localStorage.setItem('auth_token', 'admin-token'))
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('requests the canonical list route with inclusive date query parameters', async () => {
    const fetchMock = respond({ data: { items: [], from: '2026-10-07', to: '2026-11-05' } })
    const result = await getPromoChannels({ from: '2026-10-07', to: '2026-11-05' })

    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/admin/market/promo-channels?from=2026-10-07&to=2026-11-05')
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe('GET')
    expect(fetchMock.mock.calls[0]?.[1]?.headers).toEqual({ Authorization: 'Bearer admin-token' })
    expect(result.from).toBe('2026-10-07')
    expect(result.to).toBe('2026-11-05')
  })

  it.each([{ data: null }, { data: { items: null } }, { items: null }])(
    'normalizes null lists and absent summary stats for %j',
    async (payload) => {
      const fetchMock = respond(payload)
      const result = await getPromoChannels()

      expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/admin/market/promo-channels')
      expect(result).toEqual({
        from: null,
        to: null,
        items: [],
        totals: zeroStats,
        organic: {
          signups: 0,
          paying_users: 0,
          recharge_amount: '0.00',
          buyer_spend: '0.00',
          seller_users: 0,
        },
      })
    },
  )

  it('keeps string amounts and fills missing metrics without losing channel configuration', async () => {
    respond({
      data: {
        items: [{ ...channel, stats: { uv: 8, recharge_amount: '128.50', buyer_spend: '16.08' } }],
        totals: { uv: 8, recharge_amount: '128.50' },
        organic: { signups: 3, buyer_spend: '82.00' },
      },
    })

    const result = await getPromoChannels()
    expect(result.items).toEqual([{
      ...channel,
      stats: { ...zeroStats, uv: 8, recharge_amount: '128.50', buyer_spend: '16.08' },
    }])
    expect(result.totals).toEqual({ ...zeroStats, uv: 8, recharge_amount: '128.50' })
    expect(result.organic).toMatchObject({ signups: 3, paying_users: 0, buyer_spend: '82.00' })
  })

  it.each([
    { id: 17 },
    { id: 17, name: null, code: null, landing_path: null, note: null, status: null, created_at: null, updated_at: null },
    { id: 17, status: 'unknown' },
  ])('fills absent channel text and treats unknown status as disabled: %j', async (item) => {
    respond({ data: { items: [{ ...item, custom_field: 'keep-me' }] } })

    expect((await getPromoChannels()).items).toEqual([{
      id: 17,
      name: '',
      code: '',
      landing_path: '/',
      note: '',
      status: 'disabled',
      created_at: '',
      updated_at: '',
      stats: zeroStats,
      custom_field: 'keep-me',
    }])
  })

  it('creates a channel and allows the server to generate an omitted code', async () => {
    const fetchMock = respond({ data: channel }, 201)
    const input = { name: 'V2EX 帖子', landing_path: '/market?category=all', note: '推广测试' }
    const result = await createPromoChannel(input)

    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/admin/market/promo-channels')
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe('POST')
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual(input)
    expect(result).toEqual({ ...channel, stats: zeroStats })
  })

  it('updates editable fields with PUT and changes status through its dedicated action', async () => {
    const input = { name: '更新渠道', landing_path: '/', note: '' }
    const fetchMock = respond({ data: { ...channel, ...input, stats: null } })
    expect(await updatePromoChannel(17, input)).toEqual({ ...channel, ...input, stats: zeroStats })
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/admin/market/promo-channels/17')
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe('PUT')
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual(input)

    await setPromoChannelStatus(17, 'disabled')
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/v1/admin/market/promo-channels/17/status')
    expect(fetchMock.mock.calls[1]?.[1]?.method).toBe('POST')
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({ status: 'disabled' })
  })

  it('deletes a channel through DELETE and ignores an empty successful response', async () => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(deletePromoChannel(17)).resolves.toBeUndefined()
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/admin/market/promo-channels/17')
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe('DELETE')
    expect(fetchMock.mock.calls[0]?.[1]?.body).toBeUndefined()
  })

  it('loads daily stats with date filters, fills missing metrics and tolerates null items', async () => {
    const fetchMock = respond({ data: { items: [{ date: '2026-11-05', uv: 2, recharge_amount: '12.50' }] } })
    const range = { from: '2026-11-01', to: '2026-11-05' }
    expect(await getPromoChannelDaily(17, range)).toEqual({
      items: [{ ...zeroStats, date: '2026-11-05', uv: 2, recharge_amount: '12.50' }],
    })
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/admin/market/promo-channels/17/daily?from=2026-11-01&to=2026-11-05')

    respond({ data: { items: null } })
    expect(await getPromoChannelDaily(17)).toEqual({ items: [] })
  })

  it('loads attributed users with pagination, string amounts and a safe empty-list response', async () => {
    const user = {
      user_id: 20,
      email: 'buyer@example.com',
      registered_at: '2026-11-05T08:00:00Z',
      recharge_amount: '128.50',
      buyer_spend: '99.00',
      is_seller: true,
    }
    const fetchMock = respond({ data: { items: [user], total: 42 } })

    expect(await getPromoChannelUsers(17, 2, 10)).toEqual({ items: [user], total: 42 })
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/admin/market/promo-channels/17/users?page=2&page_size=10')

    const emptyFetch = respond({ data: { items: null } })
    expect(await getPromoChannelUsers(17)).toEqual({ items: [], total: 0 })
    expect(emptyFetch.mock.calls[0]?.[0]).toBe('/api/v1/admin/market/promo-channels/17/users?page=1&page_size=20')
  })

  it.each([
    { user_id: 20 },
    { user_id: 20, email: null, registered_at: null, recharge_amount: null, buyer_spend: null, is_seller: null },
  ])('normalizes absent attributed-user fields while preserving other fields: %j', async (item) => {
    respond({ data: { items: [{ ...item, custom_field: 'keep-me' }], total: 1 } })

    expect(await getPromoChannelUsers(17)).toEqual({
      items: [{
        user_id: 20,
        email: '',
        registered_at: '',
        recharge_amount: '0.00',
        buyer_spend: '0.00',
        is_seller: false,
        custom_field: 'keep-me',
      }],
      total: 1,
    })
  })

  it('passes request cancellation through the new GET functions', async () => {
    const fetchMock = respond({ data: { items: [] } })
    const abortController = new AbortController()
    const opts = { signal: abortController.signal }
    await getPromoChannels({}, opts)
    await getPromoChannelDaily(17, {}, opts)
    await getPromoChannelUsers(17, 1, 20, opts)

    const signals = fetchMock.mock.calls.map(([, init]) => init?.signal)
    expect(signals.every((signal) => signal instanceof AbortSignal)).toBe(true)
  })

  it.each([
    { error: { code: 'PROMO_CODE_TAKEN', message: '渠道码已存在' } },
    { code: 'PROMO_CODE_TAKEN', message: '渠道码已存在' },
  ])('keeps the code, status and server message for conflicting channel codes: %j', async (payload) => {
    respond(payload, 409)
    const result = createPromoChannel({ name: '重复渠道', code: 'v2ex-post' })

    await expect(result).rejects.toBeInstanceOf(MarketApiError)
    await expect(result).rejects.toMatchObject({
      code: 'PROMO_CODE_TAKEN',
      status: 409,
      message: '渠道码已存在',
    })
  })

  it('preserves a cannot-delete conflict for the page to explain existing attribution data', async () => {
    respond({ error: { code: 'PROMO_CHANNEL_HAS_DATA', message: '渠道已有访问或注册数据' } }, 409)

    await expect(deletePromoChannel(17)).rejects.toMatchObject({
      code: 'PROMO_CHANNEL_HAS_DATA',
      status: 409,
      message: '渠道已有访问或注册数据',
    })
  })

  it('keeps existing market error fallback messages when the server omits error details', async () => {
    respond(null, 503)
    await expect(marketGet('promo-channels')).rejects.toMatchObject({ message: '请求失败 503', status: 503 })
  })
})
