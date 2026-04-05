import { useQuery } from '@tanstack/react-query'
import { fetchApi, type IndicatorSet } from '../lib/api'

export function useIndicators(symbolId: number) {
  return useQuery({
    queryKey: ['indicators', symbolId],
    queryFn: () => fetchApi<IndicatorSet>(`/indicators/${symbolId}`),
    refetchInterval: 30_000,
  })
}
