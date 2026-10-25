import type { ZhipuOAuthResult, ZhipuOAuthSession } from '@/api/admin/zhipu'
export const zhipuSession: ZhipuOAuthSession = {
  session_id: 'glm-session', provider: 'bigmodel', authorize_url: 'https://bigmodel.cn/login?state=glm-state',
  expires_in: 300, expires_at: 1800000300, interval: 2
}
export const zhipuResult: ZhipuOAuthResult = {
  provider: 'bigmodel', credentials: {
    api_key: 'derived-id.derived-secret', access_token: 'business-access', zcode_jwt: 'zcode-jwt',
    refresh_token: null, zai_oauth_access_token: null,
    business_expires_at: null, oauth_expires_at: null, business_expiry_unknown: false,
    zcode_jwt_expires_at: 1800001000, oauth_provider: 'bigmodel', client_version: '3.11.2',
    zhipu_organization: 'default-org', zhipu_project: 'default-project',
    future_server_field: { preserved: true }
  }, display: { username: 'GLM user', organization_name: 'Default organization' }
}
export const zhipuCallback = 'zcode://oauth/callback?authCode=one-time-code&state=glm-state'
