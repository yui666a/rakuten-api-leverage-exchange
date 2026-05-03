import type { DecisionLogItem } from '../lib/api'

type Props = { item: DecisionLogItem }

export function DecisionDetailPanel({ item }: Props) {
  return (
    <div className="space-y-4">
      <PhaseOnePanel item={item} />
      <div className="grid gap-4 md:grid-cols-2">
        <Section title="主要指標" data={item.indicators} />
        <Section title="上位足指標" data={item.higherTfIndicators} />
      </div>
    </div>
  )
}

// PhaseOnePanel surfaces the Signal/Decision/ExecutionPolicy three-layer
// payload that PR1–PR3 introduced. Old rows (pre-PR2) carry empty strings /
// 0 in these fields; the panel still renders them as "—" so legacy data
// stays browsable.
function PhaseOnePanel({ item }: { item: DecisionLogItem }) {
  const direction = item.marketSignal?.direction ?? ''
  const strength = item.marketSignal?.strength ?? 0
  const intent = item.decision?.intent ?? ''
  const side = item.decision?.side ?? ''
  const decisionReason = item.decision?.reason ?? ''
  const exitOutcome = item.exitPolicyOutcome ?? ''

  // Hide the panel entirely when none of the new fields are populated —
  // there is no point showing "—" for every row in pre-PR2 backtest data.
  if (!direction && !intent && !side && !decisionReason && !exitOutcome) {
    return null
  }

  return (
    <div className="rounded-2xl border border-white/8 p-4">
      <h3 className="mb-3 text-xs uppercase tracking-[0.2em] text-text-secondary">
        Signal / Decision (Phase 1)
      </h3>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm md:grid-cols-3">
        <KV label="方向 (Direction)" value={direction || '—'} />
        <KV label="強度 (Strength)" value={strength ? strength.toFixed(2) : '—'} />
        <KV label="判断 (Intent)" value={intent || '—'} />
        <KV label="サイド" value={side || '—'} />
        <KV label="出口判定" value={exitOutcome || '—'} />
        <KV label="判断理由" value={decisionReason || '—'} wide />
      </dl>
    </div>
  )
}

function KV({ label, value, wide }: { label: string; value: string; wide?: boolean }) {
  return (
    <div className={wide ? 'contents md:col-span-3' : 'contents'}>
      <dt className="truncate text-text-secondary">{label}</dt>
      <dd className={wide ? 'md:col-span-2' : 'truncate text-right'}>{value}</dd>
    </div>
  )
}

function Section({ title, data }: { title: string; data: Record<string, unknown> }) {
  const entries = Object.entries(data ?? {})
  if (entries.length === 0) {
    return (
      <div className="rounded-2xl border border-white/8 p-4">
        <h3 className="text-xs uppercase tracking-[0.2em] text-text-secondary">{title}</h3>
        <p className="mt-2 text-sm text-text-secondary">データなし</p>
      </div>
    )
  }
  return (
    <div className="rounded-2xl border border-white/8 p-4">
      <h3 className="mb-3 text-xs uppercase tracking-[0.2em] text-text-secondary">{title}</h3>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
        {entries.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="truncate text-text-secondary">{k}</dt>
            <dd className="truncate text-right">{formatValue(v)}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

function formatValue(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'number') return Number.isFinite(v) ? v.toFixed(4) : String(v)
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
