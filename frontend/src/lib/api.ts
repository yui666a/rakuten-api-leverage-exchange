export const API_BASE = 'http://localhost:8080/api/v1'
export const WS_BASE = 'ws://localhost:8080/api/v1'

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

export type MarketStreamMessage = {
  type: 'ticker'
  data: LiveTicker
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

export function buildMarketWebSocketUrl(symbolId: number): string {
  if (typeof window === 'undefined') {
    return `${WS_BASE}/ws/market?symbolId=${symbolId}`
  }

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.hostname === 'localhost' ? 'localhost:8080' : window.location.host
  return `${protocol}//${host}/api/v1/ws/market?symbolId=${symbolId}`
}
