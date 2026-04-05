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

export type PnlResponse = {
  balance: number
  dailyLoss: number
  totalPosition: number
  tradingHalted: boolean
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
  initialCapital: number
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
