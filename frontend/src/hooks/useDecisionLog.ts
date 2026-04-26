import { useQuery } from '@tanstack/react-query'
import { fetchApi, type DecisionLogResponse } from '../lib/api'

export function useDecisionLog(symbolId: number, limit = 200) {
  return useQuery({
    queryKey: ['decisions', symbolId, limit],
    queryFn: () => fetchApi<DecisionLogResponse>(`/decisions?symbolId=${symbolId}&limit=${limit}`),
    refetchInterval: 15_000,
  })
}
