import { useQuery } from '@tanstack/react-query'
import { fetchApi, type Candle } from '../lib/api'

export function useCandles(symbolId: number) {
  return useQuery({
    queryKey: ['candles', symbolId],
    queryFn: () => fetchApi<Candle[]>(`/candles/${symbolId}`),
    refetchInterval: 60_000,
  })
}
