import { useSymbolContext } from '../contexts/SymbolContext'
import { useStatus } from '../hooks/useStatus'
import { useAllTickers } from '../hooks/useAllTickers'
import type { TradableSymbol } from '../lib/api'

const LEVERAGE = 2

/** 最小注文に必要な証拠金を計算 */
function requiredMargin(symbol: TradableSymbol, lastPrice: number): number {
  return (symbol.minOrderAmount * lastPrice) / LEVERAGE
}

/** 円の表示フォーマット */
function formatJPY(value: number): string {
  if (value >= 10_000) {
    return `${(value / 10_000).toFixed(1)}万`
  }
  return `${Math.ceil(value).toLocaleString()}`
}

export function SymbolSelector() {
  const { symbolId, symbols, switchSymbol, isSwitching, isSwitchAllowed } = useSymbolContext()
  const { data: status } = useStatus()
  const priceMap = useAllTickers(symbols)

  const tradableSymbols = symbols.filter((s) => s.enabled && !s.viewOnly && !s.closeOnly)

  if (tradableSymbols.length === 0) {
    return null
  }

  const balance = status?.balance ?? 0
  const disabled = isSwitching || !isSwitchAllowed

  return (
    <div className="flex items-center gap-2">
      <label
        htmlFor="symbol-select"
        className="text-xs uppercase tracking-[0.18em] text-text-secondary"
      >
        銘柄
      </label>
      <select
        id="symbol-select"
        value={symbolId}
        onChange={(e) => switchSymbol(Number(e.target.value))}
        disabled={disabled}
        className="rounded-full border border-white/10 bg-white/6 px-4 py-2 text-sm font-medium text-white outline-none transition focus:border-cyan-200 disabled:opacity-50"
      >
        {tradableSymbols.map((s) => {
          const price = priceMap.get(s.id)
          const margin = price ? requiredMargin(s, price) : undefined
          const affordable = margin !== undefined && balance > 0 ? balance >= margin : true

          const label = buildLabel(s, margin)

          return (
            <option
              key={s.id}
              value={s.id}
              disabled={!affordable}
              className="bg-bg-card text-white"
            >
              {label}
            </option>
          )
        })}
      </select>
      {isSwitching && <span className="text-xs text-cyan-200">切替中...</span>}
    </div>
  )
}

function buildLabel(s: TradableSymbol, margin: number | undefined): string {
  const pair = s.currencyPair.replace('_', '/')
  const min = `${s.minOrderAmount} ${s.baseCurrency}`
  if (margin !== undefined) {
    return `${pair}（最小 ${min} / 証拠金 ¥${formatJPY(margin)}）`
  }
  return `${pair}（最小 ${min}）`
}
