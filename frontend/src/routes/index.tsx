import { createFileRoute, Link } from '@tanstack/react-router'
import { AppFrame } from '../components/AppFrame'
import { KpiCard } from '../components/KpiCard'
import { CandlestickChart } from '../components/CandlestickChart'
import { IndicatorPanel } from '../components/IndicatorPanel'
import { PositionPanel } from '../components/PositionPanel'
import { BotControlCard } from '../components/BotControlCard'
import { useStatus } from '../hooks/useStatus'
import { usePnl } from '../hooks/usePnl'
import { useStrategy } from '../hooks/useStrategy'
import { useIndicators } from '../hooks/useIndicators'
import { useCandles } from '../hooks/useCandles'
import { usePositions } from '../hooks/usePositions'
import { useStartBot, useStopBot } from '../hooks/useBotControl'

export const Route = createFileRoute('/')({ component: Dashboard })

function Dashboard() {
  const { data: status } = useStatus()
  const { data: pnl } = usePnl()
  const { data: strategy } = useStrategy()
  const { data: indicators } = useIndicators(7)
  const { data: candles } = useCandles(7)
  const { data: positions } = usePositions(7)
  const startBot = useStartBot()
  const stopBot = useStopBot()

  const statusLabel = status?.tradingHalted
    ? 'リスク停止'
    : status?.manuallyStopped
      ? '手動停止'
      : status?.status === 'running'
        ? '稼働中'
        : '\u2014'

  return (
    <AppFrame
      title="Trading Dashboard"
      subtitle="KPI・戦略・ポジションを集約しつつ、Phase 2 の操作系を同じ導線に載せた監視画面です。"
    >
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <KpiCard
          label="残高"
          value={pnl ? `¥${pnl.balance.toLocaleString()}` : '\u2014'}
          color="text-accent-green"
        />
        <KpiCard
          label="日次損益"
          value={pnl ? `¥${(-pnl.dailyLoss).toLocaleString()}` : '\u2014'}
          color={pnl && pnl.dailyLoss > 0 ? 'text-accent-red' : 'text-accent-green'}
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
          <CandlestickChart candles={candles ?? []} />
          <div className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Strategy Insight</p>
                <h2 className="mt-2 text-xl font-semibold text-white">LLM reasoning</h2>
              </div>
              <Link to="/history" className="text-sm text-cyan-200 transition hover:text-cyan-100">
                履歴を見る
              </Link>
            </div>
            <p className="mt-4 text-sm leading-7 text-slate-300">
              {strategy?.reasoning ?? '戦略コメントはまだ生成されていません。'}
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
          <IndicatorPanel indicators={indicators} />
          <PositionPanel positions={positions} />
        </aside>
      </div>
    </AppFrame>
  )
}
