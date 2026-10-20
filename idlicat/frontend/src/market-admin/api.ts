/** 闲猫增量接口：走 sub2api 命名空间 /api/v1/admin/market/*，用当前管理员会话。 */

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

function resolvePath(path: string): string {
  if (path.startsWith('/api/v1/admin/market')) return path
  if (path.startsWith('/api/admin/')) {
    const rest = path.slice('/api/admin/'.length)
    if (rest === 'sub2api/status') return `${MARKET_API_PREFIX}/status`
    return `${MARKET_API_PREFIX}/${rest}`
  }
  if (path.startsWith('/')) return path
  return `${MARKET_API_PREFIX}/${path}`
}

export async function marketSend<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  const token = sessionToken()
  if (!token) throw new Error('登录已失效，请重新进入控制台')
  const url = resolvePath(path)
  const res = await fetch(url, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const payload = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(payload?.error?.message || payload?.message || `请求失败 ${res.status}`)
  return (payload?.data ?? payload) as T
}

export async function marketGet<T = unknown>(path: string): Promise<T> {
  return marketSend<T>('GET', path)
}
