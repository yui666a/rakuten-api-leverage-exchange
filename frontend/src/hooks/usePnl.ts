import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type PnL = {
  balance: number
  dailyLoss: number
  totalPosition: number
  tradingHalted: boolean
}

export function usePnl() {
  return useQuery({
    queryKey: ['pnl'],
    queryFn: () => fetchApi<PnL>('/pnl'),
    refetchInterval: 10_000,
  })
}
