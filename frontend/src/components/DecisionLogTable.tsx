import { useState } from 'react'
import type { DecisionLogItem } from '../lib/api'
import { DecisionDetailPanel } from './DecisionDetailPanel'

type Props = { decisions: DecisionLogItem[] }

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
            <th className="px-4 py-3 text-left">シグナル</th>
            <th className="px-4 py-3 text-right">信頼度</th>
            <th className="px-4 py-3 text-left">リスク</th>
            <th className="px-4 py-3 text-left">BookGate</th>
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
  const reason =
    item.signal.reason || item.risk.reason || item.bookGate.reason || item.order.error || '—'
  return (
    <>
      <tr className={`cursor-pointer border-t border-white/8 ${bg}`} onClick={onClick}>
        <td className="px-4 py-3">
          <div>{new Date(item.barCloseAt).toLocaleString('ja-JP')}</div>
          <div className="text-xs text-text-secondary">{item.triggerKind}</div>
        </td>
        <td className="px-4 py-3">{item.stance || '—'}</td>
        <td className="px-4 py-3 font-medium">{item.signal.action}</td>
        <td className="px-4 py-3 text-right">
          {item.signal.action === 'HOLD' ? '—' : item.signal.confidence.toFixed(2)}
        </td>
        <td className="px-4 py-3">{item.risk.outcome}</td>
        <td className="px-4 py-3">{item.bookGate.outcome}</td>
        <td className="px-4 py-3">{item.order.outcome}</td>
        <td className="px-4 py-3 text-right">
          {item.order.outcome === 'NOOP'
            ? '—'
            : `${item.order.amount} @ ${item.order.price.toLocaleString('ja-JP')}`}
        </td>
        <td className="max-w-[24rem] truncate px-4 py-3">{reason}</td>
      </tr>
      {expanded && (
        <tr className="border-t border-white/8 bg-white/3">
          <td colSpan={9} className="px-4 py-4">
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
  if (item.signal.action === 'HOLD') return 'bg-accent-yellow/6'
  return ''
}
