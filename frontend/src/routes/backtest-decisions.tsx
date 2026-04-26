import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { AppFrame } from '../components/AppFrame'
import { DecisionLogTable } from '../components/DecisionLogTable'
import { fetchApi, type DecisionLogResponse } from '../lib/api'

type Search = { id: string }

export const Route = createFileRoute('/backtest-decisions')({
  validateSearch: (raw: Record<string, unknown>): Search => ({
    id: typeof raw.id === 'string' ? raw.id : '',
  }),
  component: BacktestDecisionsPage,
})

function BacktestDecisionsPage() {
  const { id } = Route.useSearch()

  const { data, isLoading, error } = useQuery({
    queryKey: ['backtest-decisions', id],
    queryFn: () => fetchApi<DecisionLogResponse>(`/backtest/results/${id}/decisions?limit=1000`),
    enabled: id !== '',
  })

  return (
    <AppFrame
      title="Backtest Decision Log"
      subtitle={`Run ${id || '(no id)'} の 15 分足ごとの売買判断ログ`}
    >
      <div className="mb-4">
        <Link
          to="/backtest"
          className="text-sm text-cyan-200 underline-offset-2 hover:underline"
        >
          ← バックテスト結果一覧へ戻る
        </Link>
      </div>

      {id === '' && (
        <div className="rounded-2xl border border-accent-red/40 bg-accent-red/10 px-5 py-3 text-sm text-accent-red">
          run id が指定されていません。バックテスト結果一覧から開いてください。
        </div>
      )}

      {error && (
        <div className="rounded-2xl border border-accent-red/40 bg-accent-red/10 px-5 py-3 text-sm text-accent-red">
          取得に失敗しました: {(error as Error).message}
        </div>
      )}

      {isLoading && (
        <div className="rounded-2xl border border-white/8 bg-bg-card/90 p-8 text-center text-text-secondary">
          読み込み中...
        </div>
      )}

      {data && (
        <>
          <div className="mb-4 text-xs text-text-secondary">
            {data.decisions.length.toLocaleString()} 件
            {data.hasMore ? ' (上限 1000 件まで表示)' : ''}
          </div>
          <DecisionLogTable decisions={data.decisions} />
        </>
      )}
    </AppFrame>
  )
}
