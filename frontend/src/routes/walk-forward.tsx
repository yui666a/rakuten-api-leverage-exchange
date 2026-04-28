import { createFileRoute, redirect } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import {
  useWalkForwardResult,
  useWalkForwardResults,
} from '../hooks/useWalkForward'
import type {
  WalkForwardEnvelopeResponse,
  WalkForwardWindowResult,
} from '../lib/api'

// /walk-forward は /analysis?view=wfo に統合された。本体ロジックは
// WalkForwardBody として export しており、新ルートから再利用される。
export const Route = createFileRoute('/walk-forward')({
  beforeLoad: ({ search }) => {
    throw redirect({ to: '/analysis', search: { ...search, view: 'wfo' } })
  },
})

// Body without AppFrame — usable by /analysis tabbed view.
export function WalkForwardBody() {
  const [profileFilter, setProfileFilter] = useState('')
  const [pdcaFilter, setPdcaFilter] = useState('')
  const [selectedId, setSelectedId] = useState('')

  const { data, isLoading, isError } = useWalkForwardResults({
    baseProfile: profileFilter,
    pdcaCycleId: pdcaFilter,
    limit: 50,
  })
  const { data: detail, isLoading: detailLoading } = useWalkForwardResult(selectedId)

  const rows = useMemo(() => {
    const items = data?.items ?? []
    return [...items].sort((a, b) => {
      const aS = a.aggregateOOS?.robustnessScore ?? null
      const bS = b.aggregateOOS?.robustnessScore ?? null
      if (aS == null && bS == null) return 0
      if (aS == null) return 1
      if (bS == null) return -1
      return bS - aS
    })
  }, [data])

  return (
    <>
      <section className="rounded-3xl border border-white/8 bg-bg-card p-5 sm:p-6">
        <div className="mb-4 flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1 text-xs text-text-secondary">
            Base Profile
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
          <p className="text-sm text-accent-red">
            Walk-forward 結果の取得に失敗しました（BE 側で walk-forward リポジトリが未ワイヤの場合は 503）。
          </p>
        )}
        {!isLoading && !isError && rows.length === 0 && (
          <p className="text-sm text-text-secondary">
            該当する walk-forward 実行がありません。POST /api/v1/backtest/walk-forward で実行してください。
          </p>
        )}

        {rows.length > 0 && (
          <WalkForwardListTable rows={rows} onSelect={setSelectedId} selectedId={selectedId} />
        )}
      </section>

      {selectedId !== '' && (
        <section className="mt-6 rounded-3xl border border-white/8 bg-bg-card p-5 sm:p-6">
          {detailLoading && <p className="text-sm text-text-secondary">Detail loading…</p>}
          {detail && <WalkForwardDetail env={detail} />}
        </section>
      )}
    </>
  )
}

function WalkForwardListTable({
  rows,
  onSelect,
  selectedId,
}: {
  rows: WalkForwardEnvelopeResponse[]
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
            <th className="px-3 py-2">Objective</th>
            <th className="px-3 py-2 text-right">GeomMean %</th>
            <th className="px-3 py-2 text-right">Worst %</th>
            <th className="px-3 py-2 text-right">Best %</th>
            <th className="px-3 py-2 text-right">Robustness</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const a = r.aggregateOOS
            const createdAt = new Date(r.createdAt * 1000).toLocaleString('ja-JP')
            const isSelected = r.id === selectedId
            return (
              <tr
                key={r.id}
                onClick={() => onSelect(r.id)}
                className={`cursor-pointer border-b border-white/5 hover:bg-white/5 ${isSelected ? 'bg-white/10' : ''}`}
              >
                <td className="px-3 py-2 text-text-secondary text-xs">{createdAt}</td>
                <td className="px-3 py-2 text-white">{r.baseProfile}</td>
                <td className="px-3 py-2 text-text-secondary">{r.pdcaCycleId || '—'}</td>
                <td className="px-3 py-2 text-text-secondary">{r.objective || 'return'}</td>
                <td className={`px-3 py-2 text-right ${pnlColor(a?.geomMeanReturn)}`}>{formatPercent(a?.geomMeanReturn)}</td>
                <td className={`px-3 py-2 text-right ${pnlColor(a?.worstReturn)}`}>{formatPercent(a?.worstReturn)}</td>
                <td className={`px-3 py-2 text-right ${pnlColor(a?.bestReturn)}`}>{formatPercent(a?.bestReturn)}</td>
                <td className="px-3 py-2 text-right text-white">{formatNum(a?.robustnessScore)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function WalkForwardDetail({ env }: { env: WalkForwardEnvelopeResponse }) {
  const result = env.result
  const a = env.aggregateOOS

  // Best-parameter frequency: across all windows, count how often each
  // (path = value) tuple was selected. Robust parameters show up in many
  // windows — exactly what PDCA triage wants to see.
  const bestParamFrequency = useMemo(() => {
    if (!result) return new Map<string, number>()
    const counts = new Map<string, number>()
    for (const w of result.windows) {
      for (const [path, v] of Object.entries(w.bestParameters)) {
        const key = `${path} = ${v}`
        counts.set(key, (counts.get(key) ?? 0) + 1)
      }
    }
    return counts
  }, [result])

  return (
    <div>
      <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Detail</p>
      <h2 className="mt-2 text-xl font-semibold text-white">
        {env.baseProfile} {env.pdcaCycleId ? `/ ${env.pdcaCycleId}` : ''}
      </h2>
      {env.hypothesis && (
        <p className="mt-1 text-sm text-text-secondary">Hypothesis: {env.hypothesis}</p>
      )}

      <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard label="GeomMean OOS" value={formatPercent(a?.geomMeanReturn)} color={pnlColor(a?.geomMeanReturn)} />
        <KpiCard label="Worst OOS" value={formatPercent(a?.worstReturn)} color={pnlColor(a?.worstReturn)} />
        <KpiCard label="Best OOS" value={formatPercent(a?.bestReturn)} color={pnlColor(a?.bestReturn)} />
        <KpiCard label="Worst Drawdown" value={formatPercent(a?.worstDrawdown)} color="text-accent-red" />
        <KpiCard label="Robustness" value={formatNum(a?.robustnessScore)} />
        <KpiCard label="All Positive" value={a?.allPositive ? 'Yes' : 'No'} color={a?.allPositive ? 'text-accent-green' : 'text-accent-red'} />
        <KpiCard label="Objective" value={env.objective || 'return'} />
        <KpiCard label="Windows" value={String(result?.windows.length ?? 0)} />
      </div>

      {result && result.windows.length > 0 && (
        <>
          <h3 className="mt-6 text-lg font-semibold text-white">窓別 OOS リターン</h3>
          <OOSLineChart windows={result.windows} />

          <h3 className="mt-6 text-lg font-semibold text-white">窓別サマリ</h3>
          <WindowsTable windows={result.windows} />

          <h3 className="mt-6 text-lg font-semibold text-white">Best Parameter 頻度</h3>
          <BestParameterTable frequency={bestParamFrequency} totalWindows={result.windows.length} />
        </>
      )}
    </div>
  )
}

// OOSLineChart renders an inline SVG spark-line of OOS returns by window
// index. lightweight-charts would be overkill for at most ~10 bars per
// typical WFO schedule; an SVG keeps bundle size down.
function OOSLineChart({ windows }: { windows: WalkForwardWindowResult[] }) {
  const values = windows.map((w) => w.oosResult.summary.totalReturn)
  if (values.length === 0) return null
  const min = Math.min(...values, 0)
  const max = Math.max(...values, 0)
  const range = max - min || 1
  const width = Math.max(windows.length * 80, 400)
  const height = 160
  const pad = 24

  const toX = (i: number) => pad + ((width - pad * 2) * i) / Math.max(windows.length - 1, 1)
  const toY = (v: number) => pad + (height - pad * 2) * (1 - (v - min) / range)

  const pathD = values.map((v, i) => `${i === 0 ? 'M' : 'L'}${toX(i)},${toY(v)}`).join(' ')
  const zeroY = toY(0)

  return (
    <div className="mt-3 overflow-x-auto">
      <svg width={width} height={height} className="bg-bg-card rounded-lg">
        <line x1={pad} y1={zeroY} x2={width - pad} y2={zeroY} stroke="rgba(255,255,255,0.15)" strokeDasharray="2,3" />
        <path d={pathD} stroke="#06d6a0" strokeWidth={2} fill="none" />
        {values.map((v, i) => (
          <g key={i}>
            <circle cx={toX(i)} cy={toY(v)} r={3} fill={v >= 0 ? '#06d6a0' : '#ef476f'} />
            <text x={toX(i)} y={toY(v) - 8} textAnchor="middle" fontSize="10" fill="#e0e0e0">
              {(v * 100).toFixed(1)}%
            </text>
            <text x={toX(i)} y={height - 6} textAnchor="middle" fontSize="10" fill="#9ca3af">
              #{windows[i].index}
            </text>
          </g>
        ))}
      </svg>
    </div>
  )
}

function WindowsTable({ windows }: { windows: WalkForwardWindowResult[] }) {
  return (
    <div className="mt-3 overflow-x-auto">
      <table className="w-full min-w-[720px] text-sm">
        <thead>
          <tr className="border-b border-white/8 text-left text-xs uppercase tracking-wider text-text-secondary">
            <th className="px-3 py-2">#</th>
            <th className="px-3 py-2">IS</th>
            <th className="px-3 py-2">OOS</th>
            <th className="px-3 py-2">Best Params</th>
            <th className="px-3 py-2 text-right">OOS Return</th>
            <th className="px-3 py-2 text-right">OOS Max DD</th>
            <th className="px-3 py-2 text-right">OOS Trades</th>
          </tr>
        </thead>
        <tbody>
          {windows.map((w) => {
            const s = w.oosResult.summary
            return (
              <tr key={w.index} className="border-b border-white/5">
                <td className="px-3 py-2 text-white">#{w.index}</td>
                <td className="px-3 py-2 text-text-secondary text-xs">
                  {formatDate(w.inSampleFrom)} ~ {formatDate(w.inSampleTo)}
                </td>
                <td className="px-3 py-2 text-text-secondary text-xs">
                  {formatDate(w.oosFrom)} ~ {formatDate(w.oosTo)}
                </td>
                <td className="px-3 py-2 text-white text-xs">{formatParams(w.bestParameters)}</td>
                <td className={`px-3 py-2 text-right ${pnlColor(s.totalReturn)}`}>{`${(s.totalReturn * 100).toFixed(2)}%`}</td>
                <td className="px-3 py-2 text-right text-accent-red">{`${(s.maxDrawdown * 100).toFixed(2)}%`}</td>
                <td className="px-3 py-2 text-right text-white">{s.totalTrades}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function BestParameterTable({ frequency, totalWindows }: { frequency: Map<string, number>; totalWindows: number }) {
  const entries = [...frequency.entries()].sort((a, b) => b[1] - a[1])
  if (entries.length === 0) {
    return <p className="mt-3 text-sm text-text-secondary">ベースラインのみ（grid なし）。</p>
  }
  return (
    <div className="mt-3 overflow-x-auto">
      <table className="w-full min-w-[480px] text-sm">
        <thead>
          <tr className="border-b border-white/8 text-left text-xs uppercase tracking-wider text-text-secondary">
            <th className="px-3 py-2">Parameter</th>
            <th className="px-3 py-2 text-right">Count</th>
            <th className="px-3 py-2 text-right">% of windows</th>
          </tr>
        </thead>
        <tbody>
          {entries.map(([key, count]) => (
            <tr key={key} className="border-b border-white/5">
              <td className="px-3 py-2 text-white">{key}</td>
              <td className="px-3 py-2 text-right text-white">
                {count} / {totalWindows}
              </td>
              <td className={`px-3 py-2 text-right ${count / totalWindows >= 0.6 ? 'text-accent-green' : 'text-text-secondary'}`}>
                {((count / totalWindows) * 100).toFixed(0)}%
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
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

function formatDate(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10)
}

function formatParams(params: Record<string, number>): string {
  const entries = Object.entries(params)
  if (entries.length === 0) return 'baseline'
  return entries.map(([k, v]) => `${k.split('.').pop()}=${v}`).join(', ')
}
