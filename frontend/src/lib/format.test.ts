import { describe, expect, it } from 'vitest'
import { formatAmount } from './format'

describe('formatAmount', () => {
  it('strips IEEE-754 noise like 0.30000000000000004 → 0.3', () => {
    expect(formatAmount(0.1 + 0.2)).toBe('0.3')
  })

  it('renders venue lot-step precision (2 dp) cleanly', () => {
    expect(formatAmount(0.1)).toBe('0.1')
    expect(formatAmount(0.2)).toBe('0.2')
    expect(formatAmount(0.01)).toBe('0.01')
    expect(formatAmount(1.5)).toBe('1.5')
  })

  it('rounds beyond 2 dp', () => {
    expect(formatAmount(0.12345678)).toBe('0.12')
    expect(formatAmount(0.005)).toBe('0.01')
  })

  it('renders integers without trailing zeros', () => {
    expect(formatAmount(5)).toBe('5')
    expect(formatAmount(0)).toBe('0')
  })

  it('renders negative values', () => {
    expect(formatAmount(-0.1 + -0.2)).toBe('-0.3')
  })

  it('renders a dash for non-finite values', () => {
    expect(formatAmount(Number.NaN)).toBe('—')
    expect(formatAmount(Number.POSITIVE_INFINITY)).toBe('—')
  })
})
