import { useQueries } from '@tanstack/react-query'
import { fetchApi, type TradableSymbol } from '../lib/api'

type TickerResponse = {
  symbolId: number
  last: number
  bestAsk: number
  bestBid: number
}

/**
 * 全シンボルのティッカーを一括取得する。
 * 各シンボルの現在価格を Map<symbolId, lastPrice> で返す。
 */
export function useAllTickers(symbols: TradableSymbol[] | undefined) {
  const tradable = symbols?.filter((s) => s.enabled && !s.viewOnly && !s.closeOnly) ?? []

  const results = useQueries({
    queries: tradable.map((s) => ({
      queryKey: ['ticker', s.id],
      queryFn: () => fetchApi<TickerResponse>(`/ticker?symbolId=${s.id}`),
      staleTime: 60_000,
      refetchInterval: 60_000,
    })),
  })

  const priceMap = new Map<number, number>()
  results.forEach((r, i) => {
    if (r.data) {
      priceMap.set(tradable[i].id, r.data.last)
    }
  })

  return priceMap
}
