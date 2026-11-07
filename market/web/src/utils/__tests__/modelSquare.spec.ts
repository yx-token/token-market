import { describe, expect, it } from 'vitest'
import { applyMult, buildModelSquare, capabilityOfModel, channelIdOf, familyOfModel } from '@/utils/modelSquare'
import type { MarketItem, Price } from '@/api/types'

const prices: Price[] = [
  { model: 'claude-sonnet-4-5', provider: 'anthropic', mode: 'chat', input_per_1m: 3, output_per_1m: 15, cache_write_per_1m: 3.75, cache_read_per_1m: 0.3 },
  { model: 'gpt-5.4', provider: 'openai', mode: 'chat', input_per_1m: 2, output_per_1m: 8, cache_write_per_1m: 0, cache_read_per_1m: 0 },
  { model: 'MiniMax-M2', provider: 'minimax', mode: 'chat', input_per_1m: 0.3, output_per_1m: 1.2, cache_write_per_1m: 0, cache_read_per_1m: 0 },
  { model: 'mistral-large-latest', provider: 'mistral', mode: 'chat', input_per_1m: 2, output_per_1m: 6, cache_write_per_1m: 0, cache_read_per_1m: 0 },
  { model: 'qwen-max', provider: 'dashscope', mode: 'chat', input_per_1m: 1, output_per_1m: 3, cache_write_per_1m: 0, cache_read_per_1m: 0 },
  { model: 'gpt-image-1.5', provider: 'openai', mode: 'image_generation', input_per_1m: 0, output_per_1m: 0, cache_write_per_1m: 0, cache_read_per_1m: 0 },
  { model: 'veo-3.1-generate-001', provider: 'gemini', mode: 'video', input_per_1m: 0, output_per_1m: 0, cache_write_per_1m: 0, cache_read_per_1m: 0 },
]

function listing(partial: Partial<MarketItem> & Pick<MarketItem, 'platform' | 'price'>): MarketItem {
  return {
    seller_id: 1,
    listing_id: 1,
    auth_type: 'oauth',
    health: 'healthy',
    success_rate_24h: null,
    requests_24h: 0,
    ttft_p50_ms: null,
    listed_since: new Date().toISOString(),
    ...partial,
  }
}

describe('modelSquare', () => {
  it('classifies model families and channels', () => {
    expect(familyOfModel('claude-opus-4-1')).toBe('claude')
    expect(familyOfModel('gpt-5.4')).toBe('openai')
    expect(familyOfModel('MiniMax-M2')).toBe('minimax')
    expect(familyOfModel('whatever', 'deepseek')).toBe('deepseek')
    expect(channelIdOf('mistral')).toBe('mistral')
    expect(channelIdOf('dashscope')).toBe('dashscope')
  })

  it('classifies capabilities', () => {
    expect(capabilityOfModel('claude-sonnet-4-5', 'chat')).toBe('text')
    expect(capabilityOfModel('gpt-image-1.5', 'image_generation')).toBe('image')
    expect(capabilityOfModel('veo-3.1-generate-001', 'video')).toBe('video')
  })

  it('groups by global channels and picks lowest multiplier', () => {
    const groups = buildModelSquare(
      prices,
      [
        listing({ platform: 'anthropic', price: 0.8, models: ['claude-sonnet-4-5'] }),
        listing({ platform: 'anthropic', price: 0.55, models: ['claude-sonnet-4-5'] }),
        listing({ platform: 'anthropic', price: 0.2, models: ['claude-sonnet-4-5'], sku_kind: 'pack' }),
        listing({ platform: 'openai', price: 0.7, models: undefined }),
      ],
      [],
    )
    expect(groups.length).toBeGreaterThanOrEqual(5)
    const claude = groups.find((g) => g.id === 'anthropic')
    const openai = groups.find((g) => g.id === 'openai')
    const mistral = groups.find((g) => g.id === 'mistral')
    expect(claude?.rows[0]?.bestMult).toBe(0.55)
    expect(openai?.rows.find((r) => r.model === 'gpt-5.4')?.bestMult).toBe(0.7)
    expect(mistral?.title).toContain('Mistral')
    expect(applyMult(3, 0.55)).toBeCloseTo(1.65)
    expect(groups.reduce((n, g) => n + g.rows.length, 0)).toBe(7)
  })
})
