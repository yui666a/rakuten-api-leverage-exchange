import type { IndicatorSet } from '../lib/api'

type IndicatorPanelProps = {
  indicators: IndicatorSet | undefined
}

function formatNum(v: number | null, decimals = 0): string {
  if (v === null || v === undefined) return '\u2014'
  return v.toLocaleString('ja-JP', { maximumFractionDigits: decimals })
}

export function IndicatorPanel({ indicators }: IndicatorPanelProps) {
  if (!indicators) {
    return (
      <div className="bg-bg-card rounded-lg p-4">
        <div className="text-text-secondary text-xs mb-2">テクニカル指標</div>
        <div className="text-text-secondary text-sm">読み込み中...</div>
      </div>
    )
  }

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="text-text-secondary text-xs mb-3">テクニカル指標</div>
      <div className="space-y-2 text-sm">
        <div className="flex justify-between">
          <span className="text-text-secondary">RSI(14)</span>
          <span>{formatNum(indicators.rsi14, 1)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">SMA(20)</span>
          <span>{formatNum(indicators.sma20)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">SMA(50)</span>
          <span>{formatNum(indicators.sma50)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">EMA(12)</span>
          <span>{formatNum(indicators.ema12)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">EMA(26)</span>
          <span>{formatNum(indicators.ema26)}</span>
        </div>
        <div className="border-t border-bg-card-hover my-2" />
        <div className="flex justify-between">
          <span className="text-text-secondary">MACD</span>
          <span>{formatNum(indicators.macdLine, 2)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">Signal</span>
          <span>{formatNum(indicators.signalLine, 2)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">Histogram</span>
          <span>{formatNum(indicators.histogram, 2)}</span>
        </div>
      </div>
    </div>
  )
}
