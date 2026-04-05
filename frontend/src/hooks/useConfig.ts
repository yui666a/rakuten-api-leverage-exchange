import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchApi, sendApi, type RiskConfig } from '../lib/api'

export function useConfig() {
  return useQuery({
    queryKey: ['config'],
    queryFn: () => fetchApi<RiskConfig>('/config'),
  })
}

export function useUpdateConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: RiskConfig) => sendApi<RiskConfig, RiskConfig>('/config', 'PUT', config),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['config'] })
      void queryClient.invalidateQueries({ queryKey: ['status'] })
      void queryClient.invalidateQueries({ queryKey: ['pnl'] })
    },
  })
}
