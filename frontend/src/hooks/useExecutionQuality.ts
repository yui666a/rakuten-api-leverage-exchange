import { useQuery } from '@tanstack/react-query'
import { fetchExecutionQuality, type ExecutionQualityReport } from '../lib/api'

// 24h KPI fetched from GET /api/v1/execution-quality. Refreshes every 60 s
// because the underlying my-trades fetch is 1 venue call + a SQLite scan and
// we don't want to hammer either when the dashboard is just sitting open.
export function useExecutionQuality(windowSec = 86400) {
  return useQuery<ExecutionQualityReport>({
    queryKey: ['execution-quality', windowSec],
    queryFn: () => fetchExecutionQuality(windowSec),
    refetchInterval: 60_000,
    refetchOnWindowFocus: false,
    staleTime: 30_000,
  })
}
