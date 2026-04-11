import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchApi, sendApi, type TradingConfig } from '../lib/api'

export function useTradingConfig() {
  return useQuery({
    queryKey: ['trading-config'],
    queryFn: () => fetchApi<TradingConfig>('/trading-config'),
  })
}

export function useUpdateTradingConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: TradingConfig) =>
      sendApi<TradingConfig, TradingConfig>('/trading-config', 'PUT', config),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['trading-config'] })
    },
  })
}
