import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type Strategy = {
  stance: string
  reasoning: string
}

export function useStrategy() {
  return useQuery({
    queryKey: ['strategy'],
    queryFn: () => fetchApi<Strategy>('/strategy'),
    refetchInterval: 30_000,
  })
}
