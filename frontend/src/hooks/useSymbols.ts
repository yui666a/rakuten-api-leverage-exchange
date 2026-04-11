import { useQuery } from '@tanstack/react-query'
import { fetchApi, type TradableSymbol } from '../lib/api'

export function useSymbols() {
  return useQuery({
    queryKey: ['symbols'],
    queryFn: () => fetchApi<TradableSymbol[]>('/symbols'),
    staleTime: 5 * 60 * 1000,
  })
}
