import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  type BacktestCSVMeta,
  fetchApi,
  sendApi,
  type BacktestResult,
  type BacktestResultListResponse,
  type BacktestRunRequest,
} from '../lib/api'

export function useBacktestResults(limit = 20, offset = 0) {
  return useQuery({
    queryKey: ['backtest', 'results', limit, offset],
    queryFn: () =>
      fetchApi<BacktestResultListResponse>(
        `/backtest/results?limit=${limit}&offset=${offset}`,
      ),
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
