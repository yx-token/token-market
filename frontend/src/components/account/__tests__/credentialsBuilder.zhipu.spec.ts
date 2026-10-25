import { describe, expect, it } from 'vitest'
import { buildZhipuOAuthCredentials, defaultZhipuOAuthBaseUrl } from '../credentialsBuilder'
import { zhipuResult } from '@/__tests__/fixtures/zhipuOAuth'

describe('GLM OAuth credentials', () => {
  it('keeps every backend field including derived keys and future fields', () => {
    expect(buildZhipuOAuthCredentials(zhipuResult)).toEqual({ ...zhipuResult.credentials, account_mode: 'coding' })
  })
  it('replaces secrets while preserving existing routing, models and error settings', () => {
    const existing = {
      access_token: 'old-access', zcode_jwt: 'old-jwt', api_key: 'old.key',
      base_url: 'https://relay.example/anthropic', api_protocol: 'adaptive',
      api_base_urls: { chat_completions: 'https://relay.example/v4', anthropic: 'https://relay.example/anthropic' },
      model_mapping: { public: 'glm-5' }, custom_error_codes: [429], custom_setting: true
    }
    const input = { ...zhipuResult, credentials: { ...zhipuResult.credentials, base_url: 'https://server.example', api_protocol: 'anthropic', model_mapping: {} } }
    const actual = buildZhipuOAuthCredentials(input, existing)
    expect(actual).toEqual({
      ...zhipuResult.credentials, account_mode: 'coding',
      base_url: existing.base_url, api_protocol: existing.api_protocol,
      api_base_urls: existing.api_base_urls, model_mapping: existing.model_mapping,
      custom_error_codes: existing.custom_error_codes
    })
    expect(actual).not.toHaveProperty('custom_setting')
    expect(existing.access_token).toBe('old-access')
    expect(zhipuResult.credentials).not.toHaveProperty('account_mode')
  })
  it('clears omitted lifecycle values instead of retaining a future old TTL or refresh token', () => {
    const future = '2099-01-01T00:00:00Z'
    const existing = {
      refresh_token: 'old-refresh', zai_oauth_access_token: 'old-oauth', _token_version: 123,
      business_expires_at: future, zcode_jwt_expires_at: future, oauth_expires_at: future,
      business_expiry_unknown: false, user_info: { username: 'old-user' },
      temp_unschedulable_enabled: true, temp_unschedulable_rules: [{ status: 429 }],
      intercept_warmup_requests: true
    }
    const credentials = buildZhipuOAuthCredentials({
      provider: 'zai', credentials: { api_key: 'new.secret', access_token: 'new-business', zai_oauth_access_token: 'new-oauth' }, display: {}
    }, existing)
    expect(credentials).toMatchObject({
      refresh_token: null, business_expires_at: null, zcode_jwt_expires_at: null,
      oauth_expires_at: null, business_expiry_unknown: true, zai_oauth_access_token: 'new-oauth',
      temp_unschedulable_enabled: true, temp_unschedulable_rules: existing.temp_unschedulable_rules,
      intercept_warmup_requests: true
    })
    expect(credentials).not.toHaveProperty('_token_version')
    expect(credentials).not.toHaveProperty('user_info')
    expect(JSON.stringify(credentials)).not.toContain('old-refresh')
    expect(existing.refresh_token).toBe('old-refresh')
  })
  it.each(['bigmodel', 'zai'] as const)('uses official endpoints for provider %s and each protocol', provider => {
    const host = provider === 'zai' ? 'https://api.z.ai' : 'https://open.bigmodel.cn'
    expect(defaultZhipuOAuthBaseUrl(provider, 'chat_completions')).toBe(`${host}/api/coding/paas/v4`)
    expect(defaultZhipuOAuthBaseUrl(provider, 'adaptive')).toBe(`${host}/api/coding/paas/v4`)
    expect(defaultZhipuOAuthBaseUrl(provider, 'anthropic')).toBe(`${host}/api/anthropic`)
  })
})
