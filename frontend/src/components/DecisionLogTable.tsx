import { useState } from 'react'
import type { DecisionLogItem, DecisionIntent, SignalDirection } from '../lib/api'
import { translateReason } from '../lib/decisionReasonI18n'
import { DecisionDetailPanel } from './DecisionDetailPanel'

type Props = { decisions: DecisionLogItem[] }

const RISK_LABEL: Record<DecisionLogItem['risk']['outcome'], string> = {
  APPROVED: '承認',
  REJECTED: '却下',
  SKIPPED: '対象外',
}

const BOOK_GATE_LABEL: Record<DecisionLogItem['bookGate']['outcome'], string> = {
  ALLOWED: '通過',
  VETOED: '拒否',
  SKIPPED: '対象外',
}

const ORDER_LABEL: Record<DecisionLogItem['order']['outcome'], string> = {
  FILLED: '約定',
  FAILED: '失敗',
  NOOP: '発注なし',
}

// Phase 1 PR5: new shadow / decision-route columns. Empty string maps to "—"
// so PR1-era rows that lack the new fields render cleanly.
const DIRECTION_LABEL: Record<SignalDirection, string> = {
  BULLISH: '買い優勢',
  BEARISH: '売り優勢',
  NEUTRAL: '中立',
  '': '—',
}

const INTENT_LABEL: Record<DecisionIntent, string> = {
  NEW_ENTRY: '新規エントリー',
  EXIT_CANDIDATE: '利確/損切り候補',
  HOLD: '見送り',
  COOLDOWN_BLOCKED: 'クールダウン',
  '': '—',
}

export function DecisionLogTable({ decisions }: Props) {
  const [expandedId, setExpandedId] = useState<number | null>(null)

  if (decisions.length === 0) {
    return (
      <div className="rounded-2xl border border-white/8 bg-bg-card/90 p-8 text-center text-text-secondary">
        判断ログがまだありません。
      </div>
    )
  }
  return (
    <div className="overflow-hidden rounded-3xl border border-white/8 bg-bg-card/90">
      <table className="w-full text-sm">
        <thead className="bg-white/5 text-xs uppercase tracking-[0.18em] text-text-secondary">
          <tr>
            <th className="px-4 py-3 text-left">時刻</th>
            <th className="px-4 py-3 text-left">スタンス</th>
            <th className="px-4 py-3 text-left">方向</th>
            <th className="px-4 py-3 text-left">判断</th>
            <th className="px-4 py-3 text-left">シグナル</th>
            <th className="px-4 py-3 text-right">信頼度</th>
            <th className="px-4 py-3 text-left">リスク</th>
            <th className="px-4 py-3 text-left">板ガード</th>
            <th className="px-4 py-3 text-left">結果</th>
            <th className="px-4 py-3 text-right">数量/価格</th>
            <th className="px-4 py-3 text-left">理由</th>
          </tr>
        </thead>
        <tbody>
          {decisions.map((d) => (
            <Row
              key={d.id}
              item={d}
              expanded={expandedId === d.id}
              onClick={() => setExpandedId(expandedId === d.id ? null : d.id)}
            />
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Row({
  item,
  expanded,
  onClick,
}: {
  item: DecisionLogItem
  expanded: boolean
  onClick: () => void
}) {
  const bg = rowBackground(item)
  const decisionReason = item.decision?.reason ?? ''
  const rawReason =
    decisionReason ||
    item.signal.reason ||
    item.risk.reason ||
    item.bookGate.reason ||
    item.order.error ||
    '—'
  const reason = translateReason(rawReason)
  const direction = item.marketSignal?.direction ?? ''
  const intent = item.decision?.intent ?? ''
  return (
    <>
      <tr className={`cursor-pointer border-t border-white/8 ${bg}`} onClick={onClick}>
        <td className="px-4 py-3">
          <div>{new Date(item.barCloseAt).toLocaleString('ja-JP')}</div>
          <div className="text-xs text-text-secondary">{item.triggerKind}</div>
        </td>
        <td className="px-4 py-3">{item.stance || '—'}</td>
        <td className="px-4 py-3">{DIRECTION_LABEL[direction]}</td>
        <td className="px-4 py-3 font-medium">{INTENT_LABEL[intent]}</td>
        <td className="px-4 py-3">{item.signal.action}</td>
        <td className="px-4 py-3 text-right">
          {item.signal.action === 'HOLD'
            ? '—'
            : `${(item.signal.confidence * 100).toFixed(1)}%`}
        </td>
        <td className="px-4 py-3">{RISK_LABEL[item.risk.outcome] ?? item.risk.outcome}</td>
        <td className="px-4 py-3">
          {BOOK_GATE_LABEL[item.bookGate.outcome] ?? item.bookGate.outcome}
        </td>
        <td className="px-4 py-3">{ORDER_LABEL[item.order.outcome] ?? item.order.outcome}</td>
        <td className="px-4 py-3 text-right">
          {item.order.outcome === 'NOOP'
            ? '—'
            : `${item.order.amount} @ ${item.order.price.toLocaleString('ja-JP')}`}
        </td>
        <td className="max-w-[24rem] truncate px-4 py-3" title={rawReason}>
          {reason}
        </td>
      </tr>
      {expanded && (
        <tr className="border-t border-white/8 bg-white/3">
          <td colSpan={11} className="px-4 py-4">
            <DecisionDetailPanel item={item} />
          </td>
        </tr>
      )}
    </>
  )
}

function rowBackground(item: DecisionLogItem): string {
  if (item.order.outcome === 'FILLED') return 'bg-accent-green/8'
  if (item.risk.outcome === 'REJECTED' || item.bookGate.outcome === 'VETOED')
    return 'bg-accent-red/8'
  if (item.triggerKind !== 'BAR_CLOSE') return 'bg-white/3'
  // Phase 1 PR5: cooldown / exit-candidate rows are visually distinct from
  // the plain HOLD bars so operators can see where the new Decision layer
  // intervened.
  if (item.decision?.intent === 'COOLDOWN_BLOCKED') return 'bg-white/5'
  if (item.decision?.intent === 'EXIT_CANDIDATE') return 'bg-accent-yellow/8'
  if (item.signal.action === 'HOLD') return 'bg-accent-yellow/6'
  return ''
}
