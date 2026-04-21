import { useQuery } from '@tanstack/react-query'
import {
  fetchApi,
  type MultiPeriodResult,
  type MultiPeriodResultListResponse,
} from '../lib/api'

export type MultiPeriodResultsFilter = {
  limit?: number
  offset?: number
  profileName?: string
  pdcaCycleId?: string
}

function buildURL(filter: MultiPeriodResultsFilter): string {
  const params = new URLSearchParams()
  const { limit = 20, offset = 0 } = filter
  params.set('limit', String(limit))
  params.set('offset', String(offset))
  if (filter.profileName) params.set('profileName', filter.profileName)
  if (filter.pdcaCycleId) params.set('pdcaCycleId', filter.pdcaCycleId)
  return `/backtest/multi-results?${params.toString()}`
}

// useMultiPeriodResults fetches the envelope list from
// GET /backtest/multi-results. Per-period BacktestResult bodies are NOT
// populated in this response — callers that need them must issue
// useMultiPeriodResult(id) for the full rehydrated payload.
export function useMultiPeriodResults(filter: MultiPeriodResultsFilter = {}) {
  return useQuery({
    queryKey: ['backtest', 'multi-results', filter] as const,
    queryFn: () => fetchApi<MultiPeriodResultListResponse>(buildURL(filter)),
    staleTime: 30_000,
  })
}

export function useMultiPeriodResult(id: string) {
  return useQuery({
    queryKey: ['backtest', 'multi-result', id],
    queryFn: () => fetchApi<MultiPeriodResult>(`/backtest/multi-results/${id}`),
    enabled: id !== '',
    staleTime: 60_000,
  })
}
