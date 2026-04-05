import { useQuery } from '@tanstack/react-query'
import { fetchApi, type Position } from '../lib/api'

export function usePositions(symbolId: number) {
  return useQuery({
    queryKey: ['positions', symbolId],
    queryFn: () => fetchApi<Position[]>(`/positions?symbolId=${symbolId}`),
    refetchInterval: 10_000,
  })
}
