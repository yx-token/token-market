import type { KimiDeviceCodeResponse, KimiTokenInfo } from '@/api/admin/kimi'

export const kimiToken: KimiTokenInfo = {
  access_token: 'kimi-access',
  refresh_token: 'kimi-refresh',
  token_type: 'Bearer',
  expires_in: 3600,
  expires_at: 1_900_000_000,
  scope: 'coding',
  device_id: 'kimi-device',
  base_url: 'https://api.kimi.com/coding/v1',
  client_id: 'kimi-client'
}

export const kimiDeviceCode: KimiDeviceCodeResponse = {
  session_id: 'kimi-session',
  user_code: 'ABCD-EFGH',
  verification_uri: 'https://auth.kimi.com/device',
  verification_uri_complete: 'https://auth.kimi.com/device?user_code=ABCD-EFGH',
  expires_in: 60,
  interval: 5
}
