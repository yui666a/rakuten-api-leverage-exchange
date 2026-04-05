# Plan 10: フロントエンド ダッシュボード Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** TanStack Start + Lightweight Chartsでトレーディングボットの監視ダッシュボードを構築する。KPIカード、ローソク足チャート、テクニカル指標、ポジション一覧を一画面で表示する。

**Architecture:** Vite+でTanStack Startプロジェクトを作成し、TanStack Queryでバックエンド REST APIをポーリング。コンポーネントはKpiCard, CandlestickChart, IndicatorPanel, PositionPanelに分割。ダークネイビーテーマをTailwindCSSで実��する。

**Tech Stack:** Vite+, TanStack Start, TanStack Query, React 19, TypeScript, TailwindCSS v4, Lightweight Charts v4

**前提:** Plan 9（バックエンド追加エンドポイント）が完了していること。

---

## ファイル構成

```
frontend/
├── vite.config.ts                       # Vite設定（テンプレート生成）
├── tsconfig.json                        # TypeScript設定（テンプレート生成）
├── package.json                         # 依存パッケージ
├── src/
│   ├── router.tsx                       # TanStack Router設定（テンプレート生成）
│   ├── styles.css                       # TailwindCSS + ダークネイビーテ���マ
│   ├── routes/
│   │   ├── __root.tsx                   # ルートレイアウト（カスタマイズ）
│   │   └── index.tsx                    # ダッシュ���ードページ
│   ├── components/
│   │   ├── KpiCard.tsx                  # KPIカード
│   │   ├── CandlestickChart.tsx         # ローソク足チャート
│   │   ├── IndicatorPanel.tsx           # テクニカル指標パネル
│   │   └── PositionPanel.tsx            # ポジションパネル
│   ├── hooks/
│   │   ├── useStatus.ts                 # ステータス取得
│   │   ├── usePnl.ts                    # 損益取得
│   │   ├── useStrategy.ts              # 戦略方針取得
│   │   ├── useIndicators.ts            # ���クニカル指標取得
│   │   └── useCandles.ts               # ローソク足取得
│   └── lib/
│       └── api.ts                       # APIクライアント
```

---

### Task 1: TanStack Start プロジェクト作成

- [ ] **Step 1: プ��ジェクト作成**

```bash
cd /path/to/rakuten-api-leverage-exchange
npx @tanstack/cli@latest create frontend
```

対話プロンプトはデフォルトで進める。

- [ ] **Step 2: Vite+ローカルパッケージをインストール**

```bash
cd frontend
npm i vite-plus --save-dev
```

- [ ] **Step 3: 追加パッケージをインストール**

```bash
npm i @tanstack/react-query lightweight-charts
```

- [ ] **Step 4: 不要ファイルを削除**

```bash
rm -f src/routes/about.tsx src/components/Footer.tsx src/components/Header.tsx src/components/ThemeToggle.tsx
```

- [ ] **Step 5: dev起動確認**

```bash
npm run dev
```

http://localhost:3000 でTanStack Startのデフォルトページが表示されることを確��。Ctrl+Cで停止。

- [ ] **Step 6: コミット**

```bash
git add frontend/
git commit -m "feat: scaffold TanStack Start frontend project"
```

---

### Task 2: ダークネイビーテーマ + ルートレイアウト

**Files:**
- Modify: `frontend/src/styles.css`
- Modify: `frontend/src/routes/__root.tsx`

- [ ] **Step 1: styles.css をダークネイビーテーマに書き換え**

```css
@import 'tailwindcss';

@theme {
  --color-bg-primary: #0f0f23;
  --color-bg-card: #1a1a3e;
  --color-bg-card-hover: #22224a;
  --color-text-primary: #e0e0e0;
  --color-text-secondary: #666;
  --color-accent-green: #00d4aa;
  --color-accent-red: #ff4757;
  --color-accent-blue: #3742fa;
}
```

- [ ] **Step 2: __root.tsx をカスタマイズ**

```tsx
import { HeadContent, Outlet, Scripts, createRootRoute } from '@tanstack/react-router'
import appCss from '../styles.css?url'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Trading Bot Dashboard' },
    ],
    links: [
      { rel: 'stylesheet', href: appCss },
    ],
  }),
  component: RootComponent,
})

function RootComponent() {
  return (
    <html lang="ja">
      <head>
        <HeadContent />
      </head>
      <body className="bg-bg-primary text-text-primary min-h-screen">
        <Outlet />
        <Scripts />
      </body>
    </html>
  )
}
```

- [ ] **Step 3: dev起動確認**

```bash
cd frontend && npm run dev
```

ダークネイビー背景で空ページが表示されることを確認。

- [ ] **Step 4: コミット**

```bash
git add frontend/src/styles.css frontend/src/routes/__root.tsx
git commit -m "feat: apply dark navy theme and customize root layout"
```

---

### Task 3: APIクライアント + データ取得hooks

**Files:**
- Create: `frontend/src/lib/api.ts`
- Create: `frontend/src/hooks/useStatus.ts`
- Create: `frontend/src/hooks/usePnl.ts`
- Create: `frontend/src/hooks/useStrategy.ts`
- Create: `frontend/src/hooks/useIndicators.ts`
- Create: `frontend/src/hooks/useCandles.ts`

- [ ] **Step 1: api.ts を作成**

```ts
const API_BASE = 'http://localhost:8080/api/v1'

export async function fetchApi<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`)
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}
```

- [ ] **Step 2: hooks を作成**

useStatus.ts:
```ts
import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type Status = {
  status: string
  tradingHalted: boolean
  balance: number
  dailyLoss: number
  totalPosition: number
}

export function useStatus() {
  return useQuery({
    queryKey: ['status'],
    queryFn: () => fetchApi<Status>('/status'),
    refetchInterval: 10_000,
  })
}
```

usePnl.ts:
```ts
import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type PnL = {
  balance: number
  dailyLoss: number
  totalPosition: number
  tradingHalted: boolean
}

export function usePnl() {
  return useQuery({
    queryKey: ['pnl'],
    queryFn: () => fetchApi<PnL>('/pnl'),
    refetchInterval: 10_000,
  })
}
```

useStrategy.ts:
```ts
import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type Strategy = {
  stance: string
  reasoning: string
}

export function useStrategy() {
  return useQuery({
    queryKey: ['strategy'],
    queryFn: () => fetchApi<Strategy>('/strategy'),
    refetchInterval: 30_000,
  })
}
```

useIndicators.ts:
```ts
import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type IndicatorSet = {
  symbolId: number
  sma20: number | null
  sma50: number | null
  ema12: number | null
  ema26: number | null
  rsi14: number | null
  macdLine: number | null
  signalLine: number | null
  histogram: number | null
  timestamp: number
}

export function useIndicators(symbolId: number) {
  return useQuery({
    queryKey: ['indicators', symbolId],
    queryFn: () => fetchApi<IndicatorSet>(`/indicators/${symbolId}`),
    refetchInterval: 30_000,
  })
}
```

useCandles.ts:
```ts
import { useQuery } from '@tanstack/react-query'
import { fetchApi } from '../lib/api'

type Candle = {
  open: number
  high: number
  low: number
  close: number
  volume: number
  time: number
}

export function useCandles(symbolId: number) {
  return useQuery({
    queryKey: ['candles', symbolId],
    queryFn: () => fetchApi<Candle[]>(`/candles/${symbolId}`),
    refetchInterval: 60_000,
  })
}
```

- [ ] **Step 3: ビルド確認**

```bash
cd frontend && npx tsc --noEmit
```

- [ ] **Step 4: コミット**

```bash
git add frontend/src/lib/ frontend/src/hooks/
git commit -m "feat: add API client and data fetching hooks"
```

---

### Task 4: KPIカードコンポーネント

**Files:**
- Create: `frontend/src/components/KpiCard.tsx`

- [ ] **Step 1: KpiCard.tsx を作成**

```tsx
type KpiCardProps = {
  label: string
  value: string
  color?: string
}

export function KpiCard({ label, value, color = 'text-text-primary' }: KpiCardProps) {
  return (
    <div className="bg-bg-card rounded-lg p-4 text-center">
      <div className={`text-2xl font-bold ${color}`}>{value}</div>
      <div className="text-text-secondary text-xs mt-1">{label}</div>
    </div>
  )
}
```

- [ ] **Step 2: コミット**

```bash
git add frontend/src/components/KpiCard.tsx
git commit -m "feat: add KpiCard component"
```

---

### Task 5: ローソク足チャートコンポーネント

**Files:**
- Create: `frontend/src/components/CandlestickChart.tsx`

- [ ] **Step 1: CandlestickChart.tsx を作成**

```tsx
import { useEffect, useRef } from 'react'
import { createChart, type IChartApi, type ISeriesApi, type CandlestickData, type Time } from 'lightweight-charts'

type Candle = {
  open: number
  high: number
  low: number
  close: number
  time: number
}

type CandlestickChartProps = {
  candles: Candle[]
}

export function CandlestickChart({ candles }: CandlestickChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const seriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)

  useEffect(() => {
    if (!containerRef.current) return

    const chart = createChart(containerRef.current, {
      layout: {
        background: { color: '#1a1a3e' },
        textColor: '#e0e0e0',
      },
      grid: {
        vertLines: { color: '#2a2a4e' },
        horzLines: { color: '#2a2a4e' },
      },
      width: containerRef.current.clientWidth,
      height: 400,
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
      },
    })

    const series = chart.addCandlestickSeries({
      upColor: '#00d4aa',
      downColor: '#ff4757',
      borderUpColor: '#00d4aa',
      borderDownColor: '#ff4757',
      wickUpColor: '#00d4aa',
      wickDownColor: '#ff4757',
    })

    chartRef.current = chart
    seriesRef.current = series

    const handleResize = () => {
      if (containerRef.current) {
        chart.applyOptions({ width: containerRef.current.clientWidth })
      }
    }
    window.addEventListener('resize', handleResize)

    return () => {
      window.removeEventListener('resize', handleResize)
      chart.remove()
    }
  }, [])

  useEffect(() => {
    if (!seriesRef.current || candles.length === 0) return

    const data: CandlestickData<Time>[] = candles.map((c) => ({
      time: c.time as Time,
      open: c.open,
      high: c.high,
      low: c.low,
      close: c.close,
    }))

    seriesRef.current.setData(data)
    chartRef.current?.timeScale().fitContent()
  }, [candles])

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="text-text-secondary text-xs mb-2">BTC/JPY</div>
      <div ref={containerRef} />
    </div>
  )
}
```

- [ ] **Step 2: コミット**

```bash
git add frontend/src/components/CandlestickChart.tsx
git commit -m "feat: add CandlestickChart component with Lightweight Charts"
```

---

### Task 6: テクニカル指標 + ポジションパネル

**Files:**
- Create: `frontend/src/components/IndicatorPanel.tsx`
- Create: `frontend/src/components/PositionPanel.tsx`

- [ ] **Step 1: IndicatorPanel.tsx を作成**

```tsx
type IndicatorSet = {
  sma20: number | null
  sma50: number | null
  ema12: number | null
  ema26: number | null
  rsi14: number | null
  macdLine: number | null
  signalLine: number | null
  histogram: number | null
}

type IndicatorPanelProps = {
  indicators: IndicatorSet | undefined
}

function formatNum(v: number | null, decimals = 0): string {
  if (v === null || v === undefined) return '—'
  return v.toLocaleString('ja-JP', { maximumFractionDigits: decimals })
}

export function IndicatorPanel({ indicators }: IndicatorPanelProps) {
  if (!indicators) {
    return (
      <div className="bg-bg-card rounded-lg p-4">
        <div className="text-text-secondary text-xs mb-2">テクニカル指標</div>
        <div className="text-text-secondary text-sm">読み込み中...</div>
      </div>
    )
  }

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="text-text-secondary text-xs mb-3">テクニカル指標</div>
      <div className="space-y-2 text-sm">
        <div className="flex justify-between">
          <span className="text-text-secondary">RSI(14)</span>
          <span>{formatNum(indicators.rsi14, 1)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">SMA(20)</span>
          <span>{formatNum(indicators.sma20)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">SMA(50)</span>
          <span>{formatNum(indicators.sma50)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">EMA(12)</span>
          <span>{formatNum(indicators.ema12)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">EMA(26)</span>
          <span>{formatNum(indicators.ema26)}</span>
        </div>
        <div className="border-t border-bg-card-hover my-2" />
        <div className="flex justify-between">
          <span className="text-text-secondary">MACD</span>
          <span>{formatNum(indicators.macdLine, 2)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">Signal</span>
          <span>{formatNum(indicators.signalLine, 2)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-text-secondary">Histogram</span>
          <span>{formatNum(indicators.histogram, 2)}</span>
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: PositionPanel.tsx を作成**

```tsx
type Position = {
  id: number
  symbolId: number
  orderSide: string
  price: number
  remainingAmount: number
  floatingProfit: number
}

type PositionPanelProps = {
  positions: Position[] | undefined
}

export function PositionPanel({ positions }: PositionPanelProps) {
  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="text-text-secondary text-xs mb-3">ポジション</div>
      {!positions || positions.length === 0 ? (
        <div className="text-text-secondary text-sm">ポジションなし</div>
      ) : (
        <div className="space-y-2">
          {positions.map((pos) => (
            <div key={pos.id} className="flex justify-between text-sm">
              <span className={pos.orderSide === 'BUY' ? 'text-accent-green' : 'text-accent-red'}>
                {pos.orderSide === 'BUY' ? 'LONG' : 'SHORT'} {pos.remainingAmount}
              </span>
              <span className="text-text-secondary">
                @ ¥{pos.price.toLocaleString()}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 3: コミット**

```bash
git add frontend/src/components/IndicatorPanel.tsx frontend/src/components/PositionPanel.tsx
git commit -m "feat: add IndicatorPanel and PositionPanel components"
```

---

### Task 7: ダッシュボードページ組み立て

**Files:**
- Modify: `frontend/src/routes/index.tsx`
- Modify: `frontend/src/router.tsx`

- [ ] **Step 1: router.tsx に QueryClient を追加**

```tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRouter as createTanStackRouter } from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'

const queryClient = new QueryClient()

export function getRouter() {
  const router = createTanStackRouter({
    routeTree,
    scrollRestoration: true,
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
    Wrap: ({ children }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  })

  return router
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}
```

- [ ] **Step 2: index.tsx をダッシュボードに書き換え**

```tsx
import { createFileRoute } from '@tanstack/react-router'
import { KpiCard } from '../components/KpiCard'
import { CandlestickChart } from '../components/CandlestickChart'
import { IndicatorPanel } from '../components/IndicatorPanel'
import { PositionPanel } from '../components/PositionPanel'
import { useStatus } from '../hooks/useStatus'
import { usePnl } from '../hooks/usePnl'
import { useStrategy } from '../hooks/useStrategy'
import { useIndicators } from '../hooks/useIndicators'
import { useCandles } from '../hooks/useCandles'

export const Route = createFileRoute('/')({ component: Dashboard })

function Dashboard() {
  const { data: status } = useStatus()
  const { data: pnl } = usePnl()
  const { data: strategy } = useStrategy()
  const { data: indicators } = useIndicators(7)
  const { data: candles } = useCandles(7)

  return (
    <main className="max-w-7xl mx-auto p-4">
      {/* KPI Cards */}
      <div className="grid grid-cols-4 gap-4 mb-4">
        <KpiCard
          label="残高"
          value={pnl ? `¥${pnl.balance.toLocaleString()}` : '—'}
          color="text-accent-green"
        />
        <KpiCard
          label="日次損益"
          value={pnl ? `¥${(-pnl.dailyLoss).toLocaleString()}` : '—'}
          color={pnl && pnl.dailyLoss > 0 ? 'text-accent-red' : 'text-accent-green'}
        />
        <KpiCard
          label="戦略方針"
          value={strategy?.stance ?? '—'}
          color="text-accent-blue"
        />
        <KpiCard
          label="ステータス"
          value={status?.tradingHalted ? '停止中' : (status?.status ?? '—')}
          color={status?.tradingHalted ? 'text-accent-red' : 'text-accent-green'}
        />
      </div>

      {/* Chart + Side Panel */}
      <div className="grid grid-cols-3 gap-4">
        <div className="col-span-2">
          <CandlestickChart candles={candles ?? []} />
        </div>
        <div className="space-y-4">
          <IndicatorPanel indicators={indicators} />
          <PositionPanel positions={undefined} />
        </div>
      </div>
    </main>
  )
}
```

- [ ] **Step 3: dev起動確認**

```bash
cd frontend && npm run dev
```

http://localhost:3000 でダッシュボードが表示されることを確認（バックエンド未起動ならKPIは「—」表示）。

- [ ] **Step 4: コミット**

```bash
git add frontend/src/routes/index.tsx frontend/src/router.tsx
git commit -m "feat: assemble dashboard page with all components"
```
