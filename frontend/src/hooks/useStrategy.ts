import { useQuery } from '@tanstack/react-query'
import { fetchApi, type StrategyResponse } from '../lib/api'

export function useStrategy() {
  return useQuery({
    queryKey: ['strategy'],
    queryFn: () => fetchApi<StrategyResponse>('/strategy'),
    refetchInterval: 30_000,
  })
}
