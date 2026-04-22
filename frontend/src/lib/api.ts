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

// MultiPeriodAggregate mirrors entity.MultiPeriodAggregate. Scalar fields
// are nullable because BE emits JSON null for NaN / ±Inf (ruin path).
export type MultiPeriodAggregate = {
  geomMeanReturn: number | null
  returnStdDev: number | null
  worstReturn: number | null
  bestReturn: number | null
  worstDrawdown: number | null
  allPositive: boolean
  robustnessScore: number | null
}

export type LabeledBacktestResult = {
  label: string
  result: BacktestResult
}

export type MultiPeriodResult = {
  id: string
  createdAt: number
  profileName: string
  pdcaCycleId?: string
  hypothesis?: string
  parentResultId?: string | null
  periods: LabeledBacktestResult[]
  aggregate: MultiPeriodAggregate
}

// MultiPeriodResultListResponse: GET /backtest/multi-results returns
// {results: [...]} — per-period bodies are empty here (envelope only) and
// must be rehydrated via GET /backtest/multi-results/:id when needed.
export type MultiPeriodResultListResponse = {
  results: MultiPeriodResult[]
}

// --- Walk-forward (PR-13 follow-up / #120) ---
//
// The BE `result` blob is a full WalkForwardResult. Mirror enough of its
// shape to drive the summary table; deep per-window BacktestResult data
// is accessed via the nested `oosResult.summary` and `isResults[*].summary`.

export type WalkForwardISResult = {
  parameters: Record<string, number>
  summary: BacktestSummary
  score: number
  resultId?: string
}

export type WalkForwardWindowResult = {
  index: number
  inSampleFrom: number
  inSampleTo: number
  oosFrom: number
  oosTo: number
  bestParameters: Record<string, number>
  isResults: WalkForwardISResult[]
  oosResult: BacktestResult
}

export type WalkForwardResultEnvelope = {
  id: string
  createdAt: number
  baseProfile: string
  objective: string
  pdcaCycleId?: string
  hypothesis?: string
  parentResultId?: string | null
  windows: WalkForwardWindowResult[]
  aggregateOOS: MultiPeriodAggregate
}

// walkForwardResponse from GET /:id and GET list. request/result/aggregateOOS
// arrive as pre-parsed JSON (json.RawMessage on the BE) so the typed shape is
// `unknown` here; the page parses them narrowly where needed.
export type WalkForwardEnvelopeResponse = {
  id: string
  createdAt: number
  baseProfile: string
  objective: string
  pdcaCycleId?: string
  hypothesis?: string
  parentResultId?: string | null
  request?: unknown
  result?: WalkForwardResultEnvelope
  aggregateOOS?: MultiPeriodAggregate
}

export type WalkForwardListResponse = {
  items: WalkForwardEnvelopeResponse[]
  total: number
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
  // PR-12 (#113): ATR-based stop / trailing. >0 activates the ATR gate
  // instead of the percent-based fallback.
  stopLossAtrMultiplier?: number
  trailingAtrMultiplier?: number
  takeProfitPercent?: number
  maxPositionAmount?: number
  maxDailyLoss?: number
  maxConsecutiveLosses?: number
  cooldownMinutes?: number
  // PR-12 (profile UI): when set, the preset name picked in the UI. The
  // server uses this purely as the audit label on the resulting row.
  profileName?: string
  // PR-12 (profile UI): when set, supersedes profileName for strategy
  // construction — the FE picker loads a preset, the user edits the
  // fields inline, and the edited StrategyProfile ships here. Router
  // profiles (those with regime_routing) are rejected server-side.
  profileOverride?: StrategyProfile
  // PR-12: PDCA continuation label (optional).
  pdcaCycleId?: string
  hypothesis?: string
  parentResultId?: string | null
}

/* ------------------------------------------------------------------ */
/* PR-12 profile picker types                                         */
/* ------------------------------------------------------------------ */

// ProfileSummary mirrors strategyprofile.ProfileSummary on the BE side.
// Used for GET /api/v1/profiles; the picker renders these in a dropdown.
export type ProfileSummary = {
  name: string
  description: string
  isRouter: boolean
}

export type ProfileListResponse = {
  profiles: ProfileSummary[]
}

// StrategyProfile mirrors entity.StrategyProfile. Only the fields the FE
// edit-and-run form reads or writes are listed; other fields are passed
// through untouched via the catch-all `regime_routing?: unknown`.
export type StrategyProfile = {
  name: string
  description: string
  indicators: IndicatorConfig
  stance_rules: StanceRulesConfig
  signal_rules: SignalRulesConfig
  strategy_risk: StrategyRiskConfig
  htf_filter: HTFFilterConfig
  // regime_routing is out of scope for the edit-and-run UI. Preserved
  // as-is on round-trip so a profile that happens to carry a router
  // block is not silently mutated.
  regime_routing?: unknown
}

export type IndicatorConfig = {
  sma_short: number
  sma_long: number
  rsi_period: number
  macd_fast: number
  macd_slow: number
  macd_signal: number
  bb_period: number
  bb_multiplier: number
  atr_period: number
}

export type StanceRulesConfig = {
  rsi_oversold: number
  rsi_overbought: number
  sma_convergence_threshold: number
  bb_squeeze_lookback: number
  breakout_volume_ratio: number
}

export type SignalRulesConfig = {
  trend_follow: TrendFollowConfig
  contrarian: ContrarianConfig
  breakout: BreakoutConfig
}

export type TrendFollowConfig = {
  enabled: boolean
  require_macd_confirm: boolean
  require_ema_cross: boolean
  rsi_buy_max: number
  rsi_sell_min: number
  adx_min?: number
  require_obv_alignment?: boolean
}

export type ContrarianConfig = {
  enabled: boolean
  rsi_entry: number
  rsi_exit: number
  macd_histogram_limit: number
  adx_max?: number
  stoch_entry_max?: number
  stoch_exit_min?: number
}

export type BreakoutConfig = {
  enabled: boolean
  volume_ratio_min: number
  require_macd_confirm: boolean
  adx_min?: number
  donchian_period?: number
  cmf_buy_min?: number
  cmf_sell_max?: number
}

export type StrategyRiskConfig = {
  stop_loss_percent: number
  take_profit_percent: number
  stop_loss_atr_multiplier: number
  trailing_atr_multiplier?: number
  max_position_amount: number
  max_daily_loss: number
}

export type HTFFilterConfig = {
  enabled: boolean
  block_counter_trend: boolean
  alignment_boost: number
  mode?: string
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
