import { createFileRoute } from '@tanstack/react-router'
import { AppFrame } from '../components/AppFrame'
import { TradeHistoryTable } from '../components/TradeHistoryTable'
import { useMarketTickerStream } from '../hooks/useMarketTickerStream'
import { useTradeHistory } from '../hooks/useTradeHistory'

export const Route = createFileRoute('/history')({ component: HistoryPage })

function HistoryPage() {
  useMarketTickerStream(7)
  const { data: trades } = useTradeHistory(7)
  const safeTrades = trades ?? []
  const totalProfit = safeTrades.reduce((sum, trade) => sum + trade.profit, 0)

  return (
    <AppFrame
      title="Trade History"
      subtitle="楽天 private API の約定一覧を REST 経由で見せる Phase 2 画面です。最新損益の確認と異常検知を同じ導線に置きます。"
    >
      <div className="mb-4 grid gap-4 md:grid-cols-3">
        <SummaryCard label="約定件数" value={safeTrades.length.toLocaleString()} />
        <SummaryCard
          label="累計損益"
          value={`¥${totalProfit.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}`}
          color={totalProfit >= 0 ? 'text-accent-green' : 'text-accent-red'}
        />
        <SummaryCard
          label="最新更新"
          value={safeTrades[0] ? new Date(safeTrades[0].createdAt).toLocaleString('ja-JP') : '\u2014'}
        />
      </div>
      <TradeHistoryTable trades={safeTrades} />
    </AppFrame>
  )
}

type SummaryCardProps = {
  label: string
  value: string
  color?: string
}

function SummaryCard({ label, value, color = 'text-white' }: SummaryCardProps) {
  return (
    <section className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">{label}</p>
      <p className={`mt-3 text-xl font-semibold ${color}`}>{value}</p>
    </section>
  )
}
