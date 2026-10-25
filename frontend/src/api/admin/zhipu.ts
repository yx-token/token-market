import { apiClient } from '../client'
import type { Account } from '@/types'

export type ZhipuOAuthProvider = 'bigmodel' | 'zai'
export interface ZhipuOAuthResult {
  provider: ZhipuOAuthProvider
  credentials: Record<string, unknown>
  display: {
    email?: string
    username?: string
    organization_name?: string
    project_name?: string
    expires_at?: number | string
  }
}
export interface ZhipuOAuthSession {
  session_id: string
  provider: ZhipuOAuthProvider
  authorize_url: string
  expires_in: number
  expires_at: number | string
  interval: number
}
export interface ZhipuPollResponse {
  status: 'pending' | 'success' | 'expired' | 'failed'
  interval?: number
  message?: string
  result?: ZhipuOAuthResult
}
export async function initOAuth(
  payload: { provider: ZhipuOAuthProvider; proxy_id?: number },
  signal?: AbortSignal
): Promise<ZhipuOAuthSession> {
  const { data } = await apiClient.post<ZhipuOAuthSession>('/admin/zhipu/oauth/init', payload, { signal })
  return data
}
export async function pollOAuth(
  payload: { session_id: string; proxy_id?: number },
  signal?: AbortSignal
): Promise<ZhipuPollResponse> {
  const { data } = await apiClient.post<ZhipuPollResponse>('/admin/zhipu/oauth/poll', payload, { signal })
  return data
}
export async function exchangeCode(
  payload: { session_id: string; callback_url?: string; code?: string; state?: string; proxy_id?: number },
  signal?: AbortSignal
): Promise<ZhipuOAuthResult> {
  const { data } = await apiClient.post<ZhipuOAuthResult>('/admin/zhipu/oauth/exchange-code', payload, { signal })
  return data
}
export async function refreshAccount(id: number): Promise<Account> {
  const { data } = await apiClient.post<Account | { account: Account }>(`/admin/zhipu/accounts/${id}/refresh`)
  return 'account' in data ? data.account : data
}
export default { initOAuth, pollOAuth, exchangeCode, refreshAccount }
