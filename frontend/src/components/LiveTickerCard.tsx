import type { LiveTicker } from '../lib/api'

type LiveTickerCardProps = {
  ticker: LiveTicker | null
  connectionState: 'connecting' | 'connected' | 'disconnected'
  currencyPair?: string
}

function formatYen(value: number | null | undefined) {
  if (value === null || value === undefined) return '\u2014'
  return `¥${value.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}`
}

function formatTime(timestamp: number | null | undefined) {
  if (!timestamp) return '\u2014'
  return new Date(timestamp).toLocaleTimeString('ja-JP')
}

export function LiveTickerCard({ ticker, connectionState, currencyPair }: LiveTickerCardProps) {
  const delta = ticker ? ticker.last - ticker.open : null
  const deltaClass = delta !== null && delta < 0 ? 'text-accent-red' : 'text-accent-green'
  const pairLabel = currencyPair ?? 'BTC/JPY'

  return (
    <section className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Realtime Ticker</p>
          <h2 className="mt-2 text-xl font-semibold text-white">{pairLabel} ライブ価格</h2>
        </div>
        <span className={`rounded-full px-3 py-1 text-xs font-medium ${
          connectionState === 'connected'
            ? 'bg-accent-green/18 text-accent-green'
            : connectionState === 'connecting'
              ? 'bg-cyan-200/16 text-cyan-200'
              : 'bg-accent-red/18 text-accent-red'
        }`}>
          {connectionState}
        </span>
      </div>

      <div className="mt-5 grid gap-4 sm:grid-cols-2">
        <div>
          <p className="text-3xl font-semibold text-white">{formatYen(ticker?.last)}</p>
          <p className={`mt-2 text-sm ${deltaClass}`}>
            {delta === null ? '\u2014' : `${delta >= 0 ? '+' : ''}${formatYen(delta)}`}
          </p>
        </div>
        <div className="grid grid-cols-2 gap-3 text-sm">
          <Metric label="Best Ask" value={formatYen(ticker?.bestAsk)} />
          <Metric label="Best Bid" value={formatYen(ticker?.bestBid)} />
          <Metric label="Volume" value={ticker?.volume?.toLocaleString('ja-JP') ?? '\u2014'} />
          <Metric label="Updated" value={formatTime(ticker?.timestamp)} />
        </div>
      </div>
    </section>
  )
}

type MetricProps = {
  label: string
  value: string
}

function Metric({ label, value }: MetricProps) {
  return (
    <div className="rounded-2xl border border-white/6 bg-white/4 p-3">
      <p className="text-xs uppercase tracking-[0.18em] text-text-secondary">{label}</p>
      <p className="mt-2 font-medium text-white">{value}</p>
    </div>
  )
}
