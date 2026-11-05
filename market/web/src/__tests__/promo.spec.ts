import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  captureFromUrl,
  clearPromo,
  getVisitorId,
  normalizePromoCode,
  promoSignupFields,
  readPromo,
  PROMO_STORAGE_KEY,
  PROMO_TTL_MS,
  VISITOR_STORAGE_KEY,
  type PromoVisitBody,
} from '@/utils/promo'

const DAY = 24 * 60 * 60 * 1000
const VISITOR_PATTERN = /^[A-Za-z0-9_-]{8,64}$/

/** 把地址栏换成指定 search（jsdom 支持同源 replaceState） */
function setUrl(search: string, hash = '') {
  window.history.replaceState(null, '', `/${search}${hash}`)
}

function storedPromo(): { code: string; touched_at: string } | null {
  const raw = localStorage.getItem(PROMO_STORAGE_KEY)
  return raw ? JSON.parse(raw) : null
}

describe('推广渠道采集', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  function visitCall() {
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    return { url, init, body: JSON.parse(String(init.body)) as PromoVisitBody }
  }

  beforeEach(() => {
    localStorage.clear()
    setUrl('')
    // jsdom 的 document.referrer 是只读的，造一个本实例上的可覆盖属性
    Object.defineProperty(document, 'referrer', { configurable: true, get: () => 'https://www.v2ex.com/t/123456' })
    fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, text: async () => '{"accepted":true}' })
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    Reflect.deleteProperty(document, 'referrer')
    localStorage.clear()
    setUrl('')
  })

  it('合法的 ch：留存归因记录并上报一次访问', () => {
    setUrl('?ch=v2ex-post')
    const before = Date.now()
    const record = captureFromUrl()

    expect(record?.code).toBe('v2ex-post')
    expect(record?.touched_at).toBeTruthy()
    expect(Date.parse(String(record?.touched_at))).toBeGreaterThanOrEqual(before - 1000)

    const saved = storedPromo()
    expect(saved?.code).toBe('v2ex-post')

    const { url, init, body } = visitCall()
    expect(url).toBe('/api/market/promo/visit')
    expect(init.method).toBe('POST')
    expect(init.keepalive).toBe(true)
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect(body.code).toBe('v2ex-post')
    expect(body.visitor_id).toMatch(VISITOR_PATTERN)
    expect(body.landing_path).toBe('/')
    expect(body.referrer).toBe('https://www.v2ex.com/t/123456')
    // 访客标识与本地留存一致
    expect(localStorage.getItem(VISITOR_STORAGE_KEY)).toBe(body.visitor_id)
    // 只有 ch 一个参数时，地址栏回到干净的 /
    expect(window.location.search).toBe('')
  })

  it('大小写归一：大写/混合大小写都按小写存', () => {
    setUrl('?ch=V2EX-Post')
    expect(captureFromUrl()?.code).toBe('v2ex-post')
    expect(storedPromo()?.code).toBe('v2ex-post')
    expect(visitCall().body.code).toBe('v2ex-post')

    localStorage.clear()
    fetchMock.mockClear()
    setUrl('?ch=Up-Main')
    expect(captureFromUrl()?.code).toBe('up-main')
  })

  it('非法的 ch 一律忽略：不留存、不上报、不改地址', () => {
    for (const bad of ['?ch=a', '?ch=has space', '?ch=中文', '?ch=' + 'x'.repeat(33), '?ch=x!y']) {
      localStorage.clear()
      fetchMock.mockClear()
      setUrl(bad)
      expect(captureFromUrl()).toBeNull()
      expect(localStorage.getItem(PROMO_STORAGE_KEY)).toBeNull()
      expect(fetchMock).not.toHaveBeenCalled()
      expect(decodeURIComponent(window.location.search)).toBe(bad)
    }
  })

  it('没有 ch 时什么都不做', () => {
    setUrl('?utm_source=wechat')
    expect(captureFromUrl()).toBeNull()
    expect(localStorage.getItem(PROMO_STORAGE_KEY)).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(window.location.search).toBe('?utm_source=wechat')
  })

  it('覆盖旧记录（最后一次点击生效）', () => {
    localStorage.setItem(PROMO_STORAGE_KEY, JSON.stringify({ code: 'old-code', touched_at: '2020-01-01T00:00:00.000Z' }))
    setUrl('?ch=new-code')
    captureFromUrl()
    const saved = storedPromo()
    expect(saved?.code).toBe('new-code')
    expect(Date.parse(String(saved?.touched_at))).toBeGreaterThan(Date.parse('2020-01-01T00:00:00.000Z'))
    expect(promoSignupFields().channel_code).toBe('new-code')
  })

  it('从地址栏去掉 ch，保留其他查询参数与 hash', () => {
    setUrl('?ch=v2ex-post&utm_source=wechat&page=2', '#list')
    captureFromUrl()
    expect(window.location.pathname).toBe('/')
    expect(window.location.search).toBe('?utm_source=wechat&page=2')
    expect(window.location.hash).toBe('#list')
  })

  it('去掉 ch 之后再执行不会重复上报', () => {
    setUrl('?ch=v2ex-post')
    captureFromUrl()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(captureFromUrl()).toBeNull()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('上报失败不抛错（Promise reject）', async () => {
    fetchMock.mockRejectedValue(new Error('network down'))
    setUrl('?ch=v2ex-post')
    expect(() => captureFromUrl()).not.toThrow()
    await Promise.resolve()
    // 归因记录已写入，失败只影响统计
    expect(storedPromo()?.code).toBe('v2ex-post')
  })

  it('上报失败不抛错（fetch 同步抛错）', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => {
        throw new Error('blocked by extension')
      }),
    )
    setUrl('?ch=v2ex-post')
    expect(() => captureFromUrl()).not.toThrow()
    expect(storedPromo()?.code).toBe('v2ex-post')
  })

  it('fetch 返回非 Promise 时也不崩', () => {
    vi.stubGlobal('fetch', vi.fn(() => undefined))
    setUrl('?ch=v2ex-post')
    expect(() => captureFromUrl()).not.toThrow()
  })

  it('localStorage 读写抛错时不崩，只是不落盘', () => {
    const denied = () => {
      throw new Error('denied')
    }
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(denied)
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(denied)
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(denied)

    setUrl('?ch=v2ex-post')
    expect(() => captureFromUrl()).not.toThrow()
    // 仍能上报（访客标识退化成内存里的临时值）
    expect(visitCall().body.visitor_id).toMatch(VISITOR_PATTERN)
    expect(getVisitorId()).toMatch(VISITOR_PATTERN)
    expect(readPromo()).toBeNull()
    expect(promoSignupFields()).toEqual({})
    expect(() => clearPromo()).not.toThrow()
  })

  it('visitor_id 生成一次后复用', () => {
    const first = getVisitorId()
    expect(first).toMatch(VISITOR_PATTERN)
    expect(getVisitorId()).toBe(first)
    expect(localStorage.getItem(VISITOR_STORAGE_KEY)).toBe(first)
  })

  it('没有 randomUUID 时用 [A-Za-z0-9_-] 兜底', () => {
    localStorage.clear()
    vi.stubGlobal('crypto', {
      getRandomValues: <T extends ArrayBufferView | null>(arr: T): T => {
        if (arr) new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength).fill(7)
        return arr
      },
    })
    const id = getVisitorId()
    expect(id).toMatch(VISITOR_PATTERN)
    expect(id.length).toBeGreaterThanOrEqual(8)
    expect(id.length).toBeLessThanOrEqual(64)

    // 连 getRandomValues 也没有时，Math.random 兜底
    localStorage.clear()
    vi.stubGlobal('crypto', undefined)
    expect(getVisitorId()).toMatch(VISITOR_PATTERN)
  })

  it('normalizePromoCode 只接受小写 2~32 位', () => {
    expect(normalizePromoCode('v2ex-post')).toBe('v2ex-post')
    expect(normalizePromoCode('  V2EX_2026  ')).toBe('v2ex_2026')
    expect(normalizePromoCode('ab')).toBe('ab')
    expect(normalizePromoCode('a-b_c9')).toBe('a-b_c9')
    expect(normalizePromoCode('a')).toBeNull()
    expect(normalizePromoCode('a'.repeat(33))).toBeNull()
    expect(normalizePromoCode('a b')).toBeNull()
    expect(normalizePromoCode('')).toBeNull()
    expect(normalizePromoCode(null)).toBeNull()
    expect(normalizePromoCode(undefined)).toBeNull()
  })
})

describe('注册时的归因字段', () => {
  beforeEach(() => {
    localStorage.clear()
    setUrl('')
  })

  afterEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
  })

  function savePromo(code: string, touchedAt: string) {
    localStorage.setItem(PROMO_STORAGE_KEY, JSON.stringify({ code, touched_at: touchedAt }))
    localStorage.setItem(VISITOR_STORAGE_KEY, 'visitor-abc123')
  }

  it('30 天内返回渠道字段', () => {
    const touchedAt = new Date(Date.now() - 29 * DAY).toISOString()
    savePromo('v2ex-post', touchedAt)
    expect(promoSignupFields()).toEqual({
      channel_code: 'v2ex-post',
      visitor_id: 'visitor-abc123',
      channel_touched_at: touchedAt,
    })
  })

  it('30 天边界仍算有效，超过 30 天返回 {}', () => {
    const touchedAt = new Date(Date.now() - PROMO_TTL_MS).toISOString()
    savePromo('v2ex-post', touchedAt)
    const touched = Date.parse(touchedAt)
    expect(promoSignupFields(touched + PROMO_TTL_MS).channel_code).toBe('v2ex-post')
    expect(promoSignupFields(touched + PROMO_TTL_MS + 1)).toEqual({})

    // 31 天前点过链接：一定过期
    savePromo('v2ex-post', new Date(Date.now() - 31 * DAY).toISOString())
    expect(promoSignupFields()).toEqual({})
  })

  it('没有记录返回 {}', () => {
    expect(promoSignupFields()).toEqual({})
    // 只有访客标识、没有归因记录时也不带渠道
    localStorage.setItem(VISITOR_STORAGE_KEY, 'visitor-abc123')
    expect(promoSignupFields()).toEqual({})
  })

  it('坏数据返回 {}，不抛错', () => {
    for (const bad of ['not json', '{}', '{"code":"v2ex-post"}', '{"touched_at":"2026-11-05T00:00:00Z"}', '[]', 'null', '"x"']) {
      localStorage.setItem(PROMO_STORAGE_KEY, bad)
      expect(promoSignupFields()).toEqual({})
    }
    // 时间不能解析
    localStorage.setItem(PROMO_STORAGE_KEY, JSON.stringify({ code: 'v2ex-post', touched_at: 'yesterday' }))
    expect(promoSignupFields()).toEqual({})
    // 渠道码被篡改成非法值
    localStorage.setItem(PROMO_STORAGE_KEY, JSON.stringify({ code: 'BAD CODE', touched_at: new Date().toISOString() }))
    expect(promoSignupFields()).toEqual({})
  })

  it('clearPromo 只删归因记录，保留 visitor_id', () => {
    savePromo('v2ex-post', new Date().toISOString())
    clearPromo()
    expect(localStorage.getItem(PROMO_STORAGE_KEY)).toBeNull()
    expect(localStorage.getItem(VISITOR_STORAGE_KEY)).toBe('visitor-abc123')
    expect(promoSignupFields()).toEqual({})
  })
})
