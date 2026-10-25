import { apiClient } from '../client'
import type { Account } from '@/types'

export interface KimiTokenInfo {
  access_token: string
  refresh_token: string
  token_type: string
  expires_in: number
  expires_at: number | string
  scope: string
  device_id: string
  base_url: string
  client_id: string
}

export interface KimiDeviceCodeResponse {
  session_id: string
  user_code: string
  verification_uri: string
  verification_uri_complete: string
  expires_in: number
  interval: number
}

export interface KimiPollResponse {
  status: 'pending' | 'slow_down' | 'success' | 'expired' | 'denied' | 'failed'
  interval?: number
  message?: string
  token_info?: KimiTokenInfo
}

export async function generateDeviceCode(
  payload: { proxy_id?: number } = {},
  signal?: AbortSignal
): Promise<KimiDeviceCodeResponse> {
  const { data } = await apiClient.post<KimiDeviceCodeResponse>('/admin/kimi/oauth/device-code', payload, { signal })
  return data
}

export async function pollDeviceCode(
  payload: { session_id: string; proxy_id?: number },
  signal?: AbortSignal
): Promise<KimiPollResponse> {
  const { data } = await apiClient.post<KimiPollResponse>('/admin/kimi/oauth/poll', payload, { signal })
  return data
}

export async function refreshKimiToken(payload: {
  refresh_token: string
  proxy_id?: number
  device_id?: string
}): Promise<KimiTokenInfo> {
  const { data } = await apiClient.post<KimiTokenInfo>('/admin/kimi/oauth/refresh-token', payload)
  return data
}

export async function refreshAccount(id: number): Promise<Account> {
  const { data } = await apiClient.post<Account | { account: Account }>(`/admin/kimi/accounts/${id}/refresh`)
  return 'account' in data ? data.account : data
}

export default { generateDeviceCode, pollDeviceCode, refreshKimiToken, refreshAccount }
