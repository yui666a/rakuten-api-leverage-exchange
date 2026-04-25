import { describe, expect, it } from 'vitest'
import { formatRiskEvent, formatTradeEvent } from './notify-format'

describe('formatTradeEvent', () => {
  it('formats an open BUY into the expected JP title', () => {
    const desc = formatTradeEvent({
      kind: 'open',
      symbolId: 10,
      side: 'BUY',
      amount: 0.2,
      price: 9023.8,
      orderId: 100,
      reason: 'trend follow',
      timestamp: 1,
    })
    expect(desc.title).toBe('エントリー 買い 0.2')
    expect(desc.body).toContain('¥9,024')
    expect(desc.body).toContain('trend follow')
    expect(desc.tag).toBe('trade-100')
    expect(desc.beep).toBe('info')
  })

  it('uses success beep for close events', () => {
    const desc = formatTradeEvent({
      kind: 'close',
      symbolId: 10,
      side: 'SELL',
      amount: 0.2,
      price: 9100,
      orderId: 200,
      timestamp: 1,
    })
    expect(desc.title).toBe('クローズ 売り 0.2')
    expect(desc.beep).toBe('success')
    // body has no reason, so it should just be the price.
    expect(desc.body).toBe('¥9,100')
  })
})

describe('formatRiskEvent', () => {
  it('maps dd_critical to critical beep + JP title', () => {
    const desc = formatRiskEvent({
      kind: 'dd_critical',
      severity: 'critical',
      message: 'MaxDD critical: 19.0%',
      timestamp: 60_000,
    })
    expect(desc.title).toContain('🚨')
    expect(desc.beep).toBe('critical')
  })

  it('maps cooldown_started to info beep', () => {
    const desc = formatRiskEvent({
      kind: 'cooldown_started',
      severity: 'info',
      message: 'cooldown started',
      timestamp: 60_000,
    })
    expect(desc.beep).toBe('info')
  })

  it('quantises tag by minute so duplicate events within a window dedupe', () => {
    const a = formatRiskEvent({
      kind: 'dd_warning',
      severity: 'warning',
      message: 'm1',
      timestamp: 60_000 * 5,
    })
    const b = formatRiskEvent({
      kind: 'dd_warning',
      severity: 'warning',
      message: 'm1 again',
      timestamp: 60_000 * 5 + 30_000,
    })
    expect(a.tag).toBe(b.tag)
  })
})
