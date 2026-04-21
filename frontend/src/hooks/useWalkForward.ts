import { useQuery } from '@tanstack/react-query'
import {
  fetchApi,
  type WalkForwardEnvelopeResponse,
  type WalkForwardListResponse,
} from '../lib/api'

export type WalkForwardFilter = {
  limit?: number
  offset?: number
  baseProfile?: string
  pdcaCycleId?: string
}

function buildURL(filter: WalkForwardFilter): string {
  const params = new URLSearchParams()
  const { limit = 20, offset = 0 } = filter
  params.set('limit', String(limit))
  params.set('offset', String(offset))
  if (filter.baseProfile) params.set('baseProfile', filter.baseProfile)
  if (filter.pdcaCycleId) params.set('pdcaCycleId', filter.pdcaCycleId)
  return `/backtest/walk-forward?${params.toString()}`
}

// useWalkForwardResults fetches the envelope list. Per-window BacktestResult
// bodies are already embedded in the list response (the BE omits only the
// per-window `ResultJSON` payload, which here arrives as `result` — but we
// use the detail endpoint for the full view anyway).
export function useWalkForwardResults(filter: WalkForwardFilter = {}) {
  return useQuery({
    queryKey: ['backtest', 'walk-forward', filter] as const,
    queryFn: () => fetchApi<WalkForwardListResponse>(buildURL(filter)),
    staleTime: 30_000,
  })
}

export function useWalkForwardResult(id: string) {
  return useQuery({
    queryKey: ['backtest', 'walk-forward', id],
    queryFn: () => fetchApi<WalkForwardEnvelopeResponse>(`/backtest/walk-forward/${id}`),
    enabled: id !== '',
    staleTime: 60_000,
  })
}
