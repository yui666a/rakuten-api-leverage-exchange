import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  type BacktestCSVMeta,
  fetchApi,
  sendApi,
  type BacktestResult,
  type BacktestResultListResponse,
  type BacktestRunRequest,
} from '../lib/api'

// BacktestResultsFilter mirrors the query-parameter plumbing added to
// `GET /api/v1/backtest/results` (spec §5.3). Each field is optional; a
// missing or empty value means "no filter" — the hook strips them before
// building the URL so empty values do not reach the backend.
export type BacktestResultsFilter = {
  limit?: number
  offset?: number
  profileName?: string
  pdcaCycleId?: string
  hasParent?: boolean
  parentResultId?: string
}

function buildBacktestResultsURL(filter: BacktestResultsFilter): string {
  const params = new URLSearchParams()
  const { limit = 20, offset = 0 } = filter
  params.set('limit', String(limit))
  params.set('offset', String(offset))
  if (filter.profileName !== undefined && filter.profileName !== '') {
    params.set('profileName', filter.profileName)
  }
  if (filter.pdcaCycleId !== undefined && filter.pdcaCycleId !== '') {
    params.set('pdcaCycleId', filter.pdcaCycleId)
  }
  if (filter.parentResultId !== undefined && filter.parentResultId !== '') {
    params.set('parentResultId', filter.parentResultId)
  }
  if (filter.hasParent !== undefined) {
    params.set('hasParent', filter.hasParent ? 'true' : 'false')
  }
  return `/backtest/results?${params.toString()}`
}

export function useBacktestResults(filter: BacktestResultsFilter = {}) {
  const url = buildBacktestResultsURL(filter)
  return useQuery({
    // Include the full filter object in the key so distinct filter
    // combinations get their own cache entry and refetch on change.
    queryKey: ['backtest', 'results', filter] as const,
    queryFn: () => fetchApi<BacktestResultListResponse>(url),
    staleTime: 30_000,
  })
}

export function useBacktestResult(id: string) {
  return useQuery({
    queryKey: ['backtest', 'result', id],
    queryFn: () => fetchApi<BacktestResult>(`/backtest/results/${id}`),
    enabled: id !== '',
    staleTime: 60_000,
  })
}

export function useBacktestCSVMeta(dataPath: string) {
  const path = dataPath.trim()
  return useQuery({
    queryKey: ['backtest', 'csv-meta', path],
    queryFn: () =>
      fetchApi<BacktestCSVMeta>(`/backtest/csv-meta?data=${encodeURIComponent(path)}`),
    enabled: path !== '',
    staleTime: 60_000,
  })
}

export function useRunBacktest() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (request: BacktestRunRequest) =>
      sendApi<BacktestResult, BacktestRunRequest>('/backtest/run', 'POST', request),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ['backtest', 'results'] })
      queryClient.setQueryData(['backtest', 'result', result.id], result)
    },
  })
}
