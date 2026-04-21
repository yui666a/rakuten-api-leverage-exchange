const API_HOST =
  import.meta.env.VITE_API_HOST != null && import.meta.env.VITE_API_HOST !== ''
    ? import.meta.env.VITE_API_HOST
    : 'localhost:8080'

export const API_BASE = `http://${API_HOST}/api/v1`
export const WS_BASE = `ws://${API_HOST}/api/v1`

export type StatusResponse = {
  status: 'running' | 'stopped'
  tradingHalted: boolean
  manuallyStopped: boolean
  balance: number
  dailyLoss: number
  totalPosition: number
}

export type DailyPnLBreakdown = {
  realized: number
  unrealized: number
  total: number
  stale: boolean
  computedAt: number
}

export type PnlResponse = {
  balance: number
  dailyLoss: number
  totalPosition: number
  tradingHalted: boolean
  dailyPnl?: DailyPnLBreakdown
}

export type StrategyResponse = {
  stance: string
  reasoning: string
}

export type IndicatorSet = {
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

export type Candle = {
  open: number
  high: number
  low: number
  close: number
  volume: number
  time: number
}

export type Position = {
  id: number
  symbolId: number
  orderSide: 'BUY' | 'SELL'
  price: number
  remainingAmount: number
  floatingProfit: number
}

export type RiskConfig = {
  maxPositionAmount: number
  maxDailyLoss: number
  stopLossPercent: number
  takeProfitPercent: number
  initialCapital: number
  maxConsecutiveLosses: number
  cooldownMinutes: number
}

// TradableSymbol は GET /api/v1/symbols のレスポンス要素。
// 名前を Symbol にすると ES の組み込み Symbol と衝突するため TradableSymbol にしている。
export type TradableSymbol = {
  id: number
  authority: string
  tradeType: string
  currencyPair: string
  baseCurrency: string
  quoteCurrency: string
  baseScale: number
  quoteScale: number
  baseStepAmount: number
  minOrderAmount: number
  maxOrderAmount: number
  makerTradeFeePercent: number
  takerTradeFeePercent: number
  closeOnly: boolean
  viewOnly: boolean
  enabled: boolean
}

export type TradingConfig = {
  symbolId: number
  tradeAmount: number
}

export type BotControlResponse = {
  status: 'running' | 'stopped'
  tradingHalted: boolean
  manuallyStopped: boolean
}

export type TradeHistoryItem = {
  id: number
  symbolId: number
  orderSide: 'BUY' | 'SELL'
  price: number
  amount: number
  profit: number
  fee: number
  positionFee: number
  closeTradeProfit: number
  orderId: number
  positionId: number
  createdAt: number
}

export type AllTradesEntry = {
  symbolId: number
  currencyPair: string
  trades?: TradeHistoryItem[]
  error?: string
}

export type AllTradesResponse = {
  results: AllTradesEntry[]
}

export type LiveTicker = {
  symbolId: number
  bestAsk: number
  bestBid: number
  open: number
  high: number
  low: number
  last: number
  volume: number
  timestamp: number
}

export type RealtimeMarketTrades = {
  symbolId: number
  trades: Array<{
    id: number
    orderSide: 'BUY' | 'SELL'
    price: number
    amount: number
    assetAmount: number
    tradedAt: number
  }>
  timestamp: number
}

export type RealtimeOrderbook = {
  symbolId: number
  asks: Array<{ price: number; amount: number }>
  bids: Array<{ price: number; amount: number }>
  bestAsk: number
  bestBid: number
  midPrice: number
  spread: number
  timestamp: number
}

export type RealtimeEventMessage =
  | { type: 'ticker'; symbolId: number; data: LiveTicker }
  | { type: 'status'; symbolId?: number; data: StatusResponse }
  | { type: 'config'; symbolId?: number; data: RiskConfig }
  | { type: 'orderbook'; symbolId: number; data: RealtimeOrderbook }
  | { type: 'market_trades'; symbolId: number; data: RealtimeMarketTrades }

// --- Backtest types ---

// SummaryBreakdown mirrors entity.SummaryBreakdown (PR-1). Used for the
// per-exit-reason / per-signal-source tables surfaced in DetailPanel.
export type SummaryBreakdown = {
  trades: number
  winTrades: number
  lossTrades: number
  winRate: number
  totalPnL: number
  avgPnL: number
  profitFactor: number
}

// DrawdownPeriod mirrors entity.DrawdownPeriod (PR-3). recoveredAt=0 and
// recoveryBars=-1 mark an unrecovered episode.
export type DrawdownPeriod = {
  fromTimestamp: number
  toTimestamp: number
  recoveredAt: number
  depth: number
  depthBalance: number
  durationBars: number
  recoveryBars: number
}

// BacktestSummary mirrors entity.BacktestSummary. Legacy rows (persisted
// before PR-1 / PR-3) may omit the optional fields entirely; UI renders
// each section only when the payload contains non-empty data.
export type BacktestSummary = {
  periodFrom: number
  periodTo: number
  initialBalance: number
  finalBalance: number
  totalReturn: number
  totalTrades: number
  winTrades: number
  lossTrades: number
  winRate: number
  profitFactor: number
  maxDrawdown: number
  maxDrawdownBalance: number
  sharpeRatio: number
  avgHoldSeconds: number
  totalCarryingCost: number
  totalSpreadCost: number
  biweeklyWinRate?: number

  // PR-1: per-exit-reason / per-signal-source breakdowns.
  byExitReason?: Record<string, SummaryBreakdown>
  bySignalSource?: Record<string, SummaryBreakdown>

  // PR-3: drawdown history + time-in-market + expectancy.
  drawdownPeriods?: DrawdownPeriod[]
  drawdownThreshold?: number
  unrecoveredDrawdown?: DrawdownPeriod | null
  timeInMarketRatio?: number
  longestFlatStreakBars?: number
  expectancyPerTrade?: number
  avgWinJpy?: number
  avgLossJpy?: number
}

export type BacktestTrade = {
  tradeId: number
  symbolId: number
  entryTime: number
  exitTime: number
  side: string
  entryPrice: number
  exitPrice: number
  amount: number
  pnl: number
  pnlPercent: number
  carryingCost: number
  spreadCost: number
  reasonEntry: string
  reasonExit: string
}

export type BacktestResult = {
  id: string
  createdAt: number
  config: {
    symbol: string
    symbolId: number
    primaryInterval: string
    higherTfInterval: string
    fromTimestamp: number
    toTimestamp: number
    initialBalance: number
    spreadPercent: number
    dailyCarryCost: number
    slippagePercent: number
  }
  summary: BacktestSummary
  trades?: BacktestTrade[]
  // PDCA metadata — introduced by spec §5. Optional on the wire because:
  //   - profileName / pdcaCycleId / hypothesis use Go's `omitempty` tag so
  //     empty strings may be dropped from the JSON payload for legacy rows.
  //   - parentResultId is `*string` in Go with `omitempty`, so the field is
  //     absent entirely for root runs; present as null is also tolerated.
  profileName?: string
  pdcaCycleId?: string
  hypothesis?: string
  parentResultId?: string | null
}

export type BacktestResultListResponse = {
  results: BacktestResult[]
}

export type BacktestCSVMeta = {
  data: string
  symbol: string
  symbolId: number
  interval: string
  rowCount: number
  fromTimestamp: number
  toTimestamp: number
}

export type BacktestRunRequest = {
  data: string
  dataHtf?: string
  from?: string
  to?: string
  initialBalance?: number
  spread?: number
  carryingCost?: number
  slippage?: number
  tradeAmount?: number
  stopLossPercent?: number
  takeProfitPercent?: number
  maxPositionAmount?: number
  maxDailyLoss?: number
  maxConsecutiveLosses?: number
  cooldownMinutes?: number
}

export async function fetchApi<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`)
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

export async function sendApi<TResponse, TBody = undefined>(
  path: string,
  method: 'POST' | 'PUT',
  body?: TBody,
): Promise<TResponse> {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }

  return res.json()
}

export function buildRealtimeWebSocketUrl(symbolId: number): string {
  if (typeof window === 'undefined') {
    return `${WS_BASE}/ws?symbolId=${symbolId}`
  }

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.hostname === 'localhost' ? API_HOST : window.location.host
  return `${protocol}//${host}/api/v1/ws?symbolId=${symbolId}`
}
