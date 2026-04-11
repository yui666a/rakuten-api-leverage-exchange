import { useSymbolContext } from '../contexts/SymbolContext'

export function SymbolSelector() {
  const { symbolId, symbols, switchSymbol, isSwitching, isSwitchAllowed } = useSymbolContext()

  const tradableSymbols = symbols.filter((s) => s.enabled && !s.viewOnly && !s.closeOnly)

  if (tradableSymbols.length === 0) {
    return null
  }

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
        {tradableSymbols.map((s) => (
          <option key={s.id} value={s.id} className="bg-bg-card text-white">
            {s.currencyPair.replace('_', '/')}
          </option>
        ))}
      </select>
      {isSwitching && <span className="text-xs text-cyan-200">切替中...</span>}
    </div>
  )
}
