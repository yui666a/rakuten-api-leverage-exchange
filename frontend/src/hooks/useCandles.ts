import { useQuery } from '@tanstack/react-query'
import { fetchApi, type Candle } from '../lib/api'

export type CandleInterval = 'PT1M' | 'PT5M' | 'PT15M' | 'PT1H' | 'P1D' | 'P1W'

export function useCandles(symbolId: number, interval: CandleInterval = 'PT15M') {
  return useQuery({
    queryKey: ['candles', symbolId, interval],
    queryFn: () => fetchApi<Candle[]>(`/candles/${symbolId}?interval=${interval}`),
    refetchInterval: 60_000,
  })
}
