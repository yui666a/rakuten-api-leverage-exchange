import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type IndicatorSet = {
  symbolId: number
  sma20: number | null
  sma50: number | null
  ema12: number | null
  ema26: number | null
  rsi14: number | null
  macdLine: number | null
  signalLine: number | null
  histogram: number | null
  timestamp: number
}

export function useIndicators(symbolId: number) {
  return useQuery({
    queryKey: ['indicators', symbolId],
    queryFn: () => fetchApi<IndicatorSet>(`/indicators/${symbolId}`),
    refetchInterval: 30_000,
  })
}
