import { describe, expect, it } from 'vitest'
import {
  buildCcSwitchImportDeeplink,
  CC_SWITCH_USAGE_SCRIPT,
  normalizeCcSwitchPlatform,
  resolveCcSwitchImportConfig,
} from '../ccswitchImport'

interface UsageResult {
  isValid: boolean
  remaining?: number
  unit?: string
}

interface UsageScript {
  request: { url: string; method: string; headers: Record<string, string> }
  extractor: (response: unknown) => UsageResult
}

function loadUsageScript(baseUrl = 'https://market.example.com'): UsageScript {
  const source = CC_SWITCH_USAGE_SCRIPT
    .replace('{{baseUrl}}', baseUrl)
    .replace('{{apiKey}}', 'tm-buyer')
  return new Function(`return ${source}`)() as UsageScript
}

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
    expect(atob(q.get('usageScript')!)).toBe(CC_SWITCH_USAGE_SCRIPT)
    expect(q.get('usageAutoInterval')).toBe('30')
  })

  it.each([
    'https://market.example.com',
    'https://market.example.com/',
    'https://market.example.com/v1',
    'https://market.example.com/v1/',
    'https://market.example.com/antigravity',
    'https://market.example.com/antigravity/',
    'https://market.example.com/antigravity/v1',
    'https://market.example.com/antigravity/v1/',
  ])('usage script requests the local buyer balance from %s', (baseUrl) => {
    expect(loadUsageScript(baseUrl).request).toEqual({
      url: 'https://market.example.com/v1/usage',
      method: 'GET',
      headers: { Authorization: 'Bearer tm-buyer' },
    })
  })

  it('usage script reads the local CNY balance and active state', () => {
    const { extractor } = loadUsageScript()
    expect(extractor({ remaining: 12.5, unit: 'CNY', is_active: true })).toEqual({
      isValid: true,
      remaining: 12.5,
      unit: 'CNY',
    })
    expect(extractor({ remaining: 0, unit: 'CNY', is_active: false })).toEqual({
      isValid: false,
      remaining: 0,
      unit: 'CNY',
    })
    expect(extractor({ remaining: 4 })).toEqual({ isValid: true, remaining: 4, unit: 'CNY' })
  })

  it('usage script supports native balance and currency fields', () => {
    const { extractor } = loadUsageScript()
    expect(extractor({ balance: 3.25, currency: 'CNY', isValid: true })).toEqual({
      isValid: true,
      remaining: 3.25,
      unit: 'CNY',
    })
    expect(extractor({ quota: { remaining: 2.75, unit: 'CNY' } })).toEqual({
      isValid: true,
      remaining: 2.75,
      unit: 'CNY',
    })
  })

  it.each([
    { error: { type: 'api_error', message: 'usage balance unavailable' } },
    { error: 'unavailable', remaining: 500, is_active: true },
    {},
    null,
    { remaining: null },
    { remaining: '' },
    { remaining: false },
    { remaining: Number.NaN },
    { remaining: Number.POSITIVE_INFINITY },
    { remaining: Number.NEGATIVE_INFINITY },
    { remaining: 'not a balance' },
  ])('usage script rejects errors or invalid balances without inventing zero: %j', (response) => {
    const result = loadUsageScript().extractor(response)
    expect(result.isValid).toBe(false)
    expect(result.remaining).toBeUndefined()
  })

  it('antigravity deeplink embeds the same local usage script', () => {
    const link = buildCcSwitchImportDeeplink({
      baseUrl: 'https://market.example.com',
      platform: 'antigravity',
      clientType: 'gemini',
      providerName: 'Antigravity',
      apiKey: 'tm-buyer',
    })
    const q = new URL(link.replace('ccswitch://v1/import', 'https://x')).searchParams
    expect(q.get('endpoint')).toBe('https://market.example.com/antigravity')
    expect(atob(q.get('usageScript')!)).toBe(CC_SWITCH_USAGE_SCRIPT)
    expect(loadUsageScript(q.get('endpoint')!).request.url).toBe('https://market.example.com/v1/usage')
  })
})
