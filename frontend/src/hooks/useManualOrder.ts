import { useMutation, useQueryClient } from '@tanstack/react-query'
import { createManualOrder, type ManualOrderRequest, type ManualOrderResponse } from '../lib/api'

// generateClientOrderId returns a per-click idempotency key. Backend uses this
// to dedupe accidental double-submits (already wired via clientOrderRepo).
function generateClientOrderId(side: string): string {
  const t = Date.now()
  const r = Math.random().toString(36).slice(2, 8)
  return `manual-${side.toLowerCase()}-${t}-${r}`
}

type ManualOrderInput = Omit<ManualOrderRequest, 'orderType' | 'clientOrderId'>

export function useManualOrder() {
  const queryClient = useQueryClient()
  return useMutation<ManualOrderResponse, Error, ManualOrderInput>({
    mutationFn: (input) =>
      createManualOrder({
        ...input,
        orderType: 'MARKET',
        clientOrderId: generateClientOrderId(input.side),
      }),
    onSuccess: () => {
      // The backend already pushes a trade_event over the realtime hub, but
      // the order state behind /trades and /positions takes a beat to settle
      // through the rakuten REST round-trip. Force-invalidate so the user
      // sees the new position/row even if the WS push lost the race.
      void queryClient.invalidateQueries({ queryKey: ['positions'] })
      void queryClient.invalidateQueries({ queryKey: ['trades'] })
      void queryClient.invalidateQueries({ queryKey: ['pnl'] })
    },
  })
}
