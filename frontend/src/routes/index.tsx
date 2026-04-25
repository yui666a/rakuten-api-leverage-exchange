import { createFileRoute, Link, useSearch } from '@tanstack/react-router'
import { AppFrame } from '../components/AppFrame'
import { KpiCard } from '../components/KpiCard'
import { CandlestickChart } from '../components/CandlestickChart'
import { IndicatorPanel } from '../components/IndicatorPanel'
import { PositionPanel } from '../components/PositionPanel'
import { BotControlCard } from '../components/BotControlCard'
import { LiveTickerCard } from '../components/LiveTickerCard'
import { ManualTradeCard } from '../components/ManualTradeCard'
import { OrderbookPanel } from '../components/OrderbookPanel'
import { useStatus } from '../hooks/useStatus'
import { usePnl } from '../hooks/usePnl'
import { useStrategy } from '../hooks/useStrategy'
import { useIndicators } from '../hooks/useIndicators'
import { usePositions } from '../hooks/usePositions'
import { useStartBot, useStopBot } from '../hooks/useBotControl'
import { useMarketTickerStream } from '../hooks/useMarketTickerStream'
import { useSymbolContext } from '../contexts/SymbolContext'

export const Route = createFileRoute('/')({ component: Dashboard })

function Dashboard() {
  const { symbolId, currentSymbol } = useSymbolContext()
  const rootSearch = useSearch({ from: '__root__' }) as { symbol?: string }
  const { data: status } = useStatus()
  const { data: pnl } = usePnl()
  const { data: strategy } = useStrategy()
  const { data: indicators } = useIndicators(symbolId)
  const { data: positions } = usePositions(symbolId)
  const startBot = useStartBot()
  const stopBot = useStopBot()
  const { ticker, orderbook, connectionState } = useMarketTickerStream(symbolId)

  const statusLabel = status?.tradingHalted
    ? 'リスク停止'
    : status?.manuallyStopped
      ? '手動停止'
      : status?.status === 'running'
        ? '稼働中'
        : '\u2014'

  const dailyPnlTotal = pnl?.dailyPnl?.total ?? null
  const dailyPnlStale = pnl?.dailyPnl?.stale ?? false
  const dailyPnlLabel =
    dailyPnlTotal === null
      ? '\u2014'
      : `${dailyPnlTotal < 0 ? '-' : ''}¥${Math.abs(dailyPnlTotal).toLocaleString()}${dailyPnlStale ? '*' : ''}`

  const reasoningLabel = strategy?.reasoning
    ? strategy.reasoning === 'insufficient indicator data'
      ? '指標データが不足しています'
      : strategy.reasoning
    : '戦略コメントはまだ生成されていません。'

  return (
    <AppFrame
      title="トレーディングダッシュボード"
      subtitle="KPI・戦略・ポジション・操作系を集約した監視画面です。"
    >
      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        <KpiCard
          label="残高"
          value={pnl ? `¥${pnl.balance.toLocaleString()}` : '\u2014'}
          color="text-accent-green"
        />
        <KpiCard
          label="日次損益"
          value={dailyPnlLabel}
          color={dailyPnlTotal !== null && dailyPnlTotal < 0 ? 'text-accent-red' : 'text-accent-green'}
        />
        <KpiCard
          label="戦略方針"
          value={strategy?.stance ?? '\u2014'}
          color="text-cyan-200"
        />
        <KpiCard
          label="ステータス"
          value={statusLabel}
          color={status?.tradingHalted || status?.manuallyStopped ? 'text-accent-red' : 'text-accent-green'}
        />
      </div>

      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(320px,1fr)]">
        <section className="space-y-4">
          <LiveTickerCard
            ticker={ticker}
            connectionState={connectionState}
            currencyPair={currentSymbol?.currencyPair?.replace('_', '/')}
          />
          <CandlestickChart symbolId={symbolId} />
          <div className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">戦略インサイト</p>
                <h2 className="mt-2 text-xl font-semibold text-white">LLM判断理由</h2>
              </div>
              <Link
                to="/history"
                search={rootSearch}
                className="text-sm text-cyan-200 transition hover:text-cyan-100"
              >
                履歴を見る
              </Link>
            </div>
            <p className="mt-4 text-sm leading-7 text-slate-300">
              {reasoningLabel}
            </p>
          </div>
        </section>

        <aside className="space-y-4">
          <BotControlCard
            status={status}
            onStart={() => startBot.mutate()}
            onStop={() => stopBot.mutate()}
            isPending={startBot.isPending || stopBot.isPending}
          />
          <OrderbookPanel
            orderbook={orderbook}
            currencyPair={currentSymbol?.currencyPair}
          />
          <ManualTradeCard
            symbolId={symbolId}
            currencyPair={currentSymbol?.currencyPair}
            lotStep={currentSymbol?.baseStepAmount}
            minLot={currentSymbol?.minOrderAmount}
          />
          <IndicatorPanel indicators={indicators} />
          <PositionPanel positions={positions} />
        </aside>
      </div>
    </AppFrame>
  )
}
