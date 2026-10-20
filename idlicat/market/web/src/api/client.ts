// 控制面接口的请求封装：成功响应 {"data": ...}，失败响应 {"error": {"code", "message"}}。
import { errorText } from './errors'

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
  /** 给用户看的中文提示 */
  get text(): string {
    return errorText(this.code, this.message)
  }
}

let token: string | null = localStorage.getItem('tm_token')
let onUnauthorized: (() => void) | null = null

export function setToken(t: string | null) {
  token = t
  if (t) localStorage.setItem('tm_token', t)
  else localStorage.removeItem('tm_token')
}

export function getToken() {
  return token
}

export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

export type Query = Record<string, string | number | boolean | undefined | null>

function withQuery(path: string, q?: Query): string {
  if (!q) return path
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(q)) {
    if (v !== undefined && v !== null && v !== '') params.set(k, String(v))
  }
  const s = params.toString()
  return s ? `${path}?${s}` : path
}

export async function request<T>(method: string, path: string, body?: unknown, q?: Query, timeoutMs?: number): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (token) headers.Authorization = `Bearer ${token}`
  let resp: Response
  try {
    resp = await fetch(withQuery(path, q), { method, headers, body: body === undefined ? undefined : JSON.stringify(body),
      ...(timeoutMs ? { signal: AbortSignal.timeout(timeoutMs) } : {}) })
  } catch {
    throw new ApiError(0, 'NETWORK_ERROR', 'network error')
  }
  const text = await resp.text()
  let parsed: { data?: T; error?: { code: string; message: string } } = {}
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      parsed = {}
    }
  }
  if (!resp.ok) {
    const err = parsed.error ?? { code: `HTTP_${resp.status}`, message: text || resp.statusText }
    if (resp.status === 401 && token && onUnauthorized) onUnauthorized()
    throw new ApiError(resp.status, err.code, err.message)
  }
  return parsed.data as T
}

export const http = {
  get: <T>(path: string, q?: Query, timeoutMs?: number) => request<T>('GET', path, undefined, q, timeoutMs),
  post: <T>(path: string, body?: unknown, timeoutMs?: number) => request<T>('POST', path, body ?? {}, undefined, timeoutMs),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
  del: <T>(path: string) => request<T>('DELETE', path),
}
