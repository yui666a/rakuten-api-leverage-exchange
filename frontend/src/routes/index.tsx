import { createFileRoute } from '@tanstack/react-router'
import { KpiCard } from '../components/KpiCard'
import { CandlestickChart } from '../components/CandlestickChart'
import { IndicatorPanel } from '../components/IndicatorPanel'
import { PositionPanel } from '../components/PositionPanel'
import { useStatus } from '../hooks/useStatus'
import { usePnl } from '../hooks/usePnl'
import { useStrategy } from '../hooks/useStrategy'
import { useIndicators } from '../hooks/useIndicators'
import { useCandles } from '../hooks/useCandles'

export const Route = createFileRoute('/')({ component: Dashboard })

function Dashboard() {
  const { data: status } = useStatus()
  const { data: pnl } = usePnl()
  const { data: strategy } = useStrategy()
  const { data: indicators } = useIndicators(7)
  const { data: candles } = useCandles(7)

  return (
    <main className="max-w-7xl mx-auto p-4">
      {/* KPI Cards */}
      <div className="grid grid-cols-4 gap-4 mb-4">
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
          color="text-accent-blue"
        />
        <KpiCard
          label="ステータス"
          value={status?.tradingHalted ? '停止中' : (status?.status ?? '\u2014')}
          color={status?.tradingHalted ? 'text-accent-red' : 'text-accent-green'}
        />
      </div>

      {/* Chart + Side Panel */}
      <div className="grid grid-cols-3 gap-4">
        <div className="col-span-2">
          <CandlestickChart candles={candles ?? []} />
        </div>
        <div className="space-y-4">
          <IndicatorPanel indicators={indicators} />
          <PositionPanel positions={undefined} />
        </div>
      </div>
    </main>
  )
}
