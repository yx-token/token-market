import { describe, expect, it } from 'vitest'
import {
  buildCcSwitchImportDeeplink,
  normalizeCcSwitchPlatform,
  resolveCcSwitchImportConfig,
} from '../ccswitchImport'

describe('ccswitchImport', () => {
  it('normalizeCcSwitchPlatform maps display names', () => {
    expect(normalizeCcSwitchPlatform('Claude')).toBe('anthropic')
    expect(normalizeCcSwitchPlatform('ChatGPT')).toBe('openai')
    expect(normalizeCcSwitchPlatform('openai')).toBe('openai')
    expect(normalizeCcSwitchPlatform('antigravity')).toBe('antigravity')
  })

  it('openai config keeps endpoint without /v1', () => {
    const c = resolveCcSwitchImportConfig('openai', 'claude', 'https://edge.example.com')
    expect(c.app).toBe('codex')
    expect(c.endpoint).toBe('https://edge.example.com')
    expect(c.model).toBeTruthy()
  })

  it('buildCcSwitchImportDeeplink encodes provider', () => {
    const link = buildCcSwitchImportDeeplink({
      baseUrl: 'https://edge.example.com',
      platform: 'anthropic',
      clientType: 'claude',
      providerName: '闲猫',
      apiKey: 'sk-test',
    })
    expect(link.startsWith('ccswitch://v1/import?')).toBe(true)
    const q = new URL(link.replace('ccswitch://v1/import', 'https://x')).searchParams
    expect(q.get('app')).toBe('claude')
    expect(q.get('apiKey')).toBe('sk-test')
    expect(q.get('name')).toBe('闲猫')
    expect(q.get('usageEnabled')).toBe('true')
  })
})
