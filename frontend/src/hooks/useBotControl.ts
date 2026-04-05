import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sendApi, type BotControlResponse } from '../lib/api'

function useBotMutation(path: '/start' | '/stop') {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => sendApi<BotControlResponse>(path, 'POST'),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['status'] })
      void queryClient.invalidateQueries({ queryKey: ['pnl'] })
    },
  })
}

export function useStartBot() {
  return useBotMutation('/start')
}

export function useStopBot() {
  return useBotMutation('/stop')
}
