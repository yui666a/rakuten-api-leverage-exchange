import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type Candle = {
  open: number
  high: number
  low: number
  close: number
  volume: number
  time: number
}

export function useCandles(symbolId: number) {
  return useQuery({
    queryKey: ['candles', symbolId],
    queryFn: () => fetchApi<Candle[]>(`/candles/${symbolId}`),
    refetchInterval: 60_000,
  })
}
