import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  closePosition,
  type ClosePositionResponse,
} from '../lib/api'

function generateClientOrderId(positionID: number): string {
  const t = Date.now()
  const r = Math.random().toString(36).slice(2, 8)
  return `manual-close-${positionID}-${t}-${r}`
}

type CloseInput = {
  positionId: number
  symbolId: number
}

export function useClosePosition() {
  const queryClient = useQueryClient()
  return useMutation<ClosePositionResponse, Error, CloseInput>({
    mutationFn: ({ positionId, symbolId }) =>
      closePosition(positionId, {
        symbolId,
        clientOrderId: generateClientOrderId(positionId),
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['positions'] })
      void queryClient.invalidateQueries({ queryKey: ['trades'] })
      void queryClient.invalidateQueries({ queryKey: ['pnl'] })
    },
  })
}
