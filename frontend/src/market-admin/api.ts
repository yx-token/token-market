/** 管理端市场 API：规范前缀 `/api/v1/admin/market/*`（见 docs/dev/admin-market-api-canonical.md）。 */

export const MARKET_API_PREFIX = '/api/v1/admin/market'

export interface MarketRequestOptions {
  signal?: AbortSignal
  timeoutMs?: number
}

export class MarketApiError extends Error {
  constructor(message: string, public readonly status: number, public readonly code?: string) {
    super(message)
    this.name = 'MarketApiError'
  }
}

export interface EngineProxySharing {
  proxy_id: number
  is_shared: boolean
  connection_report: {
    success: boolean
    message?: string
    region?: string
    ip?: string
    latency_ms?: number
  } | null
  connection_checked_at?: string | null
}

function sessionToken(): string {
  return (
    localStorage.getItem('engine_auth_token') ||
    localStorage.getItem('tm_token') ||
    localStorage.getItem('auth_token') ||
    ''
  )
}

export function marketToken(): string {
  return sessionToken()
}

/** 接受相对后缀或完整规范路径；`/api/admin/*` 仅作兼容 shim。 */
function resolvePath(path: string): string {
  if (path.startsWith(MARKET_API_PREFIX)) return path
  if (path.startsWith('/api/admin/')) {
    const rest = path.slice('/api/admin/'.length)
    if (rest === 'sub2api/status') return `${MARKET_API_PREFIX}/status`
    return `${MARKET_API_PREFIX}/${rest}`
  }
  if (path.startsWith('/')) return path
  return `${MARKET_API_PREFIX}/${path}`
}

function mergeAbortSignal(signal: AbortSignal | undefined, timeoutMs: number | undefined): {
  signal?: AbortSignal
  cancel: () => void
} {
  if ((!timeoutMs || timeoutMs <= 0) && !signal) return { cancel: () => undefined }
  const ac = new AbortController()
  const onAbort = () => ac.abort()
  if (signal) {
    if (signal.aborted) ac.abort()
    else signal.addEventListener('abort', onAbort, { once: true })
  }
  let timer: ReturnType<typeof setTimeout> | undefined
  if (timeoutMs && timeoutMs > 0) {
    timer = setTimeout(() => ac.abort(), timeoutMs)
  }
  return {
    signal: ac.signal,
    cancel: () => {
      if (timer) clearTimeout(timer)
      if (signal) signal.removeEventListener('abort', onAbort)
    },
  }
}

export async function marketSend<T = unknown>(
  method: string,
  path: string,
  body?: unknown,
  opts?: MarketRequestOptions,
): Promise<T> {
  const token = sessionToken()
  if (!token) throw new Error('登录已失效，请重新进入控制台')
  const url = resolvePath(path)
  const merged = mergeAbortSignal(opts?.signal, opts?.timeoutMs)
  try {
    const res = await fetch(url, {
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: merged.signal,
    })
    const payload = await res.json().catch(() => ({}))
    if (!res.ok) {
      const message = payload?.error?.message || payload?.message || `请求失败 ${res.status}`
      const code = typeof payload?.error?.code === 'string'
        ? payload.error.code
        : typeof payload?.code === 'string' ? payload.code : undefined
      throw new MarketApiError(message, res.status, code)
    }
    // 有 data 字段就以它为准（含 null）：Go 空切片序列化为 null，回退到整个 payload 会让调用方的 `?? []` 失效。
    const hasData = payload !== null && typeof payload === 'object' && Object.prototype.hasOwnProperty.call(payload, 'data')
    return (hasData ? payload.data : payload) as T
  } finally {
    merged.cancel()
  }
}

export async function marketGet<T = unknown>(
  path: string,
  opts?: MarketRequestOptions,
): Promise<T> {
  return marketSend<T>('GET', path, undefined, opts)
}

export function getEngineProxySharing(): Promise<EngineProxySharing[]> {
  return marketGet<EngineProxySharing[]>(`${MARKET_API_PREFIX}/engine-proxies/sharing`)
}

export function setEngineProxySharing(
  proxyID: number,
  isShared: boolean,
): Promise<EngineProxySharing> {
  return marketSend<EngineProxySharing>(
    'PUT',
    `${MARKET_API_PREFIX}/engine-proxies/${proxyID}/sharing`,
    { is_shared: isShared },
  )
}

export function testEngineProxy(proxyID: number): Promise<EngineProxySharing> {
  return marketSend<EngineProxySharing>(
    'POST',
    `${MARKET_API_PREFIX}/engine-proxies/${proxyID}/test`,
  )
}

export type PromoChannelStatus = 'active' | 'disabled'

export interface PromoDateRange {
  from?: string
  to?: string
}

export interface PromoStats {
  pv: number
  uv: number
  signups: number
  paying_users: number
  recharge_amount: string
  buyer_spend: string
  seller_users: number
}

export type PromoOrganicStats = Omit<PromoStats, 'pv' | 'uv'>

export interface PromoChannel {
  id: number
  code: string
  name: string
  landing_path: string
  note: string
  status: PromoChannelStatus
  created_at: string
  updated_at: string
  stats: PromoStats
}

export interface PromoChannelList {
  from: string | null
  to: string | null
  items: PromoChannel[]
  totals: PromoStats
  organic: PromoOrganicStats
}

export interface CreatePromoChannelInput {
  name: string
  code?: string
  landing_path?: string
  note?: string
}

export interface UpdatePromoChannelInput {
  name: string
  landing_path: string
  note: string
}

export interface PromoChannelDaily extends PromoStats {
  date: string
}

export interface PromoChannelUser {
  user_id: number
  email: string
  registered_at: string
  recharge_amount: string
  buyer_spend: string
  is_seller: boolean
}

type PromoChannelResponse = {
  id: number
  status?: string | null
  stats?: Partial<PromoStats> | null
} & {
  [Key in 'name' | 'code' | 'landing_path' | 'note' | 'created_at' | 'updated_at']?: string | null
}

type PromoChannelUserResponse = { user_id: number } & {
  [Key in Exclude<keyof PromoChannelUser, 'user_id'>]?: PromoChannelUser[Key] | null
}

function normalizePromoOrganicStats(stats?: Partial<PromoOrganicStats> | null): PromoOrganicStats {
  return {
    signups: stats?.signups ?? 0,
    paying_users: stats?.paying_users ?? 0,
    recharge_amount: stats?.recharge_amount ?? '0.00',
    buyer_spend: stats?.buyer_spend ?? '0.00',
    seller_users: stats?.seller_users ?? 0,
  }
}

function normalizePromoStats(stats?: Partial<PromoStats> | null): PromoStats {
  return {
    pv: stats?.pv ?? 0,
    uv: stats?.uv ?? 0,
    ...normalizePromoOrganicStats(stats),
  }
}

function normalizePromoChannel(channel: PromoChannelResponse): PromoChannel {
  return {
    ...channel,
    name: channel.name ?? '',
    code: channel.code ?? '',
    landing_path: channel.landing_path ?? '/',
    note: channel.note ?? '',
    created_at: channel.created_at ?? '',
    updated_at: channel.updated_at ?? '',
    status: channel.status === 'active' ? 'active' : 'disabled',
    stats: normalizePromoStats(channel.stats),
  }
}

function promoRangeQuery(range: PromoDateRange): string {
  const query = new URLSearchParams()
  if (range.from) query.set('from', range.from)
  if (range.to) query.set('to', range.to)
  const value = query.toString()
  return value ? `?${value}` : ''
}

export async function getPromoChannels(
  range: PromoDateRange = {},
  opts?: MarketRequestOptions,
): Promise<PromoChannelList> {
  const result = await marketGet<{
    from?: string | null
    to?: string | null
    items?: PromoChannelResponse[] | null
    totals?: Partial<PromoStats> | null
    organic?: Partial<PromoOrganicStats> | null
  } | null>(`promo-channels${promoRangeQuery(range)}`, opts)
  return {
    from: result?.from ?? null,
    to: result?.to ?? null,
    items: (result?.items ?? []).map(normalizePromoChannel),
    totals: normalizePromoStats(result?.totals),
    organic: normalizePromoOrganicStats(result?.organic),
  }
}

export async function createPromoChannel(input: CreatePromoChannelInput): Promise<PromoChannel> {
  return normalizePromoChannel(await marketSend<PromoChannelResponse>('POST', 'promo-channels', input))
}

export async function updatePromoChannel(id: number, input: UpdatePromoChannelInput): Promise<PromoChannel> {
  return normalizePromoChannel(await marketSend<PromoChannelResponse>('PUT', `promo-channels/${id}`, input))
}

export async function setPromoChannelStatus(id: number, status: PromoChannelStatus): Promise<PromoChannel> {
  return normalizePromoChannel(await marketSend<PromoChannelResponse>('POST', `promo-channels/${id}/status`, { status }))
}

export async function deletePromoChannel(id: number): Promise<void> {
  await marketSend('DELETE', `promo-channels/${id}`)
}

export async function getPromoChannelDaily(
  id: number,
  range: PromoDateRange = {},
  opts?: MarketRequestOptions,
): Promise<{ items: PromoChannelDaily[] }> {
  const result = await marketGet<{
    items?: (Partial<PromoStats> & { date: string })[] | null
  } | null>(`promo-channels/${id}/daily${promoRangeQuery(range)}`, opts)
  return {
    items: (result?.items ?? []).map((item) => ({ ...item, ...normalizePromoStats(item) })),
  }
}

export async function getPromoChannelUsers(
  id: number,
  page = 1,
  pageSize = 20,
  opts?: MarketRequestOptions,
): Promise<{ items: PromoChannelUser[]; total: number }> {
  const query = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  const result = await marketGet<{
    items?: PromoChannelUserResponse[] | null
    total?: number
  } | null>(`promo-channels/${id}/users?${query}`, opts)
  return {
    items: (result?.items ?? []).map((user) => ({
      ...user,
      email: user.email ?? '',
      registered_at: user.registered_at ?? '',
      recharge_amount: user.recharge_amount ?? '0.00',
      buyer_spend: user.buyer_spend ?? '0.00',
      is_seller: user.is_seller ?? false,
    })),
    total: result?.total ?? 0,
  }
}

// ---- loxo 任务中心对接（market/docs/2026-11-06-loxo-reward-tasks.md） ----

export type RewardTicketState = 'pending' | 'active' | 'completed' | 'skipped' | 'invalid'
export type RewardEventState = 'pending' | 'accepted' | 'completed' | 'ignored' | 'rejected' | 'failed'

export const REWARD_TICKET_STATES: RewardTicketState[] = ['pending', 'active', 'completed', 'skipped', 'invalid']
export const REWARD_EVENT_STATES: RewardEventState[] = ['pending', 'accepted', 'completed', 'ignored', 'rejected', 'failed']

export interface RewardTaskConfig {
  enabled: boolean
  base_url: string
  app_key: string
  secret_set: boolean
  sdk_enabled: boolean
  updated_at: string | null
  summary: {
    tickets: Record<RewardTicketState, number>
    events: Record<RewardEventState, number>
  }
}

export interface SaveRewardTaskConfigInput {
  enabled: boolean
  base_url: string
  app_key: string
  /** 空串表示不修改已保存的 secret */
  app_secret: string
  sdk_enabled: boolean
}

export interface RewardPingResult {
  partner_name: string
  app_key: string
  server_time: number
  clock_skew_seconds: number
}

export interface RewardTicket {
  id: number
  ticket: string
  user_id: number
  user_email: string
  new_user: boolean
  state: RewardTicketState
  reason: string
  task_id: number | null
  task_title: string
  task_type: string
  period_key: string
  expires_at: string | null
  attempts: number
  created_at: string
  updated_at: string
}

export interface RewardEvent {
  id: number
  ticket_id: number
  ticket: string
  user_id: number
  user_email: string
  task_title: string
  event_id: string
  event_type: 'register' | 'recharge'
  recharge_order_id: number | null
  amount: string | null
  state: RewardEventState
  reason: string
  rewarded: boolean
  attempts: number
  next_attempt_at: string
  sent_at: string | null
  created_at: string
}

function countMap<K extends string>(keys: K[], raw: unknown): Record<K, number> {
  const src = raw && typeof raw === 'object' ? (raw as Record<string, unknown>) : {}
  const out = {} as Record<K, number>
  for (const k of keys) {
    const n = Number(src[k])
    out[k] = Number.isFinite(n) ? n : 0
  }
  return out
}

function normalizeRewardConfig(raw: Partial<RewardTaskConfig> | null | undefined): RewardTaskConfig {
  return {
    enabled: raw?.enabled === true,
    base_url: raw?.base_url || 'https://loxo.cc',
    app_key: raw?.app_key || '',
    secret_set: raw?.secret_set === true,
    sdk_enabled: raw?.sdk_enabled !== false,
    updated_at: raw?.updated_at ?? null,
    summary: {
      tickets: countMap(REWARD_TICKET_STATES, raw?.summary?.tickets),
      events: countMap(REWARD_EVENT_STATES, raw?.summary?.events),
    },
  }
}

export async function getRewardTaskConfig(opts?: MarketRequestOptions): Promise<RewardTaskConfig> {
  return normalizeRewardConfig(await marketGet<Partial<RewardTaskConfig> | null>('reward-tasks/config', opts))
}

export async function saveRewardTaskConfig(input: SaveRewardTaskConfigInput): Promise<RewardTaskConfig> {
  return normalizeRewardConfig(await marketSend<Partial<RewardTaskConfig> | null>('PUT', 'reward-tasks/config', input))
}

export function pingRewardTasks(): Promise<RewardPingResult> {
  return marketSend<RewardPingResult>('POST', 'reward-tasks/ping', {}, { timeoutMs: 20_000 })
}

function rewardListQuery(state: string, page: number, pageSize: number): string {
  const q = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (state) q.set('state', state)
  return q.toString()
}

export async function getRewardTickets(state = '', page = 1, pageSize = 20, opts?: MarketRequestOptions): Promise<{ items: RewardTicket[]; total: number }> {
  const r = await marketGet<{ items?: RewardTicket[] | null; total?: number } | null>(`reward-tasks/tickets?${rewardListQuery(state, page, pageSize)}`, opts)
  return { items: r?.items ?? [], total: Number(r?.total) || 0 }
}

export async function getRewardEvents(state = '', page = 1, pageSize = 20, opts?: MarketRequestOptions): Promise<{ items: RewardEvent[]; total: number }> {
  const r = await marketGet<{ items?: RewardEvent[] | null; total?: number } | null>(`reward-tasks/events?${rewardListQuery(state, page, pageSize)}`, opts)
  return { items: r?.items ?? [], total: Number(r?.total) || 0 }
}

export function retryRewardEvent(id: number): Promise<RewardEvent> {
  return marketSend<RewardEvent>('POST', `reward-tasks/events/${id}/retry`)
}
