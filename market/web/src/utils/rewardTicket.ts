// loxo 任务中心票据：落地链接里的 ?reward_ticket=、本地留存、注册 / 登录后绑定、浏览任务 SDK。
// 口径见 docs/2026-11-06-loxo-reward-tasks.md「票据流转」。
// 与推广渠道（utils/promo.ts）一样，每一步失败都只静默放弃，绝不能影响页面渲染、注册或登录。
import { http } from '@/api/client'

/** loxo 票据：rt_ 开头，总长不超过 64（与后端校验一致） */
export const REWARD_TICKET_PATTERN = /^rt_[A-Za-z0-9_-]{8,61}$/
/** 本地留存的票据最多保留 30 天（loxo 回调类票据有效期即 30 天，服务端仍会再校验） */
export const REWARD_TTL_MS = 30 * 24 * 60 * 60 * 1000
/** 等待绑定的票据：{ ticket, touched_at } */
export const REWARD_STORAGE_KEY = 'tm_reward_ticket'
/** loxo SDK 自己读取的会话键（见 reward-task-sdk.js） */
export const SDK_TICKET_KEY = 'reward_task_ticket'
/** 本标签页需要重新加载 SDK 时用的 { script_url, app_key } */
export const SDK_INFO_KEY = 'tm_reward_sdk'
export const REWARD_LANDING_PATH = '/api/market/reward/landing'
export const REWARD_BIND_PATH = '/api/me/reward-ticket'

export interface RewardRecord {
  ticket: string
  touched_at: string
}

export interface RewardSdkInfo {
  script_url: string
  app_key: string
}

export interface RewardLanding {
  valid: boolean | null
  task_type: string
  sdk: RewardSdkInfo | null
}

function storage(kind: 'local' | 'session'): Storage | null {
  try {
    return kind === 'local' ? window.localStorage : window.sessionStorage
  } catch {
    return null
  }
}

function read(kind: 'local' | 'session', key: string): string | null {
  try {
    return storage(kind)?.getItem(key) ?? null
  } catch {
    return null
  }
}

function write(kind: 'local' | 'session', key: string, value: string): void {
  try {
    storage(kind)?.setItem(key, value)
  } catch {
    // 无痕模式 / 配额满：放弃留存
  }
}

function remove(kind: 'local' | 'session', key: string): void {
  try {
    storage(kind)?.removeItem(key)
  } catch {
    // 存储不可用时无事可做
  }
}

export function normalizeRewardTicket(raw: string | null | undefined): string | null {
  if (typeof raw !== 'string') return null
  const ticket = raw.trim()
  return REWARD_TICKET_PATTERN.test(ticket) ? ticket : null
}

/** 读等待绑定的票据；坏数据或超过 30 天返回 null */
export function readRewardTicket(now: number = Date.now()): RewardRecord | null {
  const raw = read('local', REWARD_STORAGE_KEY)
  if (!raw) return null
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return null
    const { ticket, touched_at: touchedAt } = parsed as { ticket?: unknown; touched_at?: unknown }
    if (typeof ticket !== 'string' || typeof touchedAt !== 'string') return null
    if (!REWARD_TICKET_PATTERN.test(ticket)) return null
    const at = Date.parse(touchedAt)
    if (!Number.isFinite(at) || now - at > REWARD_TTL_MS) return null
    return { ticket, touched_at: touchedAt }
  } catch {
    return null
  }
}

export function clearRewardTicket(): void {
  remove('local', REWARD_STORAGE_KEY)
}

/** 只在本地票据还是 ticket 时才清，避免异步回调清掉用户随后带来的新票据 */
function clearRewardTicketIf(ticket: string): void {
  if (readRewardTicket()?.ticket === ticket) clearRewardTicket()
}

/** 注册请求要带的票据字段；没有可用票据时为 {} */
export function rewardSignupFields(): { reward_ticket?: string } {
  const record = readRewardTicket()
  return record ? { reward_ticket: record.ticket } : {}
}

function stripTicketQuery(params: URLSearchParams): void {
  try {
    const rest = new URLSearchParams(params.toString())
    rest.delete('reward_ticket')
    const query = rest.toString()
    const state = window.history.state ?? null
    const { pathname, hash } = window.location
    window.history.replaceState(state, '', `${pathname}${query ? `?${query}` : ''}${hash}`)
  } catch {
    // 受限环境禁用 replaceState：票据留在地址栏也不影响绑定
  }
}

/**
 * 启动时同步调用一次（必须在路由读取地址栏之前）：读 ?reward_ticket=，合法则留存并从地址栏去掉。
 * 返回本次落地的票据；没有带或非法时返回 null。
 */
export function captureRewardTicketFromUrl(): string | null {
  try {
    const params = new URLSearchParams(window.location.search)
    const raw = params.get('reward_ticket')
    if (raw === null) return null
    stripTicketQuery(params)
    const ticket = normalizeRewardTicket(raw)
    if (!ticket) return null
    const record: RewardRecord = { ticket, touched_at: new Date().toISOString() }
    write('local', REWARD_STORAGE_KEY, JSON.stringify(record))
    return ticket
  } catch {
    return null
  }
}

function validSdkInfo(v: unknown): v is RewardSdkInfo {
  if (!v || typeof v !== 'object') return false
  const { script_url: url, app_key: key } = v as { script_url?: unknown; app_key?: unknown }
  if (typeof url !== 'string' || typeof key !== 'string' || !key) return false
  try {
    const parsed = new URL(url)
    return (parsed.protocol === 'https:' || parsed.protocol === 'http:') && parsed.pathname.endsWith('/reward-task-sdk.js')
  } catch {
    return false
  }
}

/** 插入 loxo SDK 脚本；同一页面只插一次 */
export function injectRewardSdk(info: RewardSdkInfo): boolean {
  try {
    if (!validSdkInfo(info)) return false
    if ((window as unknown as { RewardTaskSDK?: unknown }).RewardTaskSDK) return true
    if (document.querySelector('script[data-tm-reward-sdk]')) return true
    const el = document.createElement('script')
    el.src = info.script_url
    el.async = true
    el.setAttribute('data-app-key', info.app_key)
    el.setAttribute('data-lang', 'zh')
    el.setAttribute('data-tm-reward-sdk', '1')
    document.head.appendChild(el)
    return true
  } catch {
    return false
  }
}

/** 同一标签页刷新后：SDK 未完成（会话里还有它的票据）时重新加载 */
export function restoreRewardSdk(): boolean {
  if (!read('session', SDK_TICKET_KEY)) return false
  const raw = read('session', SDK_INFO_KEY)
  if (!raw) return false
  try {
    const info: unknown = JSON.parse(raw)
    return validSdkInfo(info) ? injectRewardSdk(info) : false
  } catch {
    return false
  }
}

/** 问服务端票据属于哪类任务；浏览任务就地启动 SDK，无效票据丢弃，其余保留等待绑定 */
export async function resolveRewardLanding(ticket: string): Promise<RewardLanding | null> {
  try {
    const resp = await fetch(REWARD_LANDING_PATH, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ ticket }),
    })
    if (!resp.ok) return null
    const body = (await resp.json()) as { data?: RewardLanding }
    const landing = body?.data
    if (!landing || typeof landing !== 'object') return null
    if (landing.valid === false) {
      clearRewardTicketIf(ticket)
      return landing
    }
    if (landing.valid === true && landing.task_type === 'browse') {
      // 浏览任务不需要绑定账号
      clearRewardTicketIf(ticket)
      if (landing.sdk && validSdkInfo(landing.sdk)) {
        write('session', SDK_TICKET_KEY, ticket)
        write('session', SDK_INFO_KEY, JSON.stringify(landing.sdk))
        injectRewardSdk(landing.sdk)
      }
    }
    return landing
  } catch {
    return null
  }
}

let binding: Promise<void> | null = null

/** 已登录时把等待中的票据绑定到当前账号；成功或被拒（4xx）都清掉本地票据，网络错误留着下次再试 */
export function bindPendingRewardTicket(): Promise<void> {
  if (binding) return binding
  const record = readRewardTicket()
  if (!record) {
    clearRewardTicket()
    return Promise.resolve()
  }
  binding = (async () => {
    try {
      await http.post<{ accepted: boolean }>(REWARD_BIND_PATH, { ticket: record.ticket })
      clearRewardTicketIf(record.ticket)
    } catch (err) {
      const status = (err as { status?: number })?.status ?? 0
      if (status >= 400 && status < 500 && status !== 401 && status !== 429) clearRewardTicketIf(record.ticket)
    } finally {
      binding = null
    }
  })()
  return binding
}
