import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTradingConfig, useUpdateTradingConfig } from '../hooks/useTradingConfig'
import { useSymbols } from '../hooks/useSymbols'
import type { TradableSymbol } from '../lib/api'

// 両 API が失敗した極端ケースのみ使う最終退避値
const FALLBACK_SYMBOL_ID = 7

type SymbolContextValue = {
  symbolId: number
  symbols: TradableSymbol[]
  currentSymbol: TradableSymbol | undefined
  switchSymbol: (symbolId: number) => void
  isSwitching: boolean
  // tradingConfig がロード完了するまで false。
  // 未ロード時に fallback tradeAmount で PUT して誤上書きするのを防ぐため、
  // SymbolSelector 側で disabled に反映する。
  isSwitchAllowed: boolean
}

const SymbolContext = createContext<SymbolContextValue | null>(null)

function pickFirstTradableId(symbols: TradableSymbol[] | undefined): number | null {
  if (!symbols || symbols.length === 0) return null
  const found = symbols.find((s) => s.enabled && !s.viewOnly && !s.closeOnly)
  return found?.id ?? null
}

export function SymbolProvider({ children }: { children: ReactNode }) {
  const {
    data: tradingConfig,
    isLoading: isConfigLoading,
    isError: isConfigError,
  } = useTradingConfig()
  const { data: symbols, isLoading: isSymbolsLoading } = useSymbols()
  const updateConfig = useUpdateTradingConfig()
  const queryClient = useQueryClient()

  const [symbolId, setSymbolId] = useState<number | null>(null)

  // 初期化:
  //   1. tradingConfig 取得成功 → その値を使う
  //   2. tradingConfig 失敗 + symbols あり → symbols から最初の有効銘柄
  //   3. 両方失敗 → FALLBACK_SYMBOL_ID
  useEffect(() => {
    if (symbolId !== null) return
    if (tradingConfig) {
      setSymbolId(tradingConfig.symbolId)
      return
    }
    if (isConfigError) {
      const fallbackFromSymbols = pickFirstTradableId(symbols)
      if (fallbackFromSymbols !== null) {
        setSymbolId(fallbackFromSymbols)
      } else if (!isSymbolsLoading) {
        setSymbolId(FALLBACK_SYMBOL_ID)
      }
    }
  }, [tradingConfig, isConfigError, symbols, isSymbolsLoading, symbolId])

  // tradingConfig がロード完了している時だけ switchSymbol を許可。
  // 未ロード時に fallback tradeAmount で PUT して誤上書きするのを防ぐ。
  const isSwitchAllowed = tradingConfig !== undefined

  const switchSymbol = useCallback(
    (newSymbolId: number) => {
      if (!tradingConfig) return
      const prevSymbolId = symbolId
      setSymbolId(newSymbolId)
      updateConfig.mutate(
        { symbolId: newSymbolId, tradeAmount: tradingConfig.tradeAmount },
        {
          onSuccess: () => {
            void queryClient.invalidateQueries({ queryKey: ['candles'] })
            void queryClient.invalidateQueries({ queryKey: ['indicators'] })
            void queryClient.invalidateQueries({ queryKey: ['positions'] })
            void queryClient.invalidateQueries({ queryKey: ['trades'] })
            void queryClient.invalidateQueries({ queryKey: ['status'] })
            void queryClient.invalidateQueries({ queryKey: ['pnl'] })
          },
          onError: () => {
            setSymbolId(prevSymbolId)
          },
        },
      )
    },
    [symbolId, tradingConfig, updateConfig, queryClient],
  )

  if (symbolId === null) {
    if (isConfigLoading || isSymbolsLoading) {
      return (
        <div className="flex min-h-screen items-center justify-center">
          <p className="text-sm text-text-secondary">Loading...</p>
        </div>
      )
    }
    return (
      <div className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-text-secondary">Loading...</p>
      </div>
    )
  }

  const safeSymbols = symbols ?? []
  const currentSymbol = safeSymbols.find((s) => s.id === symbolId)

  return (
    <SymbolContext.Provider
      value={{
        symbolId,
        symbols: safeSymbols,
        currentSymbol,
        switchSymbol,
        isSwitching: updateConfig.isPending,
        isSwitchAllowed,
      }}
    >
      {children}
    </SymbolContext.Provider>
  )
}

export function useSymbolContext() {
  const ctx = useContext(SymbolContext)
  if (!ctx) throw new Error('useSymbolContext must be used within SymbolProvider')
  return ctx
}
