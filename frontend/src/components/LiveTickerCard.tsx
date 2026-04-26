import type { LiveTicker, RealtimeOrderbook } from '../lib/api'

type LiveTickerCardProps = {
  ticker: LiveTicker | null
  // 楽天 venue は ticker payload を ~600ms 遅らせて配信するため、表示価格は
  // ほぼリアルタイム (p50≈10ms) で届く orderbook の midPrice を使う。
  // 約定が止まる低出来高シンボル (ETH/JPY など) でも板が動けば表示が更新される。
  orderbook: RealtimeOrderbook | null
  connectionState: 'connecting' | 'connected' | 'disconnected'
  currencyPair?: string
}

function formatYen(value: number | null | undefined) {
  if (value === null || value === undefined) return '—'
  return `¥${value.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}`
}

function formatTime(timestamp: number | null | undefined) {
  if (!timestamp) return '—'
  return new Date(timestamp).toLocaleTimeString('ja-JP')
}

const CONNECTION_LABEL: Record<'connecting' | 'connected' | 'disconnected', string> = {
  connecting: '接続中…',
  connected: '接続済み',
  disconnected: '切断',
}

export function LiveTickerCard({ ticker, orderbook, connectionState, currencyPair }: LiveTickerCardProps) {
  const mid = orderbook?.midPrice && orderbook.midPrice > 0 ? orderbook.midPrice : null
  const referencePrice = mid ?? ticker?.last ?? null
  const delta = referencePrice !== null && ticker ? referencePrice - ticker.open : null
  const deltaClass = delta !== null && delta < 0 ? 'text-accent-red' : 'text-accent-green'
  const pairLabel = currencyPair ?? 'BTC/JPY'
  const updatedAt = orderbook?.timestamp ?? ticker?.timestamp

  return (
    <section className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">リアルタイムティッカー</p>
          <h2 className="mt-2 text-xl font-semibold text-white">{pairLabel} 実勢価格</h2>
        </div>
        <span className={`rounded-full px-3 py-1 text-xs font-medium ${
          connectionState === 'connected'
            ? 'bg-accent-green/18 text-accent-green'
            : connectionState === 'connecting'
              ? 'bg-cyan-200/16 text-cyan-200'
              : 'bg-accent-red/18 text-accent-red'
        }`}>
          {CONNECTION_LABEL[connectionState]}
        </span>
      </div>

      <div className="mt-5 grid gap-4 sm:grid-cols-2">
        <div>
          <p className="text-3xl font-semibold text-white">{formatYen(referencePrice)}</p>
          <p className={`mt-2 text-sm ${deltaClass}`}>
            {delta === null ? '—' : `${delta >= 0 ? '+' : ''}${formatYen(delta)}`}
          </p>
          <p className="mt-1 text-[11px] text-text-secondary">
            Mid (板) / Last {formatYen(ticker?.last)}
          </p>
        </div>
        <div className="grid grid-cols-2 gap-3 text-sm">
          <Metric label="売気配" value={formatYen(ticker?.bestAsk)} />
          <Metric label="買気配" value={formatYen(ticker?.bestBid)} />
          <Metric label="出来高" value={ticker?.volume?.toLocaleString('ja-JP') ?? '—'} />
          <Metric label="更新時刻" value={formatTime(updatedAt)} />
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
