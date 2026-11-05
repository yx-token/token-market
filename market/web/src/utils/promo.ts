// 推广渠道（promo）采集：落地链接里的 ?ch=、本地留存、注册时带上归因字段。
// 口径见 docs/2026-11-05-promo-channels.md「C 端采集（market/web）」。
// 这里每一步失败都只静默放弃：采集问题绝不能让页面渲染或注册失败。
// 注意与 sub2api 的上游「渠道」、recharge_orders.channel（支付方式）无关，一律用 promo 前缀。

/** 渠道码：统一小写，2~32 位 [a-z0-9_-]（与后端校验一致） */
export const PROMO_CODE_PATTERN = /^[a-z0-9_-]{2,32}$/
/** 最后一次点击的有效期：30 天 */
export const PROMO_TTL_MS = 30 * 24 * 60 * 60 * 1000
/** 本地留存：{ code, touched_at } */
export const PROMO_STORAGE_KEY = 'tm_promo'
/** 访客标识：8~64 位 [A-Za-z0-9_-] */
export const VISITOR_STORAGE_KEY = 'tm_visitor_id'
export const PROMO_VISIT_PATH = '/api/market/promo/visit'

const VISITOR_ID_PATTERN = /^[A-Za-z0-9_-]{8,64}$/
// 长度 64，能整除 256，取模不引入偏斜
const VISITOR_ALPHABET = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-'
const FALLBACK_VISITOR_LENGTH = 32

export interface PromoRecord {
  code: string
  touched_at: string
}

export interface PromoSignupFields {
  channel_code: string
  visitor_id: string
  channel_touched_at: string
}

/** 注册时额外带的归因字段；没有可用记录时为 {} */
export type PromoSignupExtra = Partial<PromoSignupFields>

export interface PromoVisitBody {
  code: string
  visitor_id: string
  landing_path: string
  referrer: string
}

// 无痕模式、禁用 Cookie 的浏览器读 localStorage 会直接抛错
function readStorage(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

function writeStorage(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // 配额满或被禁用：放弃本地留存，不影响当次流程
  }
}

function removeStorage(key: string): void {
  try {
    window.localStorage.removeItem(key)
  } catch {
    // 存储不可用时无事可做，直接跳过
    return
  }
}

/** `?ch=` 归一化：小写 + 校验；非法返回 null */
export function normalizePromoCode(raw: string | null | undefined): string | null {
  if (typeof raw !== 'string') return null
  const code = raw.trim().toLowerCase()
  return PROMO_CODE_PATTERN.test(code) ? code : null
}

function randomChars(length: number): string {
  let out = ''
  try {
    const cryptoObj = globalThis.crypto
    if (cryptoObj && typeof cryptoObj.getRandomValues === 'function') {
      const bytes = new Uint8Array(length)
      cryptoObj.getRandomValues(bytes)
      for (const b of bytes) out += VISITOR_ALPHABET[b % VISITOR_ALPHABET.length]
      return out
    }
  } catch {
    // 落到 Math.random 兜底
  }
  out = ''
  for (let i = 0; i < length; i += 1) out += VISITOR_ALPHABET[Math.floor(Math.random() * VISITOR_ALPHABET.length)]
  return out
}

function newVisitorId(): string {
  try {
    const cryptoObj = globalThis.crypto
    if (cryptoObj && typeof cryptoObj.randomUUID === 'function') {
      const id = cryptoObj.randomUUID()
      if (VISITOR_ID_PATTERN.test(id)) return id
    }
  } catch {
    // 非安全上下文（http）/老浏览器没有 randomUUID
  }
  return randomChars(FALLBACK_VISITOR_LENGTH)
}

/** 取（必要时生成）本浏览器的访客标识；即使写入失败也返回可用值 */
export function getVisitorId(): string {
  const existing = readStorage(VISITOR_STORAGE_KEY)
  if (existing && VISITOR_ID_PATTERN.test(existing)) return existing
  const id = newVisitorId()
  writeStorage(VISITOR_STORAGE_KEY, id)
  return id
}

/** 读本地归因记录；坏数据/被篡改返回 null */
export function readPromo(): PromoRecord | null {
  const raw = readStorage(PROMO_STORAGE_KEY)
  if (!raw) return null
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return null
    const { code, touched_at: touchedAt } = parsed as { code?: unknown; touched_at?: unknown }
    if (typeof code !== 'string' || typeof touchedAt !== 'string') return null
    if (!PROMO_CODE_PATTERN.test(code)) return null
    if (!Number.isFinite(Date.parse(touchedAt))) return null
    return { code, touched_at: touchedAt }
  } catch {
    return null
  }
}

/** 去掉地址栏的 ch，保留其余查询参数与 hash（避免用户复制/刷新又重复上报） */
function stripPromoQuery(params: URLSearchParams): void {
  try {
    const rest = new URLSearchParams(params.toString())
    rest.delete('ch')
    const query = rest.toString()
    // 保留 history.state（vue-router 在里面存滚动位置），只换 URL
    const state = window.history.state ?? null
    const { pathname, hash } = window.location
    window.history.replaceState(state, '', `${pathname}${query ? `?${query}` : ''}${hash}`)
  } catch {
    // 某些受限环境禁用 replaceState：链接留着也不影响归因
  }
}

function reportPromoVisit(body: PromoVisitBody): void {
  try {
    const pending = fetch(PROMO_VISIT_PATH, {
      method: 'POST',
      keepalive: true,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    // fire-and-forget：离线、被广告拦截、5xx 都不能冒泡到调用方
    if (pending && typeof pending.catch === 'function') pending.catch(() => {})
  } catch {
    // fetch 同步抛错（或环境没有 fetch）同样忽略
  }
}

/**
 * 启动时调用一次：读 location.search 的 ch，合法则留存并上报一次访问。
 * 返回留存的记录，没有带（或非法）时返回 null。
 */
export function captureFromUrl(): PromoRecord | null {
  try {
    const params = new URLSearchParams(window.location.search)
    const raw = params.get('ch')
    if (raw === null) return null
    const code = normalizePromoCode(raw)
    if (!code) return null
    const visitorId = getVisitorId()
    const record: PromoRecord = { code, touched_at: new Date().toISOString() }
    // 最后一次点击覆盖旧值
    writeStorage(PROMO_STORAGE_KEY, JSON.stringify(record))
    const body: PromoVisitBody = {
      code,
      visitor_id: visitorId,
      landing_path: window.location.pathname,
      referrer: document.referrer || '',
    }
    reportPromoVisit(body)
    stripPromoQuery(params)
    return record
  } catch {
    return null
  }
}

/** 注册请求要带的归因字段；没有记录、坏数据或超过 30 天返回 {} */
export function promoSignupFields(now: number = Date.now()): PromoSignupExtra {
  const record = readPromo()
  if (!record) return {}
  const touchedAt = Date.parse(record.touched_at)
  if (!Number.isFinite(touchedAt)) return {}
  if (now - touchedAt > PROMO_TTL_MS) return {}
  return { channel_code: record.code, visitor_id: getVisitorId(), channel_touched_at: record.touched_at }
}

/** 注册成功后只清归因记录，保留 tm_visitor_id（同浏览器后续访问仍算同一访客） */
export function clearPromo(): void {
  removeStorage(PROMO_STORAGE_KEY)
}
