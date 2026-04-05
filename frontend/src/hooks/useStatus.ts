import { useQuery } from '@tanstack/react-query'
import { fetchApi, type StatusResponse } from '../lib/api'

export function useStatus() {
  return useQuery({
    queryKey: ['status'],
    queryFn: () => fetchApi<StatusResponse>('/status'),
    refetchInterval: 10_000,
  })
}
