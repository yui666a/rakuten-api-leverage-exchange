import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { DecisionDetailPanel } from '../DecisionDetailPanel'
import type { DecisionLogItem } from '../../lib/api'

function makeItem(overrides: Partial<DecisionLogItem> = {}): DecisionLogItem {
  return {
    id: 1,
    barCloseAt: 1_700_000_000_000,
    sequenceInBar: 0,
    triggerKind: 'BAR_CLOSE',
    symbolId: 3,
    currencyPair: 'LTC_JPY',
    primaryInterval: 'PT15M',
    stance: 'TREND_FOLLOW',
    lastPrice: 9000,
    signal: { action: 'HOLD', confidence: 0, reason: '' },
    risk: { outcome: 'SKIPPED', reason: '' },
    bookGate: { outcome: 'SKIPPED', reason: '' },
    order: { outcome: 'NOOP', orderId: 0, amount: 0, price: 0, error: '' },
    closedPositionId: 0,
    openedPositionId: 0,
    indicators: {},
    higherTfIndicators: {},
    createdAt: 1_700_000_000_000,
    ...overrides,
  }
}

describe('DecisionDetailPanel', () => {
  it('decision.reason を「判断理由」として表示する', () => {
    const item = makeItem({
      marketSignal: { direction: 'BEARISH', strength: 0.6 },
      decision: {
        intent: 'EXIT_CANDIDATE',
        side: 'SELL',
        reason: 'long held; bearish signal -> exit candidate',
      },
    })
    render(<DecisionDetailPanel item={item} />)
    expect(
      screen.getByText('long held; bearish signal -> exit candidate'),
    ).toBeInTheDocument()
  })

  it('decision.reason が空でも signal.reason があればフォールバック表示する', () => {
    // PR3 三層分離以前の bar、もしくは onSignal だけが先に走った in-flight 行
    // のように decision_reason が空、signal_reason に値が入っているケース。
    const item = makeItem({
      marketSignal: { direction: 'BULLISH', strength: 0.4 },
      decision: { intent: 'NEW_ENTRY', side: 'BUY', reason: '' },
      signal: {
        action: 'BUY',
        confidence: 0.4,
        reason: 'trend_follow: SMA uptrend + RSI<70',
      },
    })
    render(<DecisionDetailPanel item={item} />)
    expect(
      screen.getByText('trend_follow: SMA uptrend + RSI<70'),
    ).toBeInTheDocument()
  })

  it('両方空なら "—" を表示する', () => {
    const item = makeItem({
      marketSignal: { direction: 'NEUTRAL', strength: 0 },
      decision: { intent: 'HOLD', side: '', reason: '' },
    })
    render(<DecisionDetailPanel item={item} />)
    // 「判断理由」ラベルの dd セルが — になっていればOK
    const labels = screen.getAllByText('判断理由')
    expect(labels.length).toBeGreaterThan(0)
    expect(screen.getAllByText('—').length).toBeGreaterThan(0)
  })

  it('Phase 1 フィールドが全て空の pre-PR2 行ではパネルを描画しない', () => {
    const item = makeItem()
    const { container } = render(<DecisionDetailPanel item={item} />)
    // 「Signal / Decision (Phase 1)」見出しが消えていること
    expect(container.querySelector('h3')?.textContent).not.toMatch(
      /Signal \/ Decision/,
    )
  })
})
