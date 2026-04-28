import { useState } from 'react'
import type { Candle } from '../lib/api'
import { ADXChart } from './ADXChart'
import { MACDChart } from './MACDChart'
import { RSIChart } from './RSIChart'
import { StochasticsChart } from './StochasticsChart'

type Props = {
  candles: Candle[]
}

type PanelKey = 'macd' | 'rsi' | 'stoch' | 'adx'

const PANELS: { key: PanelKey; label: string }[] = [
  { key: 'macd', label: 'MACD' },
  { key: 'rsi', label: 'RSI' },
  { key: 'stoch', label: 'Stochastics' },
  { key: 'adx', label: 'ADX' },
]

export function IndicatorSubPanels({ candles }: Props) {
  const [active, setActive] = useState<PanelKey>('macd')
  if (candles.length === 0) return null
  return (
    <div className="mt-3 rounded-3xl border border-white/8 bg-bg-card/90 p-3 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <div className="mb-2 flex flex-wrap gap-2 px-1">
        {PANELS.map((p) => (
          <button
            key={p.key}
            type="button"
            onClick={() => setActive(p.key)}
            className={`rounded-full border px-3 py-1 text-xs font-medium transition ${
              active === p.key
                ? 'border-white/20 bg-white/10 text-white'
                : 'border-white/8 bg-transparent text-text-secondary hover:text-white'
            }`}
          >
            {p.label}
          </button>
        ))}
      </div>
      {active === 'macd' && <MACDChart candles={candles} />}
      {active === 'rsi' && <RSIChart candles={candles} />}
      {active === 'stoch' && <StochasticsChart candles={candles} />}
      {active === 'adx' && <ADXChart candles={candles} />}
    </div>
  )
}
