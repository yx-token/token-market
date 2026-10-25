import { describe, expect, it } from 'vitest'
import { buildKimiOAuthCredentials } from '../credentialsBuilder'
import { kimiToken } from '@/__tests__/fixtures/kimiOAuth'

describe('Kimi OAuth credentials', () => {
  it.each([1_900_000_000, '1900000000', '2030-03-17T17:46:40Z'])('preserves expires_at %s as Grok does', expires_at => {
    expect(buildKimiOAuthCredentials({ ...kimiToken, expires_at })).toEqual({
      access_token: 'kimi-access', refresh_token: 'kimi-refresh', token_type: 'Bearer',
      expires_at, scope: 'coding', device_id: 'kimi-device', base_url: kimiToken.base_url, account_mode: 'coding'
    })
  })

  it.each([undefined, 'https://api.kimi.com/coding/v1', 'https://api.kimi.com/coding/v1/'])('uses the Anthropic base on creation and reauthorization (%s)', base_url => {
    expect(buildKimiOAuthCredentials(kimiToken, { api_protocol: 'anthropic', base_url }).base_url).toBe('https://api.kimi.com/coding')
  })

  it('merges new credentials while preserving protocol, mappings and a custom endpoint', () => {
    const existing = {
      access_token: 'old', refresh_token: 'old', api_key: 'legacy', account_mode: 'payg',
      api_protocol: 'anthropic', base_url: 'https://proxy.example/coding',
      model_mapping: { public: 'kimi-k2.5' }, api_base_urls: { anthropic: 'https://proxy.example/coding' }
    }
    expect(buildKimiOAuthCredentials(kimiToken, existing)).toMatchObject({
      api_protocol: 'anthropic', base_url: existing.base_url, model_mapping: existing.model_mapping,
      api_base_urls: existing.api_base_urls, access_token: 'kimi-access', refresh_token: 'kimi-refresh', account_mode: 'coding'
    })
    expect(buildKimiOAuthCredentials(kimiToken, existing)).not.toHaveProperty('api_key')
    expect(existing.access_token).toBe('old')
  })
})
