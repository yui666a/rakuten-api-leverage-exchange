import { useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { AppFrame } from '../components/AppFrame'
import { DecisionLogTable } from '../components/DecisionLogTable'
import { TradeHistoryTable, type TradeHistoryRow } from '../components/TradeHistoryTable'
import { useMarketTickerStream } from '../hooks/useMarketTickerStream'
import { useTradeHistory } from '../hooks/useTradeHistory'
import { useAllTrades } from '../hooks/useAllTrades'
import { useDecisionLog } from '../hooks/useDecisionLog'
import { useSymbolContext } from '../contexts/SymbolContext'

export const Route = createFileRoute('/history')({ component: HistoryPage })

type TabKey = 'all' | 'single' | 'decisions'

function HistoryPage() {
  const { symbolId, currentSymbol } = useSymbolContext()
  useMarketTickerStream(symbolId)

  const [tab, setTab] = useState<TabKey>('all')

  const { data: singleTrades } = useTradeHistory(symbolId)
  const { data: allTradesData } = useAllTrades()
  const { data: decisionData } = useDecisionLog(symbolId)

  const rows = useMemo<TradeHistoryRow[]>(() => {
    if (tab === 'single') {
      const pair = currentSymbol?.currencyPair
      return (singleTrades ?? []).map((t) => ({ ...t, currencyPair: pair }))
    }
    const results = allTradesData?.results ?? []
    const flat: TradeHistoryRow[] = []
    for (const entry of results) {
      if (!entry.trades) continue
      for (const t of entry.trades) {
        flat.push({ ...t, currencyPair: entry.currencyPair })
      }
    }
    flat.sort((a, b) => b.createdAt - a.createdAt)
    return flat
  }, [tab, singleTrades, allTradesData, currentSymbol])

  const failedSymbols = useMemo(() => {
    if (tab !== 'all') return []
    return (allTradesData?.results ?? []).filter((e) => e.error)
  }, [tab, allTradesData])

  const totalProfit = rows.reduce((sum, trade) => sum + trade.profit, 0)
  const subtitle =
    tab === 'all'
      ? '楽天 private API から全通貨の約定をまとめて REST 経由で表示します。'
      : tab === 'single'
      ? `現在選択中の ${currentSymbol?.currencyPair ?? ''} に絞った約定一覧です。`
      : `${currentSymbol?.currencyPair ?? ''} の 15 分足ごとの売買判断ログです。`

  return (
    <AppFrame title="Trade History" subtitle={subtitle}>
      <div className="mb-4 flex gap-2">
        <TabButton active={tab === 'all'} onClick={() => setTab('all')}>
          全通貨の約定
        </TabButton>
        <TabButton active={tab === 'single'} onClick={() => setTab('single')}>
          {currentSymbol?.currencyPair ?? '個別通貨'}の約定
        </TabButton>
        <TabButton active={tab === 'decisions'} onClick={() => setTab('decisions')}>
          {currentSymbol?.currencyPair ?? '個別通貨'}の判断ログ
        </TabButton>
      </div>
      {tab !== 'decisions' && (
        <div className="mb-4 grid gap-4 md:grid-cols-3">
          <SummaryCard label="約定件数" value={rows.length.toLocaleString()} />
          <SummaryCard
            label="累計損益"
            value={`¥${totalProfit.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}`}
            color={totalProfit >= 0 ? 'text-accent-green' : 'text-accent-red'}
          />
          <SummaryCard
            label="最新更新"
            value={rows[0] ? new Date(rows[0].createdAt).toLocaleString('ja-JP') : '—'}
          />
        </div>
      )}
      {failedSymbols.length > 0 && (
        <div className="mb-4 rounded-2xl border border-accent-red/40 bg-accent-red/10 px-5 py-3 text-sm text-accent-red">
          一部通貨の取得に失敗しました: {failedSymbols.map((e) => e.currencyPair).join(', ')}
        </div>
      )}
      {tab === 'decisions' ? (
        <DecisionLogTable decisions={decisionData?.decisions ?? []} />
      ) : (
        <TradeHistoryTable trades={rows} showCurrencyPair={tab === 'all'} />
      )}
    </AppFrame>
  )
}

type TabButtonProps = {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}

function TabButton({ active, onClick, children }: TabButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-full border px-4 py-2 text-sm font-medium transition ${
        active
          ? 'border-white/20 bg-white/10 text-white'
          : 'border-white/8 bg-transparent text-text-secondary hover:text-white'
      }`}
    >
      {children}
    </button>
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
