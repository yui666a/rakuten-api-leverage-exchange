import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  type ReactNode,
} from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
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

function pickFirstTradable(symbols: TradableSymbol[] | undefined): TradableSymbol | null {
  if (!symbols || symbols.length === 0) return null
  return symbols.find((s) => s.enabled && !s.viewOnly && !s.closeOnly) ?? null
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

  const search = useSearch({ from: '__root__' }) as { symbol?: string }
  const navigate = useNavigate()
  const urlSymbol = search.symbol

  // URL の symbol → symbols の currencyPair と一致するエントリを探す。
  // URL が source of truth なので、ここで導出した symbolId が全画面に流れる。
  const resolvedFromUrl = useMemo<TradableSymbol | null>(() => {
    if (!urlSymbol || !symbols) return null
    return symbols.find((s) => s.currencyPair === urlSymbol) ?? null
  }, [urlSymbol, symbols])

  // URL に symbol が無い/不正な場合のデフォルト解決:
  //   1. tradingConfig 取得成功 → その symbolId の currencyPair
  //   2. tradingConfig 失敗 + symbols あり → symbols から最初の有効銘柄
  //   3. 両方失敗 → FALLBACK_SYMBOL_ID の currencyPair（symbols から引けなければ null）
  const defaultSymbol = useMemo<TradableSymbol | null>(() => {
    if (!symbols) return null
    if (tradingConfig) {
      const match = symbols.find((s) => s.id === tradingConfig.symbolId)
      if (match) return match
    }
    if (isConfigError || tradingConfig) {
      const first = pickFirstTradable(symbols)
      if (first) return first
    }
    return symbols.find((s) => s.id === FALLBACK_SYMBOL_ID) ?? null
  }, [tradingConfig, isConfigError, symbols])

  // URL の symbol が無い/不正な場合、デフォルトを書き戻す。
  // replace: true で履歴に残さない。
  useEffect(() => {
    if (resolvedFromUrl) return
    if (!defaultSymbol) return
    // tradingConfig がロード中は待つ。確定したデフォルトだけを URL に書く。
    if (isConfigLoading) return
    void navigate({
      to: '.',
      search: (prev) => ({ ...prev, symbol: defaultSymbol.currencyPair }),
      replace: true,
    })
  }, [resolvedFromUrl, defaultSymbol, isConfigLoading, navigate])

  // tradingConfig がロード完了している時だけ switchSymbol を許可。
  // 未ロード時に fallback tradeAmount で PUT して誤上書きするのを防ぐ。
  const isSwitchAllowed = tradingConfig !== undefined

  // Defense-in-depth (D): if URL says LTC but the backend's pipeline still
  // points at BTC (e.g. fresh container, persistence not yet caught up), the
  // WS subscription would silently stay on the wrong asset and the dashboard
  // would freeze on stale ticks. Auto-PUT once on mount to close the gap.
  // We only fire when:
  //   - both URL and backend config are settled (no race during boot)
  //   - the symbols differ
  //   - no PUT is already in-flight (avoid loops on transient errors)
  //
  // The deps array intentionally only depends on the resolved IDs (primitive
  // values) — putting the mutation object or queryClient in deps would
  // re-fire this effect on every mutation status change and re-PUT the same
  // value, which silently restarted the EventDrivenPipeline event loop and
  // wiped the decision recorder's pending bar. The mutate / invalidate calls
  // are accessed via the closure but their identities don't drive the effect.
  const urlResolvedId = resolvedFromUrl?.id
  const backendSymbolId = tradingConfig?.symbolId
  const backendTradeAmount = tradingConfig?.tradeAmount
  useEffect(() => {
    if (urlResolvedId === undefined || backendSymbolId === undefined) return
    if (updateConfig.isPending) return
    if (urlResolvedId === backendSymbolId) return
    console.warn(
      '[SymbolContext] mismatch between URL symbol and backend trading_config; auto-syncing',
      { urlSymbolId: urlResolvedId, backendSymbolId },
    )
    updateConfig.mutate(
      { symbolId: urlResolvedId, tradeAmount: backendTradeAmount ?? 0 },
      {
        onSuccess: () => {
          void queryClient.invalidateQueries({ queryKey: ['candles'] })
          void queryClient.invalidateQueries({ queryKey: ['indicators'] })
        },
      },
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlResolvedId, backendSymbolId])

  const switchSymbol = useCallback(
    (newSymbolId: number) => {
      if (!tradingConfig) return
      if (!symbols) return
      const next = symbols.find((s) => s.id === newSymbolId)
      if (!next) return
      const prevSymbol = urlSymbol
      void navigate({
        to: '.',
        search: (prev) => ({ ...prev, symbol: next.currencyPair }),
      })
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
            void navigate({
              to: '.',
              search: (prev) => ({ ...prev, symbol: prevSymbol }),
            })
          },
        },
      )
    },
    [urlSymbol, symbols, tradingConfig, updateConfig, queryClient, navigate],
  )

  if (!resolvedFromUrl) {
    if (isConfigLoading || isSymbolsLoading || !symbols) {
      return (
        <div className="flex min-h-screen items-center justify-center">
          <p className="text-sm text-text-secondary">Loading...</p>
        </div>
      )
    }
    // symbols はあるがまだ URL へ書き戻し中 → 次のレンダリングで resolvedFromUrl が埋まる。
    return (
      <div className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-text-secondary">Loading...</p>
      </div>
    )
  }

  const currentSymbol = resolvedFromUrl
  const symbolId = resolvedFromUrl.id

  return (
    <SymbolContext.Provider
      value={{
        symbolId,
        symbols: symbols ?? [],
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
