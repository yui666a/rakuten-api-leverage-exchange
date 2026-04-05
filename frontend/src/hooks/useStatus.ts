import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type Status = {
  status: string
  tradingHalted: boolean
  balance: number
  dailyLoss: number
  totalPosition: number
}

export function useStatus() {
  return useQuery({
    queryKey: ['status'],
    queryFn: () => fetchApi<Status>('/status'),
    refetchInterval: 10_000,
  })
}
