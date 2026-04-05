import { useQuery } from '@tanstack/react-query'
import { fetchApi, type PnlResponse } from '../lib/api'

export function usePnl() {
  return useQuery({
    queryKey: ['pnl'],
    queryFn: () => fetchApi<PnlResponse>('/pnl'),
    refetchInterval: 10_000,
  })
}
