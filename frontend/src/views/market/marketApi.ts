export function marketSessionToken(): string {
  return localStorage.getItem('tm_token') || ''
}

export async function marketJson<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const token = marketSessionToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(path, { ...init, headers })
  const payload = await res.json().catch(() => ({}))
  if (!res.ok) {
    const message = payload?.error?.message || payload?.message || `请求失败 ${res.status}`
    throw new Error(message)
  }
  return (payload?.data ?? payload) as T
}
