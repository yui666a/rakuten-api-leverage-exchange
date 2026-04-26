import type { DecisionLogItem } from '../lib/api'

type Props = { item: DecisionLogItem }

export function DecisionDetailPanel({ item }: Props) {
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Section title="主要指標" data={item.indicators} />
      <Section title="上位足指標" data={item.higherTfIndicators} />
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
