import { createFileRoute } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { AppFrame } from '../components/AppFrame'
import {
  useMultiPeriodResult,
  useMultiPeriodResults,
} from '../hooks/useMultiPeriod'
import type { LabeledBacktestResult, MultiPeriodResult } from '../lib/api'

export const Route = createFileRoute('/backtest-multi')({
  component: BacktestMultiPage,
})

function BacktestMultiPage() {
  const [profileFilter, setProfileFilter] = useState('')
  const [pdcaFilter, setPdcaFilter] = useState('')
  const [selectedId, setSelectedId] = useState('')

  const { data, isLoading, isError } = useMultiPeriodResults({
    profileName: profileFilter,
    pdcaCycleId: pdcaFilter,
    limit: 50,
  })
  const { data: detail, isLoading: detailLoading } = useMultiPeriodResult(selectedId)

  const rows = useMemo(() => {
    const items = data?.results ?? []
    // Rank by RobustnessScore desc (higher is better). Nulls sink to the
    // bottom so ruin-path runs don't hide profitable candidates.
    return [...items].sort((a, b) => {
      const aS = a.aggregate.robustnessScore
      const bS = b.aggregate.robustnessScore
      if (aS == null && bS == null) return 0
      if (aS == null) return 1
      if (bS == null) return -1
      return bS - aS
    })
  }, [data])

  return (
    <AppFrame
      title="マルチ期間バックテスト"
      subtitle="`/backtest/run-multi` で保存された envelope を RobustnessScore でランキング表示"
    >
      <section className="rounded-3xl border border-white/8 bg-bg-card p-5 sm:p-6">
        <div className="mb-4 flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1 text-xs text-text-secondary">
            Profile Name
            <input
              value={profileFilter}
              onChange={(e) => setProfileFilter(e.target.value)}
              className="rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-sm text-white"
              placeholder="production"
            />
          </label>
          <label className="flex flex-col gap-1 text-xs text-text-secondary">
            PDCA Cycle ID
            <input
              value={pdcaFilter}
              onChange={(e) => setPdcaFilter(e.target.value)}
              className="rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-sm text-white"
              placeholder="cycle22"
            />
          </label>
        </div>

        {isLoading && <p className="text-sm text-text-secondary">Loading…</p>}
        {isError && (
          <p className="text-sm text-accent-red">Multi-period 結果の取得に失敗しました。</p>
        )}
        {!isLoading && !isError && rows.length === 0 && (
          <p className="text-sm text-text-secondary">
            該当する multi-period 実行がありません。`POST /api/v1/backtest/run-multi` で実行してください。
          </p>
        )}

        {rows.length > 0 && <MultiResultsTable rows={rows} onSelect={setSelectedId} selectedId={selectedId} />}
      </section>

      {selectedId !== '' && (
        <section className="mt-6 rounded-3xl border border-white/8 bg-bg-card p-5 sm:p-6">
          {detailLoading && <p className="text-sm text-text-secondary">Detail loading…</p>}
          {detail && <MultiResultDetail detail={detail} />}
        </section>
      )}
    </AppFrame>
  )
}

function MultiResultsTable({
  rows,
  onSelect,
  selectedId,
}: {
  rows: MultiPeriodResult[]
  onSelect: (id: string) => void
  selectedId: string
}) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] text-sm">
        <thead>
          <tr className="border-b border-white/8 text-left text-xs uppercase tracking-wider text-text-secondary">
            <th className="px-3 py-2">Created</th>
            <th className="px-3 py-2">Profile</th>
            <th className="px-3 py-2">Cycle</th>
            <th className="px-3 py-2 text-right">Periods</th>
            <th className="px-3 py-2 text-right">GeomMean %</th>
            <th className="px-3 py-2 text-right">Worst %</th>
            <th className="px-3 py-2 text-right">Best %</th>
            <th className="px-3 py-2 text-right">Robustness</th>
            <th className="px-3 py-2">All+?</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const a = r.aggregate
            const createdAt = new Date(r.createdAt * 1000).toLocaleString('ja-JP')
            const isSelected = r.id === selectedId
            return (
              <tr
                key={r.id}
                onClick={() => onSelect(r.id)}
                className={`cursor-pointer border-b border-white/5 hover:bg-white/5 ${isSelected ? 'bg-white/10' : ''}`}
              >
                <td className="px-3 py-2 text-text-secondary text-xs">{createdAt}</td>
                <td className="px-3 py-2 text-white">{r.profileName || '—'}</td>
                <td className="px-3 py-2 text-text-secondary">{r.pdcaCycleId || '—'}</td>
                <td className="px-3 py-2 text-right text-white">{r.periods?.length ?? 0}</td>
                <td className={`px-3 py-2 text-right ${pnlColor(a.geomMeanReturn)}`}>{formatPercent(a.geomMeanReturn)}</td>
                <td className={`px-3 py-2 text-right ${pnlColor(a.worstReturn)}`}>{formatPercent(a.worstReturn)}</td>
                <td className={`px-3 py-2 text-right ${pnlColor(a.bestReturn)}`}>{formatPercent(a.bestReturn)}</td>
                <td className="px-3 py-2 text-right text-white">{formatNum(a.robustnessScore)}</td>
                <td className="px-3 py-2">
                  <span className={a.allPositive ? 'text-accent-green' : 'text-text-secondary'}>
                    {a.allPositive ? '✓' : '—'}
                  </span>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function MultiResultDetail({ detail }: { detail: MultiPeriodResult }) {
  const a = detail.aggregate
  return (
    <div>
      <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Detail</p>
      <h2 className="mt-2 text-xl font-semibold text-white">
        {detail.profileName} {detail.pdcaCycleId ? `/ ${detail.pdcaCycleId}` : ''}
      </h2>
      {detail.hypothesis && (
        <p className="mt-1 text-sm text-text-secondary">Hypothesis: {detail.hypothesis}</p>
      )}

      {/* Aggregate KPIs */}
      <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard label="GeomMean Return" value={formatPercent(a.geomMeanReturn)} color={pnlColor(a.geomMeanReturn)} />
        <KpiCard label="StdDev Return" value={formatPercent(a.returnStdDev)} />
        <KpiCard label="Worst Return" value={formatPercent(a.worstReturn)} color={pnlColor(a.worstReturn)} />
        <KpiCard label="Best Return" value={formatPercent(a.bestReturn)} color={pnlColor(a.bestReturn)} />
        <KpiCard label="Worst Drawdown" value={formatPercent(a.worstDrawdown)} color="text-accent-red" />
        <KpiCard label="Robustness" value={formatNum(a.robustnessScore)} />
        <KpiCard label="All Positive" value={a.allPositive ? 'Yes' : 'No'} color={a.allPositive ? 'text-accent-green' : 'text-accent-red'} />
      </div>

      {/* Per-period table */}
      <h3 className="mt-6 text-lg font-semibold text-white">Per-period 結果</h3>
      <div className="mt-3 overflow-x-auto">
        <table className="w-full min-w-[720px] text-sm">
          <thead>
            <tr className="border-b border-white/8 text-left text-xs uppercase tracking-wider text-text-secondary">
              <th className="px-3 py-2">Label</th>
              <th className="px-3 py-2 text-right">Return</th>
              <th className="px-3 py-2 text-right">Max DD</th>
              <th className="px-3 py-2 text-right">Win Rate</th>
              <th className="px-3 py-2 text-right">Profit Factor</th>
              <th className="px-3 py-2 text-right">Sharpe</th>
              <th className="px-3 py-2 text-right">Trades</th>
            </tr>
          </thead>
          <tbody>
            {detail.periods.map((p) => (
              <PeriodRow key={p.label} period={p} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function PeriodRow({ period }: { period: LabeledBacktestResult }) {
  const s = period.result.summary
  return (
    <tr className="border-b border-white/5">
      <td className="px-3 py-2 text-white">{period.label}</td>
      <td className={`px-3 py-2 text-right ${pnlColor(s.totalReturn)}`}>
        {`${(s.totalReturn * 100).toFixed(2)}%`}
      </td>
      <td className="px-3 py-2 text-right text-accent-red">{`${(s.maxDrawdown * 100).toFixed(2)}%`}</td>
      <td className="px-3 py-2 text-right text-white">{s.winRate.toFixed(1)}%</td>
      <td className={`px-3 py-2 text-right ${s.profitFactor >= 1 ? 'text-accent-green' : 'text-accent-red'}`}>
        {s.profitFactor.toFixed(2)}
      </td>
      <td className="px-3 py-2 text-right text-white">{s.sharpeRatio.toFixed(2)}</td>
      <td className="px-3 py-2 text-right text-white">{s.totalTrades}</td>
    </tr>
  )
}

function KpiCard({ label, value, color = 'text-white' }: { label: string; value: string; color?: string }) {
  return (
    <div className="rounded-2xl border border-white/8 bg-white/4 p-4">
      <p className="text-xs uppercase tracking-[0.25em] text-text-secondary">{label}</p>
      <p className={`mt-2 text-lg font-semibold ${color}`}>{value}</p>
    </div>
  )
}

function pnlColor(v: number | null | undefined): string {
  if (v == null) return 'text-text-secondary'
  if (v > 0) return 'text-accent-green'
  if (v < 0) return 'text-accent-red'
  return 'text-white'
}

function formatPercent(v: number | null | undefined): string {
  if (v == null) return '—'
  return `${(v * 100).toFixed(2)}%`
}

function formatNum(v: number | null | undefined): string {
  if (v == null) return '—'
  return v.toFixed(4)
}
