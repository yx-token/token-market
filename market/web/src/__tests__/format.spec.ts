import { describe, expect, it } from 'vitest'
import { money, multiplier, parseAmount, percent, tokens } from '@/utils/format'
import { label } from '@/utils/labels'

describe('format', () => {
  it('formats money with more precision for tiny amounts', () => {
    expect(money(12.5)).toBe('¥12.50')
    expect(money(0.0042)).toBe('¥0.0042')
    expect(money(null)).toBe('—')
  })
  it('formats multipliers, percents and tokens', () => {
    expect(multiplier(0.6000000001)).toBe('0.6×')
    expect(percent(0.987)).toBe('98.7%')
    expect(tokens(1500)).toBe('1.5k')
    expect(tokens(2_500_000)).toBe('2.50M')
  })
  it('parses amounts with at most two decimals', () => {
    expect(parseAmount('100')).toBe(100)
    expect(parseAmount('9.99')).toBe(9.99)
    expect(parseAmount('1.234')).toBeNull()
    expect(parseAmount('0')).toBeNull()
    expect(parseAmount('abc')).toBeNull()
  })
  it('labels unknown values with the raw value', () => {
    expect(label('health', 'degraded').text).toBe('降智')
    expect(label('health', 'weird').text).toBe('weird')
  })
})
