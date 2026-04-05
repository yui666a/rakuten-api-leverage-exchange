import { useQuery } from '@tanstack/react-query'
import { fetchApi, type TradeHistoryItem } from '../lib/api'

export function useTradeHistory(symbolId: number) {
  return useQuery({
    queryKey: ['trades', symbolId],
    queryFn: () => fetchApi<TradeHistoryItem[]>(`/trades?symbolId=${symbolId}`),
    refetchInterval: 15_000,
  })
}
