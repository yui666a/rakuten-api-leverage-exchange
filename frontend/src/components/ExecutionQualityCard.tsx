import { useExecutionQuality } from '../hooks/useExecutionQuality'

// Compact 24h execution-quality summary. Hidden when the API has not yet
// returned a payload (e.g. endpoint disabled / no trades in window).
export function ExecutionQualityCard() {
  const { data, isLoading, isError } = useExecutionQuality(86400)

  return (
    <div className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Execution Quality</p>
          <h2 className="mt-2 text-xl font-semibold text-white">直近 24 時間</h2>
        </div>
        <span className="rounded-full bg-white/8 px-2.5 py-1 font-mono text-[11px] text-slate-300">24h</span>
      </div>

      {isLoading ? (
        <p className="mt-4 text-sm text-text-secondary">読み込み中…</p>
      ) : isError || !data ? (
        <p className="mt-4 text-sm text-text-secondary">データを取得できませんでした</p>
      ) : data.trades.count === 0 ? (
        <p className="mt-4 text-sm text-text-secondary">直近 24 時間の約定はありません</p>
      ) : (
        <div className="mt-4 grid grid-cols-2 gap-3 text-sm">
          <Row label="取引数" value={`${data.trades.count} 件`} />
          <Row
            label="Maker 比率"
            value={`${(data.trades.makerRatio * 100).toFixed(1)} %`}
            valueClass="text-accent-green"
          />
          <Row
            label="平均スリッページ"
            value={
              data.trades.avgSlippageBps != null
                ? `${data.trades.avgSlippageBps.toFixed(1)} bps`
                : '—'
            }
            valueClass={
              data.trades.avgSlippageBps != null && data.trades.avgSlippageBps > 0
                ? 'text-accent-red'
                : 'text-accent-green'
            }
          />
          <Row
            label="手数料合計"
            value={formatJpy(data.trades.totalFeeJpy)}
            valueClass={data.trades.totalFeeJpy < 0 ? 'text-accent-green' : 'text-slate-200'}
          />
        </div>
      )}
    </div>
  )
}

function Row({
  label,
  value,
  valueClass = 'text-white',
}: {
  label: string
  value: string
  valueClass?: string
}) {
  return (
    <div className="flex flex-col gap-1 rounded-xl border border-white/8 bg-white/5 px-3 py-2">
      <span className="text-[10px] uppercase tracking-wider text-text-secondary">{label}</span>
      <span className={`font-mono text-base ${valueClass}`}>{value}</span>
    </div>
  )
}

function formatJpy(value: number): string {
  if (!Number.isFinite(value)) return '—'
  const sign = value < 0 ? '-' : ''
  const abs = Math.abs(value)
  if (abs < 1) return `${sign}¥${abs.toFixed(2)}`
  return `${sign}¥${Math.round(abs).toLocaleString('ja-JP')}`
}
