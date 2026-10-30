/** 管理端市场 API：规范前缀 `/api/v1/admin/market/*`（见 docs/dev/admin-market-api-canonical.md）。 */

export const MARKET_API_PREFIX = '/api/v1/admin/market'

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
  opts?: { signal?: AbortSignal; timeoutMs?: number },
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
    if (!res.ok) throw new Error(payload?.error?.message || payload?.message || `请求失败 ${res.status}`)
    return (payload?.data ?? payload) as T
  } finally {
    merged.cancel()
  }
}

export async function marketGet<T = unknown>(
  path: string,
  opts?: { signal?: AbortSignal; timeoutMs?: number },
): Promise<T> {
  return marketSend<T>('GET', path, undefined, opts)
}
