// Pure formatters that turn backend payloads into Notification + beep
// parameters. Kept separate from useTradeNotifications so they can be
// unit-tested without mocking React / WebSocket / Notification.

import { formatAmount } from './format'
import type { BeepKind } from './notifier'
import type { RiskEventPayload, TradeEventPayload } from './api'

export type NotifyDescriptor = {
  title: string
  body: string
  tag: string
  beep: BeepKind
}

export function formatTradeEvent(p: TradeEventPayload): NotifyDescriptor {
  const sideJP = p.side === 'BUY' ? '買い' : '売り'
  const verb = p.kind === 'open' ? 'エントリー' : 'クローズ'
  const lot = formatAmount(p.amount)
  const price = `¥${Math.round(p.price).toLocaleString('ja-JP')}`
  return {
    title: `${verb} ${sideJP} ${lot}`,
    body: `${price}${p.reason ? ` — ${p.reason}` : ''}`,
    tag: `trade-${p.orderId}`,
    beep: p.kind === 'open' ? 'info' : 'success',
  }
}

export function formatRiskEvent(p: RiskEventPayload): NotifyDescriptor {
  return {
    title: riskTitle(p.kind),
    body: p.message,
    tag: `risk-${p.kind}-${Math.floor(p.timestamp / 60_000)}`,
    beep: severityToBeep(p.severity),
  }
}

function riskTitle(kind: RiskEventPayload['kind']): string {
  switch (kind) {
    case 'dd_warning':
      return '⚠️ ドローダウン警告'
    case 'dd_critical':
      return '🚨 ドローダウン重大'
    case 'consecutive_losses':
      return '⚠️ 連敗発生'
    case 'cooldown_started':
      return '⏸ クールダウン開始'
    case 'daily_loss_warning':
      return '⚠️ 日次損失警告'
    case 'circuit_breaker':
      return '🚨 サーキットブレーカー作動'
    case 'reconciliation_drift':
      return '⚠️ 整合性ずれ検出'
    default:
      return 'リスクイベント'
  }
}

function severityToBeep(s: RiskEventPayload['severity']): BeepKind {
  switch (s) {
    case 'critical':
      return 'critical'
    case 'warning':
      return 'warning'
    default:
      return 'info'
  }
}
