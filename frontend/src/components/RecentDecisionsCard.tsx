import { Link } from '@tanstack/react-router'
import type { DecisionLogItem, StrategyResponse } from '../lib/api'
import { useDecisionLog } from '../hooks/useDecisionLog'
import { translateReason } from '../lib/decisionReasonI18n'
import { StanceLegendPopover } from './StanceLegendPopover'

const RECENT_LIMIT = 200

type Props = {
  symbolId: number
  strategy: StrategyResponse | undefined
  rootSearch: { symbol?: string }
}

export function RecentDecisionsCard({ symbolId, strategy, rootSearch }: Props) {
  const { data, isLoading } = useDecisionLog(symbolId, RECENT_LIMIT)
  const decisions = data?.decisions ?? []

  const stance = strategy?.stance ?? null
  const reasoningLabel = pickReasoningLabel(strategy?.reasoning)
  const lastEvaluatedAt = decisions[0]?.barCloseAt ?? null

  return (
    <section className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">直近の判断</p>
          <div className="flex flex-wrap items-center gap-3">
            <span
              className={`text-3xl font-bold tracking-wide ${stanceColorClass(stance)}`}
            >
              {stance ?? '—'}
            </span>
            <StanceLegendPopover />
            {lastEvaluatedAt !== null && (
              <span className="text-xs text-text-secondary">
                最終評価: {new Date(lastEvaluatedAt).toLocaleString('ja-JP')}
              </span>
            )}
          </div>
        </div>
        <Link
          to="/history"
          search={{ ...rootSearch, tab: 'decisions' }}
          className="text-sm text-cyan-200 transition hover:text-cyan-100"
        >
          全件を見る →
        </Link>
      </header>

      <p className="mt-3 text-sm leading-7 text-slate-300">{reasoningLabel}</p>

      <div className="mt-4">
        {isLoading && decisions.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/3 p-4 text-center text-xs text-text-secondary">
            読み込み中…
          </div>
        ) : decisions.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/3 p-4 text-center text-xs text-text-secondary">
            まだ判断履歴がありません。
          </div>
        ) : (
          <MiniDecisionTable decisions={decisions} />
        )}
      </div>
    </section>
  )
}

function MiniDecisionTable({ decisions }: { decisions: DecisionLogItem[] }) {
  return (
    <div className="overflow-hidden rounded-2xl border border-white/8">
      <table className="w-full text-xs">
        <thead className="bg-white/5 text-[0.65rem] uppercase tracking-[0.18em] text-text-secondary">
          <tr>
            <th className="px-3 py-2 text-left">時刻</th>
            <th className="px-3 py-2 text-left">スタンス</th>
            <th className="px-3 py-2 text-left">判断</th>
            <th className="px-3 py-2 text-left">シグナル</th>
            <th className="px-3 py-2 text-right">信頼度</th>
            <th className="px-3 py-2 text-left">結果</th>
            <th className="px-3 py-2 text-right">数量/価格</th>
            <th className="px-3 py-2 text-left">理由</th>
          </tr>
        </thead>
        <tbody>
          {decisions.map((d) => (
            <MiniRow key={d.id} item={d} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

// Phase 1 PR5: short Intent labels for the dashboard mini-table. Empty
// (pre-PR2 row) maps to "—" so legacy data still renders.
const INTENT_SHORT_LABEL: Record<NonNullable<DecisionLogItem['decision']>['intent'], string> = {
  NEW_ENTRY: '新規',
  EXIT_CANDIDATE: '出口候補',
  HOLD: '見送り',
  COOLDOWN_BLOCKED: 'クールダウン',
  '': '—',
}

function MiniRow({ item }: { item: DecisionLogItem }) {
  const bg = rowBackground(item)
  const rawReason =
    item.decision?.reason ||
    item.signal.reason ||
    item.risk.reason ||
    item.bookGate.reason ||
    item.order.error ||
    '—'
  const reason = translateReason(rawReason)
  const outcome = outcomeLabel(item)
  const intent = item.decision?.intent ?? ''
  return (
    <tr className={`border-t border-white/8 ${bg}`}>
      <td className="px-3 py-2 whitespace-nowrap">
        {new Date(item.barCloseAt).toLocaleTimeString('ja-JP', {
          hour: '2-digit',
          minute: '2-digit',
        })}
      </td>
      <td className="px-3 py-2">{item.stance || '—'}</td>
      <td className="px-3 py-2 whitespace-nowrap">{INTENT_SHORT_LABEL[intent]}</td>
      <td className="px-3 py-2 font-medium">{item.signal.action}</td>
      <td className="px-3 py-2 text-right">
        {item.signal.action === 'HOLD'
          ? '—'
          : `${(item.signal.confidence * 100).toFixed(1)}%`}
      </td>
      <td className="px-3 py-2 whitespace-nowrap">{outcome}</td>
      <td className="px-3 py-2 text-right whitespace-nowrap">
        {item.order.outcome === 'NOOP'
          ? '—'
          : `${item.order.amount} @ ${item.order.price.toLocaleString('ja-JP')}`}
      </td>
      <td className="max-w-[18rem] truncate px-3 py-2 text-text-secondary" title={rawReason}>
        {reason}
      </td>
    </tr>
  )
}

function pickReasoningLabel(reasoning: string | undefined): string {
  if (!reasoning) return '戦略コメントはまだ生成されていません。'
  if (reasoning === 'insufficient indicator data') return '指標データが不足しています'
  return reasoning
}

function stanceColorClass(stance: string | null): string {
  switch (stance) {
    case 'TREND_FOLLOW':
      return 'text-accent-green'
    case 'CONTRARIAN':
      return 'text-amber-300'
    case 'BREAKOUT':
      return 'text-fuchsia-300'
    case 'HOLD':
      return 'text-cyan-200'
    default:
      return 'text-text-secondary'
  }
}

function rowBackground(item: DecisionLogItem): string {
  if (item.order.outcome === 'FILLED') return 'bg-accent-green/8'
  if (item.risk.outcome === 'REJECTED' || item.bookGate.outcome === 'VETOED')
    return 'bg-accent-red/8'
  if (item.triggerKind !== 'BAR_CLOSE') return 'bg-white/3'
  if (item.signal.action === 'HOLD') return 'bg-accent-yellow/6'
  return ''
}

function outcomeLabel(item: DecisionLogItem): string {
  if (item.order.outcome === 'FILLED') return `約定`
  if (item.order.outcome === 'FAILED') return '失敗'
  if (item.risk.outcome === 'REJECTED') return '却下(リスク)'
  if (item.bookGate.outcome === 'VETOED') return '却下(板)'
  // Phase 1 PR5: cooldown / exit candidates short-circuit before the
  // legacy HOLD / NOOP labels.
  if (item.decision?.intent === 'COOLDOWN_BLOCKED') return 'クールダウン'
  if (item.decision?.intent === 'EXIT_CANDIDATE') return '出口候補(待機)'
  if (item.signal.action === 'HOLD') return 'HOLD'
  if (item.triggerKind !== 'BAR_CLOSE') return item.triggerKind
  return '発注なし'
}
