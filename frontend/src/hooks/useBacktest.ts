import { useQuery } from '@tanstack/react-query'
import { fetchApi, type BacktestResult, type BacktestResultListResponse } from '../lib/api'

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
