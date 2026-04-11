import { useQuery } from '@tanstack/react-query'
import { fetchApi, type AllTradesResponse } from '../lib/api'

export function useAllTrades() {
  return useQuery({
    queryKey: ['trades', 'all'],
    queryFn: () => fetchApi<AllTradesResponse>('/trades/all'),
    refetchInterval: 15_000,
  })
}
