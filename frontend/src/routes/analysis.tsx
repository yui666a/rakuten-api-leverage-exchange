import { createFileRoute, useNavigate, useSearch } from '@tanstack/react-router'
import { AppFrame } from '../components/AppFrame'
import { BacktestBody } from './backtest'
import { BacktestMultiBody } from './backtest-multi'
import { WalkForwardBody } from './walk-forward'

type ViewKey = 'runs' | 'multi' | 'wfo'

const VIEW_KEYS: ViewKey[] = ['runs', 'multi', 'wfo']

const VIEW_TABS: { key: ViewKey; label: string; subtitle: string }[] = [
  {
    key: 'runs',
    label: '単発バックテスト',
    subtitle: '過去のバックテスト結果の一覧と詳細を確認できます。',
  },
  {
    key: 'multi',
    label: 'マルチ期間',
    subtitle: 'POST /api/v1/backtest/run-multi の envelope を RobustnessScore でランキング表示',
  },
  {
    key: 'wfo',
    label: 'WFO',
    subtitle: '/backtest/walk-forward の envelope を参照。窓別 OOS リターンと Best パラメータ頻度を表示',
  },
]

type AnalysisSearch = {
  symbol?: string
  view?: ViewKey
}

export const Route = createFileRoute('/analysis')({
  component: AnalysisPage,
  validateSearch: (search: Record<string, unknown>): AnalysisSearch => ({
    symbol: typeof search.symbol === 'string' ? search.symbol : undefined,
    view:
      typeof search.view === 'string' && (VIEW_KEYS as string[]).includes(search.view)
        ? (search.view as ViewKey)
        : undefined,
  }),
})

function AnalysisPage() {
  const search = useSearch({ from: '/analysis' })
  const navigate = useNavigate({ from: '/analysis' })
  const view: ViewKey = search.view ?? 'runs'
  const tab = VIEW_TABS.find((t) => t.key === view) ?? VIEW_TABS[0]

  const setView = (next: ViewKey) => {
    navigate({
      search: (prev) => ({ ...prev, view: next === 'runs' ? undefined : next }),
      replace: true,
    })
  }

  return (
    <AppFrame title="分析" subtitle={tab.subtitle}>
      <div className="mb-4 flex flex-wrap gap-2">
        {VIEW_TABS.map((t) => (
          <button
            key={t.key}
            type="button"
            onClick={() => setView(t.key)}
            className={`rounded-full border px-4 py-2 text-sm font-medium transition ${
              view === t.key
                ? 'border-white/20 bg-white/10 text-white'
                : 'border-white/8 bg-transparent text-text-secondary hover:text-white'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>
      {view === 'runs' && <BacktestBody />}
      {view === 'multi' && <BacktestMultiBody />}
      {view === 'wfo' && <WalkForwardBody />}
    </AppFrame>
  )
}
