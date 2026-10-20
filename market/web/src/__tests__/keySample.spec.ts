import { describe, expect, it } from 'vitest'
import { sampleRequest } from '@/utils/keySample'

const base = 'https://edge.example.com'
const key = 'tm-abcdefghijklmnop'

describe('sampleRequest', () => {
  it('gives an Anthropic-style example for Claude groups', () => {
    const s = sampleRequest('anthropic', base, key)
    expect(s.code).toContain(`${base}/v1/messages`)
    expect(s.code).toContain(`x-api-key: ${key}`)
    expect(s.code).toContain('"model":"claude-sonnet-4-5"')
    expect(s.code).not.toContain('gpt-')
  })

  it('gives an OpenAI-style example for GPT groups', () => {
    const s = sampleRequest('openai', base, key)
    expect(s.code).toContain(`${base}/v1/chat/completions`)
    expect(s.code).toContain(`Authorization: Bearer ${key}`)
    expect(s.code).toContain('"model":"gpt-5.4"')
    expect(s.code).not.toContain('claude-')
  })

  it('gives a Gemini example for Gemini groups', () => {
    const s = sampleRequest('gemini', base, key)
    expect(s.code).toContain(`${base}/v1beta/models/gemini-2.5-pro:generateContent`)
    expect(s.code).toContain(`x-goog-api-key: ${key}`)
  })

  it('prefers a model of the same platform from the market price list', () => {
    const models = ['claude-opus-4-1', 'gpt-5.1', 'gemini-2.5-pro']
    expect(sampleRequest('openai', base, key, models).code).toContain('"model":"gpt-5.1"')
    expect(sampleRequest('anthropic', base, key, models).code).toContain('"model":"claude-opus-4-1"')
    expect(sampleRequest('gemini', base, key, models).code).toContain('models/gemini-2.5-pro:generateContent')
  })

  it('keeps the Anthropic example for the composite group', () => {
    expect(sampleRequest('composite', base, key).code).toContain('/v1/messages')
  })

  it('falls back to the Anthropic example for unknown platforms', () => {
    expect(sampleRequest('', base, key).code).toContain('/v1/messages')
  })
})
