import { useMemo } from 'react'
import type { RealtimeOrderbook } from '../lib/api'

type OrderbookPanelProps = {
  orderbook: RealtimeOrderbook | null
  currencyPair?: string
}

const ROWS = 12

export function OrderbookPanel({ orderbook, currencyPair }: OrderbookPanelProps) {
  const ask = useMemo(() => buildSide(orderbook?.asks, 'asc', ROWS), [orderbook?.asks])
  const bid = useMemo(() => buildSide(orderbook?.bids, 'desc', ROWS), [orderbook?.bids])

  const askMaxTotal = ask.reduce((m, r) => Math.max(m, r.cumulative), 0)
  const bidMaxTotal = bid.reduce((m, r) => Math.max(m, r.cumulative), 0)
  const sideMax = Math.max(askMaxTotal, bidMaxTotal, 1)

  const spread = orderbook ? orderbook.bestAsk - orderbook.bestBid : 0
  const spreadPct =
    orderbook && orderbook.midPrice > 0 ? (spread / orderbook.midPrice) * 100 : 0

  return (
    <div className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Order Book</p>
          <h2 className="mt-2 text-xl font-semibold text-white">板情報</h2>
        </div>
        <span className="rounded-full bg-white/8 px-2.5 py-1 font-mono text-[11px] text-slate-300">
          {currencyPair?.replace('_', '/') ?? '—'}
        </span>
      </div>

      <div className="mt-4 grid grid-cols-3 gap-2 px-1 text-[10px] uppercase tracking-wider text-text-secondary">
        <span>価格 (JPY)</span>
        <span className="text-right">数量</span>
        <span className="text-right">累計 (JPY)</span>
      </div>

      <div className="mt-1 space-y-px">
        {ask.map((row, idx) => (
          <Row key={`ask-${idx}`} row={row} sideMax={sideMax} side="ask" />
        ))}
      </div>

      <MidBar orderbook={orderbook} spread={spread} spreadPct={spreadPct} />

      <div className="space-y-px">
        {bid.map((row, idx) => (
          <Row key={`bid-${idx}`} row={row} sideMax={sideMax} side="bid" />
        ))}
      </div>

      {!orderbook && (
        <p className="mt-3 text-[11px] text-text-secondary">板を待機中…</p>
      )}
    </div>
  )
}

type SideRow = {
  price: number
  amount: number
  total: number       // 価格 × 数量
  cumulative: number  // ベスト側からの累計 total
  filler: boolean
}

function buildSide(
  raw: Array<{ price: number; amount: number }> | undefined,
  // 並び順 (asc=安い→高い、desc=高い→安い)。Ask は ベスト Ask が下に来るよう
  // asc を渡し、表示前に逆順にして上から順に並べると、Bid と隣接する中央線
  // 直下にベスト Ask が、その上に薄い段がくる楽天と同じレイアウトになる。
  order: 'asc' | 'desc',
  rows: number,
): SideRow[] {
  const arr = (raw ?? []).slice().sort((a, b) => (order === 'asc' ? a.price - b.price : b.price - a.price))
  const top = arr.slice(0, rows)

  // ベスト価格 (asc/desc どちらでも先頭) からの累計
  let cum = 0
  const built: SideRow[] = top.map((r) => {
    const total = r.price * r.amount
    cum += total
    return {
      price: r.price,
      amount: r.amount,
      total,
      cumulative: cum,
      filler: false,
    }
  })

  // 表示は Ask=ベストが下、Bid=ベストが上
  if (order === 'asc') built.reverse()

  // 行数が足りない時は空行で埋めて固定高を保つ
  while (built.length < rows) {
    if (order === 'asc') built.unshift({ price: 0, amount: 0, total: 0, cumulative: 0, filler: true })
    else built.push({ price: 0, amount: 0, total: 0, cumulative: 0, filler: true })
  }

  return built
}

function Row({
  row,
  sideMax,
  side,
}: {
  row: SideRow
  sideMax: number
  side: 'ask' | 'bid'
}) {
  if (row.filler) {
    return <div className="h-[18px]" />
  }
  const widthPct = Math.min(100, (row.cumulative / sideMax) * 100)
  const barClass = side === 'ask' ? 'bg-accent-red/15' : 'bg-accent-green/15'
  const priceClass = side === 'ask' ? 'text-accent-red' : 'text-accent-green'
  return (
    <div className="relative grid h-[18px] grid-cols-3 items-center px-1 font-mono text-[11px] tabular-nums">
      <div
        className={`pointer-events-none absolute inset-y-0 right-0 rounded-sm ${barClass}`}
        style={{ width: `${widthPct}%` }}
      />
      <span className={`relative z-10 ${priceClass}`}>{formatPrice(row.price)}</span>
      <span className="relative z-10 text-right text-slate-200">{formatAmount(row.amount)}</span>
      <span className="relative z-10 text-right text-slate-300">{formatTotal(row.cumulative)}</span>
    </div>
  )
}

function MidBar({
  orderbook,
  spread,
  spreadPct,
}: {
  orderbook: RealtimeOrderbook | null
  spread: number
  spreadPct: number
}) {
  if (!orderbook) {
    return (
      <div className="my-2 flex items-center justify-between rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-[11px] text-text-secondary">
        <span>Mid —</span>
        <span>Spread —</span>
      </div>
    )
  }
  return (
    <div className="my-2 flex items-center justify-between rounded-xl border border-white/10 bg-white/5 px-3 py-2 font-mono text-[12px] tabular-nums">
      <div>
        <span className="text-[10px] uppercase tracking-wider text-text-secondary">Mid </span>
        <span className="text-white">{formatPrice(orderbook.midPrice)}</span>
      </div>
      <div className="text-right">
        <span className="text-[10px] uppercase tracking-wider text-text-secondary">Spread </span>
        <span className="text-white">{formatPrice(spread)}</span>
        <span className="ml-1 text-text-secondary">({spreadPct.toFixed(3)}%)</span>
      </div>
    </div>
  )
}

function formatPrice(value: number): string {
  if (!Number.isFinite(value) || value === 0) return '—'
  return value.toLocaleString('ja-JP', { maximumFractionDigits: 1 })
}

function formatAmount(value: number): string {
  if (!Number.isFinite(value)) return '—'
  if (value === 0) return '0'
  if (value >= 100) return value.toLocaleString('ja-JP', { maximumFractionDigits: 0 })
  return value.toFixed(2).replace(/\.?0+$/, '')
}

function formatTotal(value: number): string {
  if (!Number.isFinite(value) || value === 0) return '—'
  return Math.round(value).toLocaleString('ja-JP')
}
