import { useRef } from 'react'
import { Link } from '@tanstack/react-router'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { DecisionLogItem, StrategyResponse } from '../lib/api'
import { useDecisionLog } from '../hooks/useDecisionLog'
import { translateReason } from '../lib/decisionReasonI18n'
import { StanceLegendPopover } from './StanceLegendPopover'

const RECENT_LIMIT = 200
const ROW_HEIGHT = 36 // px
const VISIBLE_ROWS = 12
// 各列の grid template (8 列、合計 100%)
const GRID_TEMPLATE_COLUMNS = '8% 12% 9% 7% 8% 11% 14% 31%'

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
          <VirtualizedDecisionTable decisions={decisions} />
        )}
      </div>
    </section>
  )
}

function VirtualizedDecisionTable({ decisions }: { decisions: DecisionLogItem[] }) {
  const parentRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: decisions.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 5,
  })

  return (
    <div
      ref={parentRef}
      className="overflow-auto rounded-2xl border border-white/8 text-xs"
      style={{ height: VISIBLE_ROWS * ROW_HEIGHT }}
    >
      <div
        className="sticky top-0 z-10 grid bg-bg-card text-[0.65rem] uppercase tracking-[0.18em] text-text-secondary"
        style={{ gridTemplateColumns: GRID_TEMPLATE_COLUMNS }}
      >
        <div className="px-3 py-2 text-left">時刻</div>
        <div className="px-3 py-2 text-left">スタンス</div>
        <div className="px-3 py-2 text-left">判断</div>
        <div className="px-3 py-2 text-left">シグナル</div>
        <div className="px-3 py-2 text-right">信頼度</div>
        <div className="px-3 py-2 text-left">結果</div>
        <div className="px-3 py-2 text-right">数量/価格</div>
        <div className="px-3 py-2 text-left">理由</div>
      </div>
      <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
        {virtualizer.getVirtualItems().map((vrow) => {
          const item = decisions[vrow.index]
          return (
            <VirtualRow
              key={item.id}
              item={item}
              top={vrow.start}
              height={ROW_HEIGHT}
            />
          )
        })}
      </div>
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

function VirtualRow({
  item,
  top,
  height,
}: {
  item: DecisionLogItem
  top: number
  height: number
}) {
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
    <div
      className={`grid border-t border-white/8 ${bg}`}
      style={{
        position: 'absolute',
        top: 0,
        left: 0,
        width: '100%',
        height,
        transform: `translateY(${top}px)`,
        gridTemplateColumns: GRID_TEMPLATE_COLUMNS,
      }}
    >
      <div className="px-3 py-2 whitespace-nowrap">
        {new Date(item.barCloseAt).toLocaleTimeString('ja-JP', {
          hour: '2-digit',
          minute: '2-digit',
        })}
      </div>
      <div className="px-3 py-2 truncate">{item.stance || '—'}</div>
      <div className="px-3 py-2 whitespace-nowrap">
        {INTENT_SHORT_LABEL[intent]}
      </div>
      <div className="px-3 py-2 font-medium">{item.signal.action}</div>
      <div className="px-3 py-2 text-right">
        {item.signal.action === 'HOLD'
          ? '—'
          : `${(item.signal.confidence * 100).toFixed(1)}%`}
      </div>
      <div className="px-3 py-2 whitespace-nowrap truncate">{outcome}</div>
      <div className="px-3 py-2 text-right whitespace-nowrap truncate">
        {item.order.outcome === 'NOOP'
          ? '—'
          : `${item.order.amount} @ ${item.order.price.toLocaleString('ja-JP')}`}
      </div>
      <div
        className="truncate px-3 py-2 text-text-secondary"
        title={rawReason}
      >
        {reason}
      </div>
    </div>
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
