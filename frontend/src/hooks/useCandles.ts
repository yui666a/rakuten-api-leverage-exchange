import { useInfiniteQuery } from '@tanstack/react-query'
import { fetchApi, type Candle } from '../lib/api'

export type CandleInterval = 'PT1M' | 'PT5M' | 'PT15M' | 'PT1H' | 'P1D' | 'P1W'

const PAGE_SIZE = 500

export function useCandles(symbolId: number, interval: CandleInterval = 'PT15M') {
  return useInfiniteQuery({
    queryKey: ['candles', symbolId, interval],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams({ interval, limit: String(PAGE_SIZE) })
      if (pageParam) params.set('before', String(pageParam))
      const result = await fetchApi<Candle[] | null>(`/candles/${symbolId}?${params}`)
      return result ?? []
    },
    initialPageParam: 0 as number,
    getNextPageParam: (lastPage) => {
      if (!lastPage || lastPage.length === 0) return undefined
      // The oldest candle's time becomes the cursor for the next page
      return lastPage[0].time // data comes oldest→newest from the API
    },
    enabled: Number.isFinite(symbolId),
    refetchInterval: 60_000,
  })
}
