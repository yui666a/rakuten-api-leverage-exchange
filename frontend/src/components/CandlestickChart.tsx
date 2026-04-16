import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { createChart, CandlestickSeries, LineSeries, type IChartApi, type ISeriesApi, type CandlestickData, type LineData, type Time, type SeriesType, type ISeriesPrimitive, type SeriesAttachedParameter, type IPrimitivePaneView, type IPrimitivePaneRenderer } from 'lightweight-charts'
import type { CanvasRenderingTarget2D } from 'fancy-canvas'
import { useCandles, type CandleInterval } from '../hooks/useCandles'
import { MACDChart } from './MACDChart'
import { RSIChart } from './RSIChart'
import { StochasticsChart } from './StochasticsChart'

type CandlestickChartProps = {
  symbolId: number
}

type MALineKey = 'sma20' | 'sma50' | 'sma100' | 'sma200' | 'ema12' | 'ema26'
type BBKey = 'bb1' | 'bb2' | 'bb3'
type BBLineKey = `${BBKey}Upper` | `${BBKey}Lower`
type IchimokuLineKey = 'tenkan' | 'kijun' | 'senkouA' | 'senkouB' | 'chikou'

const INTERVAL_OPTIONS: { value: CandleInterval; label: string }[] = [
  { value: 'PT1M', label: '1m' },
  { value: 'PT5M', label: '5m' },
  { value: 'PT15M', label: '15m' },
  { value: 'PT1H', label: '1h' },
  { value: 'P1D', label: '1D' },
  { value: 'P1W', label: '1W' },
]

const MA_CONFIG: Record<MALineKey, { label: string; color: string }> = {
  sma20: { label: 'SMA(20)', color: '#f5a623' },
  sma50: { label: 'SMA(50)', color: '#e74c8b' },
  sma100: { label: 'SMA(100)', color: '#26a69a' },
  sma200: { label: 'SMA(200)', color: '#ef5350' },
  ema12: { label: 'EMA(12)', color: '#00bfff' },
  ema26: { label: 'EMA(26)', color: '#a78bfa' },
}

const BB_CONFIG: Record<BBKey, { label: string; color: string; fillColor: string; multiplier: number }> = {
  bb1: { label: 'BB(1σ)', color: '#4fc3f7', fillColor: 'rgba(79, 195, 247, 0.12)', multiplier: 1 },
  bb2: { label: 'BB(2σ)', color: '#7c4dff', fillColor: 'rgba(124, 77, 255, 0.10)', multiplier: 2 },
  bb3: { label: 'BB(3σ)', color: '#ff6e40', fillColor: 'rgba(255, 110, 64, 0.08)', multiplier: 3 },
}

const ICHIMOKU_CONFIG: Record<IchimokuLineKey, { label: string; color: string }> = {
  tenkan: { label: '転換線', color: '#2196f3' },
  kijun: { label: '基準線', color: '#ff5722' },
  senkouA: { label: '先行スパン1', color: '#4caf50' },
  senkouB: { label: '先行スパン2', color: '#e91e63' },
  chikou: { label: '遅行スパン', color: '#ffeb3b' },
}

const ICHIMOKU_KUMO_COLORS = {
  bullish: 'rgba(76, 175, 80, 0.12)',
  bearish: 'rgba(233, 30, 99, 0.12)',
}

/** Threshold: fetch more data when user scrolls within N bars of the left edge. */
const SCROLL_THRESHOLD = 20

function calcSMA(closes: number[], period: number): (number | null)[] {
  const result: (number | null)[] = []
  for (let i = 0; i < closes.length; i++) {
    if (i < period - 1) {
      result.push(null)
    } else {
      let sum = 0
      for (let j = i - period + 1; j <= i; j++) sum += closes[j]
      result.push(sum / period)
    }
  }
  return result
}

function calcEMA(closes: number[], period: number): (number | null)[] {
  const result: (number | null)[] = []
  const k = 2 / (period + 1)
  for (let i = 0; i < closes.length; i++) {
    if (i < period - 1) {
      result.push(null)
    } else if (i === period - 1) {
      let sum = 0
      for (let j = 0; j < period; j++) sum += closes[j]
      result.push(sum / period)
    } else {
      const prev = result[i - 1]!
      result.push((closes[i] - prev) * k + prev)
    }
  }
  return result
}

function calcBollingerBands(
  closes: number[],
  period: number,
  multiplier: number,
): { upper: (number | null)[]; lower: (number | null)[] } {
  const upper: (number | null)[] = []
  const lower: (number | null)[] = []
  for (let i = 0; i < closes.length; i++) {
    if (i < period - 1) {
      upper.push(null)
      lower.push(null)
    } else {
      let sum = 0
      for (let j = i - period + 1; j <= i; j++) sum += closes[j]
      const mean = sum / period
      let sumSqDiff = 0
      for (let j = i - period + 1; j <= i; j++) {
        const diff = closes[j] - mean
        sumSqDiff += diff * diff
      }
      const stdDev = Math.sqrt(sumSqDiff / period)
      upper.push(mean + multiplier * stdDev)
      lower.push(mean - multiplier * stdDev)
    }
  }
  return { upper, lower }
}

type IchimokuResult = {
  tenkan: (number | null)[]
  kijun: (number | null)[]
  senkouA: (number | null)[]
  senkouB: (number | null)[]
  chikou: (number | null)[]
}

/** High-low midpoint over a rolling window. */
function highLowMid(highs: number[], lows: number[], period: number, index: number): number | null {
  if (index < period - 1) return null
  let maxH = -Infinity
  let minL = Infinity
  for (let j = index - period + 1; j <= index; j++) {
    if (highs[j] > maxH) maxH = highs[j]
    if (lows[j] < minL) minL = lows[j]
  }
  return (maxH + minL) / 2
}

function calcIchimoku(
  highs: number[],
  lows: number[],
  closes: number[],
): IchimokuResult {
  const len = closes.length
  const tenkan: (number | null)[] = []
  const kijun: (number | null)[] = []
  const senkouA: (number | null)[] = []
  const senkouB: (number | null)[] = []
  const chikou: (number | null)[] = []

  // Tenkan-sen (9), Kijun-sen (26)
  for (let i = 0; i < len; i++) {
    tenkan.push(highLowMid(highs, lows, 9, i))
    kijun.push(highLowMid(highs, lows, 26, i))
  }

  // Senkou Span A & B: computed at index i, plotted at i+26
  // So we need arrays of length len+26, where the first 26 entries are from "shifted" future data
  const totalLen = len + 26
  for (let i = 0; i < totalLen; i++) {
    const srcIdx = i - 26 // the source candle index
    if (srcIdx < 0 || srcIdx >= len) {
      senkouA.push(null)
      senkouB.push(null)
    } else {
      const t = tenkan[srcIdx]
      const k = kijun[srcIdx]
      senkouA.push(t !== null && k !== null ? (t + k) / 2 : null)
      senkouB.push(highLowMid(highs, lows, 52, srcIdx))
    }
  }

  // Chikou Span: close plotted 26 periods back
  for (let i = 0; i < len; i++) {
    const srcIdx = i + 26
    chikou.push(srcIdx < len ? closes[srcIdx] : null)
  }

  return { tenkan, kijun, senkouA, senkouB, chikou }
}

type BandPoint = { time: Time; upper: number; lower: number }

class BollingerBandFillRenderer implements IPrimitivePaneRenderer {
  private _data: BandPoint[] = []
  private _color: string = 'rgba(0,0,0,0)'
  private _series: ISeriesApi<SeriesType, Time> | null = null
  private _chart: IChartApi | null = null

  update(data: BandPoint[], color: string, series: ISeriesApi<SeriesType, Time>, chart: IChartApi) {
    this._data = data
    this._color = color
    this._series = series
    this._chart = chart
  }

  draw(target: CanvasRenderingTarget2D): void {
    const series = this._series
    const chart = this._chart
    if (!series || !chart || this._data.length === 0) return

    target.useMediaCoordinateSpace(({ context: ctx }) => {
      const points: { x: number; yUpper: number; yLower: number }[] = []

      for (const d of this._data) {
        const x = chart.timeScale().timeToCoordinate(d.time)
        const yUpper = series.priceToCoordinate(d.upper)
        const yLower = series.priceToCoordinate(d.lower)
        if (x === null || yUpper === null || yLower === null) continue
        points.push({ x, yUpper, yLower })
      }

      if (points.length < 2) return

      ctx.beginPath()
      // Draw upper edge left → right
      ctx.moveTo(points[0].x, points[0].yUpper)
      for (let i = 1; i < points.length; i++) {
        ctx.lineTo(points[i].x, points[i].yUpper)
      }
      // Draw lower edge right → left
      for (let i = points.length - 1; i >= 0; i--) {
        ctx.lineTo(points[i].x, points[i].yLower)
      }
      ctx.closePath()
      ctx.fillStyle = this._color
      ctx.fill()
    })
  }
}

class BollingerBandFillPrimitive implements ISeriesPrimitive<Time> {
  private _renderer = new BollingerBandFillRenderer()
  private _data: BandPoint[] = []
  private _color: string
  private _series: ISeriesApi<SeriesType, Time> | null = null
  private _chart: IChartApi | null = null
  private _paneViews: IPrimitivePaneView[]

  constructor(color: string) {
    this._color = color
    const renderer = this._renderer
    this._paneViews = [{
      zOrder: () => 'bottom' as const,
      renderer: () => renderer,
    }]
  }

  setData(data: BandPoint[]) {
    this._data = data
    this._updateRenderer()
  }

  attached(param: SeriesAttachedParameter<Time, SeriesType>) {
    this._series = param.series
    this._chart = param.chart
    this._updateRenderer()
  }

  detached() {
    this._series = null
    this._chart = null
  }

  updateAllViews() {
    this._updateRenderer()
  }

  paneViews(): readonly IPrimitivePaneView[] {
    return this._paneViews
  }

  private _updateRenderer() {
    if (this._series && this._chart) {
      this._renderer.update(this._data, this._color, this._series, this._chart)
    }
  }
}

type KumoPoint = { time: Time; senkouA: number; senkouB: number }

class KumoFillRenderer implements IPrimitivePaneRenderer {
  private _data: KumoPoint[] = []
  private _bullColor: string = ICHIMOKU_KUMO_COLORS.bullish
  private _bearColor: string = ICHIMOKU_KUMO_COLORS.bearish
  private _series: ISeriesApi<SeriesType, Time> | null = null
  private _chart: IChartApi | null = null

  update(data: KumoPoint[], series: ISeriesApi<SeriesType, Time>, chart: IChartApi) {
    this._data = data
    this._series = series
    this._chart = chart
  }

  draw(target: CanvasRenderingTarget2D): void {
    const series = this._series
    const chart = this._chart
    if (!series || !chart || this._data.length === 0) return

    target.useMediaCoordinateSpace(({ context: ctx }) => {
      // Convert all points to pixel coordinates
      const pixels: { x: number; yA: number; yB: number }[] = []
      for (const d of this._data) {
        const x = chart.timeScale().timeToCoordinate(d.time)
        const yA = series.priceToCoordinate(d.senkouA)
        const yB = series.priceToCoordinate(d.senkouB)
        if (x === null || yA === null || yB === null) continue
        pixels.push({ x, yA, yB })
      }

      if (pixels.length < 2) return

      // Draw segments, switching color when the relationship between A and B changes
      let segStart = 0
      for (let i = 1; i <= pixels.length; i++) {
        const prevBull = this._data[segStart] ? this._data[segStart].senkouA >= this._data[segStart].senkouB : true
        const currBull = i < this._data.length ? this._data[i]?.senkouA >= this._data[i]?.senkouB : prevBull
        const isLast = i === pixels.length

        if (isLast || currBull !== prevBull) {
          const seg = pixels.slice(segStart, i + (isLast ? 0 : 1))
          if (seg.length >= 2) {
            ctx.beginPath()
            ctx.moveTo(seg[0].x, seg[0].yA)
            for (let j = 1; j < seg.length; j++) ctx.lineTo(seg[j].x, seg[j].yA)
            for (let j = seg.length - 1; j >= 0; j--) ctx.lineTo(seg[j].x, seg[j].yB)
            ctx.closePath()
            ctx.fillStyle = prevBull ? this._bullColor : this._bearColor
            ctx.fill()
          }
          segStart = i
        }
      }
    })
  }
}

class KumoFillPrimitive implements ISeriesPrimitive<Time> {
  private _renderer = new KumoFillRenderer()
  private _paneViews: IPrimitivePaneView[]
  private _series: ISeriesApi<SeriesType, Time> | null = null
  private _chart: IChartApi | null = null

  constructor() {
    const renderer = this._renderer
    this._paneViews = [{
      zOrder: () => 'bottom' as const,
      renderer: () => renderer,
    }]
  }

  setData(data: KumoPoint[]) {
    if (this._series && this._chart) {
      this._renderer.update(data, this._series, this._chart)
    }
  }

  attached(param: SeriesAttachedParameter<Time, SeriesType>) {
    this._series = param.series
    this._chart = param.chart
  }

  detached() {
    this._series = null
    this._chart = null
  }

  updateAllViews() {
    // renderer already has references, will use latest data on next draw
  }

  paneViews(): readonly IPrimitivePaneView[] {
    return this._paneViews
  }
}

export function CandlestickChart({ symbolId }: CandlestickChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const seriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const lineSeriesRefs = useRef<Partial<Record<MALineKey, ISeriesApi<'Line'>>>>({})
  const bbSeriesRefs = useRef<Partial<Record<BBLineKey, ISeriesApi<'Line'>>>>({})
  const bbFillRefs = useRef<Partial<Record<BBKey, BollingerBandFillPrimitive>>>({})
  const ichimokuSeriesRefs = useRef<Partial<Record<IchimokuLineKey, ISeriesApi<'Line'>>>>({})
  const kumoFillRef = useRef<KumoFillPrimitive | null>(null)
  const prevCandleCountRef = useRef(0)

  const [interval, setInterval] = useState<CandleInterval>('PT15M')
  const { data, isFetching, hasNextPage, fetchNextPage, isFetchingNextPage } = useCandles(symbolId, interval)

  // Flatten all pages into a single sorted (oldest→newest) candle array
  const candles = useMemo(() => {
    if (!data?.pages) return []
    // Pages: page 0 = newest, page 1 = older, etc.
    // Each page is already sorted oldest→newest from API.
    // Concat older pages first, then newer.
    const all = [...data.pages].reverse().flat()
    // Deduplicate by time (in case of overlap from refetch)
    const seen = new Set<number>()
    return all.filter((c) => {
      if (seen.has(c.time)) return false
      seen.add(c.time)
      return true
    })
  }, [data])

  const [visible, setVisible] = useState<Record<MALineKey, boolean>>({
    sma20: true,
    sma50: true,
    sma100: false,
    sma200: false,
    ema12: false,
    ema26: false,
  })

  const [bbVisible, setBBVisible] = useState<Record<BBKey, boolean>>({
    bb1: false,
    bb2: true,
    bb3: false,
  })

  const [ichimokuVisible, setIchimokuVisible] = useState(false)

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

    const series = chart.addSeries(CandlestickSeries, {
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
      lineSeriesRefs.current = {}
      bbSeriesRefs.current = {}
      bbFillRefs.current = {}
      ichimokuSeriesRefs.current = {}
      kumoFillRef.current = null
      prevCandleCountRef.current = 0
      chart.remove()
    }
  }, [])

  // Infinite scroll: detect when user is near the left (oldest) edge
  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return

    const onVisibleRangeChange = () => {
      const range = chart.timeScale().getVisibleLogicalRange()
      if (!range) return
      if (range.from <= SCROLL_THRESHOLD && hasNextPage && !isFetchingNextPage) {
        fetchNextPage()
      }
    }

    chart.timeScale().subscribeVisibleLogicalRangeChange(onVisibleRangeChange)
    return () => {
      chart.timeScale().unsubscribeVisibleLogicalRangeChange(onVisibleRangeChange)
    }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  // Update chart data — preserve scroll position when prepending older candles
  useEffect(() => {
    if (!seriesRef.current || !chartRef.current || candles.length === 0) return

    const chart = chartRef.current
    const prevCount = prevCandleCountRef.current
    const prepended = candles.length > prevCount && prevCount > 0

    // Save the visible time range before updating data.
    // Time-based range is stable even when logical indices shift due to prepend.
    const visibleTimeRange = prepended ? chart.timeScale().getVisibleRange() : null

    const chartData: CandlestickData<Time>[] = candles.map((c) => ({
      time: (Math.floor(c.time / 1000)) as Time,
      open: c.open,
      high: c.high,
      low: c.low,
      close: c.close,
    }))

    seriesRef.current.setData(chartData)
    prevCandleCountRef.current = candles.length

    if (visibleTimeRange) {
      // Restore the same time window after prepending older candles
      chart.timeScale().setVisibleRange(visibleTimeRange)
    } else if (prevCount === 0) {
      // First load — fit all content
      chart.timeScale().fitContent()
    }
    // On refetch with no new candles, do nothing — keep current scroll position
  }, [candles])

  // Manage MA line series based on toggle state
  useEffect(() => {
    const chart = chartRef.current
    if (!chart || candles.length === 0) return

    const closes = candles.map((c) => c.close)
    const times = candles.map((c) => Math.floor(c.time / 1000) as Time)

    const computeMA = (key: MALineKey): (number | null)[] => {
      switch (key) {
        case 'sma20': return calcSMA(closes, 20)
        case 'sma50': return calcSMA(closes, 50)
        case 'sma100': return calcSMA(closes, 100)
        case 'sma200': return calcSMA(closes, 200)
        case 'ema12': return calcEMA(closes, 12)
        case 'ema26': return calcEMA(closes, 26)
      }
    }

    for (const key of Object.keys(MA_CONFIG) as MALineKey[]) {
      const existing = lineSeriesRefs.current[key]

      if (visible[key]) {
        const values = computeMA(key)
        const lineData: LineData<Time>[] = []
        for (let i = 0; i < values.length; i++) {
          if (values[i] !== null) {
            lineData.push({ time: times[i], value: values[i]! })
          }
        }

        if (existing) {
          existing.setData(lineData)
        } else {
          const lineSeries = chart.addSeries(LineSeries, {
            color: MA_CONFIG[key].color,
            lineWidth: 1,
            priceLineVisible: false,
            lastValueVisible: false,
          })
          lineSeries.setData(lineData)
          lineSeriesRefs.current[key] = lineSeries
        }
      } else if (existing) {
        chart.removeSeries(existing)
        delete lineSeriesRefs.current[key]
      }
    }
  }, [visible, candles])

  // Manage Bollinger Band line series and fill primitives based on toggle state
  useEffect(() => {
    const chart = chartRef.current
    const candleSeries = seriesRef.current
    if (!chart || !candleSeries || candles.length === 0) return

    const closes = candles.map((c) => c.close)
    const times = candles.map((c) => Math.floor(c.time / 1000) as Time)

    for (const key of Object.keys(BB_CONFIG) as BBKey[]) {
      const cfg = BB_CONFIG[key]
      const upperKey: BBLineKey = `${key}Upper`
      const lowerKey: BBLineKey = `${key}Lower`
      const existingUpper = bbSeriesRefs.current[upperKey]
      const existingLower = bbSeriesRefs.current[lowerKey]

      if (bbVisible[key]) {
        const { upper, lower } = calcBollingerBands(closes, 20, cfg.multiplier)

        const upperData: LineData<Time>[] = []
        const lowerData: LineData<Time>[] = []
        const fillData: BandPoint[] = []
        for (let i = 0; i < upper.length; i++) {
          if (upper[i] !== null && lower[i] !== null) {
            upperData.push({ time: times[i], value: upper[i]! })
            lowerData.push({ time: times[i], value: lower[i]! })
            fillData.push({ time: times[i], upper: upper[i]!, lower: lower[i]! })
          }
        }

        const lineOpts = {
          color: cfg.color,
          lineWidth: 1 as const,
          lineStyle: 2 as const, // dashed
          priceLineVisible: false,
          lastValueVisible: false,
          crosshairMarkerVisible: false,
        }

        if (existingUpper) {
          existingUpper.setData(upperData)
        } else {
          const s = chart.addSeries(LineSeries, lineOpts)
          s.setData(upperData)
          bbSeriesRefs.current[upperKey] = s
        }

        if (existingLower) {
          existingLower.setData(lowerData)
        } else {
          const s = chart.addSeries(LineSeries, lineOpts)
          s.setData(lowerData)
          bbSeriesRefs.current[lowerKey] = s
        }

        // Fill primitive
        let fill = bbFillRefs.current[key]
        if (!fill) {
          fill = new BollingerBandFillPrimitive(cfg.fillColor)
          candleSeries.attachPrimitive(fill as ISeriesPrimitive<Time>)
          bbFillRefs.current[key] = fill
        }
        fill.setData(fillData)
      } else {
        if (existingUpper) {
          chart.removeSeries(existingUpper)
          delete bbSeriesRefs.current[upperKey]
        }
        if (existingLower) {
          chart.removeSeries(existingLower)
          delete bbSeriesRefs.current[lowerKey]
        }
        const fill = bbFillRefs.current[key]
        if (fill) {
          candleSeries.detachPrimitive(fill as ISeriesPrimitive<Time>)
          delete bbFillRefs.current[key]
        }
      }
    }
  }, [bbVisible, candles])

  // Manage Ichimoku Kinko Hyo lines and kumo fill
  useEffect(() => {
    const chart = chartRef.current
    const candleSeries = seriesRef.current
    if (!chart || !candleSeries || candles.length === 0) return

    if (ichimokuVisible) {
      const highs = candles.map((c) => c.high)
      const lows = candles.map((c) => c.low)
      const closes = candles.map((c) => c.close)
      const times = candles.map((c) => Math.floor(c.time / 1000) as Time)

      const ichi = calcIchimoku(highs, lows, closes)

      // Generate future times for senkou spans (26 periods ahead)
      // Estimate interval from last two candles
      const lastTime = candles[candles.length - 1].time
      const prevTime = candles.length > 1 ? candles[candles.length - 2].time : lastTime - 60000
      const candleIntervalMs = lastTime - prevTime
      const futureTimes: Time[] = []
      for (let i = 1; i <= 26; i++) {
        futureTimes.push(Math.floor((lastTime + candleIntervalMs * i) / 1000) as Time)
      }
      const allSenkouTimes = [...times, ...futureTimes]

      // Build line data for each component
      const lineConfigs: { key: IchimokuLineKey; values: (number | null)[]; timesArr: Time[] }[] = [
        { key: 'tenkan', values: ichi.tenkan, timesArr: times },
        { key: 'kijun', values: ichi.kijun, timesArr: times },
        { key: 'senkouA', values: ichi.senkouA, timesArr: allSenkouTimes },
        { key: 'senkouB', values: ichi.senkouB, timesArr: allSenkouTimes },
        { key: 'chikou', values: ichi.chikou, timesArr: times },
      ]

      for (const { key, values, timesArr } of lineConfigs) {
        const lineData: LineData<Time>[] = []
        for (let i = 0; i < values.length; i++) {
          if (values[i] !== null && i < timesArr.length) {
            lineData.push({ time: timesArr[i], value: values[i]! })
          }
        }

        const existing = ichimokuSeriesRefs.current[key]
        if (existing) {
          existing.setData(lineData)
        } else {
          const cfg = ICHIMOKU_CONFIG[key]
          const s = chart.addSeries(LineSeries, {
            color: cfg.color,
            lineWidth: 1,
            lineStyle: key === 'chikou' ? 2 : 0, // chikou is dashed
            priceLineVisible: false,
            lastValueVisible: false,
            crosshairMarkerVisible: false,
          })
          s.setData(lineData)
          ichimokuSeriesRefs.current[key] = s
        }
      }

      // Kumo fill
      const kumoData: KumoPoint[] = []
      for (let i = 0; i < ichi.senkouA.length; i++) {
        if (ichi.senkouA[i] !== null && ichi.senkouB[i] !== null && i < allSenkouTimes.length) {
          kumoData.push({ time: allSenkouTimes[i], senkouA: ichi.senkouA[i]!, senkouB: ichi.senkouB[i]! })
        }
      }

      let kumoFill = kumoFillRef.current
      if (!kumoFill) {
        kumoFill = new KumoFillPrimitive()
        candleSeries.attachPrimitive(kumoFill as ISeriesPrimitive<Time>)
        kumoFillRef.current = kumoFill
      }
      kumoFill.setData(kumoData)
    } else {
      // Remove all ichimoku series
      for (const key of Object.keys(ICHIMOKU_CONFIG) as IchimokuLineKey[]) {
        const existing = ichimokuSeriesRefs.current[key]
        if (existing) {
          chart.removeSeries(existing)
          delete ichimokuSeriesRefs.current[key]
        }
      }
      const kumoFill = kumoFillRef.current
      if (kumoFill) {
        candleSeries.detachPrimitive(kumoFill as ISeriesPrimitive<Time>)
        kumoFillRef.current = null
      }
    }
  }, [ichimokuVisible, candles])

  const toggle = useCallback((key: MALineKey) => {
    setVisible((prev) => ({ ...prev, [key]: !prev[key] }))
  }, [])

  const toggleBB = useCallback((key: BBKey) => {
    setBBVisible((prev) => ({ ...prev, [key]: !prev[key] }))
  }, [])

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="mb-2 space-y-2">
        {/* Row 1: interval selector */}
        <div className="flex items-center gap-2">
          <div className="flex gap-1">
            {INTERVAL_OPTIONS.map((opt) => {
              const active = interval === opt.value
              return (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => setInterval(opt.value)}
                  className="rounded-md px-2.5 py-0.5 text-[11px] font-medium transition"
                  style={{
                    backgroundColor: active ? 'rgba(0, 212, 170, 0.18)' : 'rgba(255,255,255,0.06)',
                    color: active ? '#00d4aa' : '#94a3b8',
                    border: `1px solid ${active ? 'rgba(0, 212, 170, 0.45)' : 'rgba(255,255,255,0.1)'}`,
                  }}
                >
                  {opt.label}
                </button>
              )
            })}
          </div>
          {(isFetching || isFetchingNextPage) && (
            <span className="text-[10px] text-text-secondary">
              {isFetchingNextPage ? '過去データを読み込み中...' : '読み込み中...'}
            </span>
          )}
        </div>
        {/* Row 2: indicator toggles */}
        <div className="flex flex-wrap items-center gap-1.5">
          {(Object.keys(MA_CONFIG) as MALineKey[]).map((key) => (
            <button
              key={key}
              type="button"
              onClick={() => toggle(key)}
              className="rounded-full px-2.5 py-0.5 text-[11px] font-medium transition"
              style={{
                backgroundColor: visible[key] ? MA_CONFIG[key].color + '22' : 'rgba(255,255,255,0.06)',
                color: visible[key] ? MA_CONFIG[key].color : '#94a3b8',
                border: `1px solid ${visible[key] ? MA_CONFIG[key].color + '55' : 'rgba(255,255,255,0.1)'}`,
              }}
            >
              {MA_CONFIG[key].label}
            </button>
          ))}
          <span className="mx-0.5 self-center text-[10px] text-white/20">|</span>
          <button
            type="button"
            onClick={() => setIchimokuVisible((v) => !v)}
            className="rounded-full px-2.5 py-0.5 text-[11px] font-medium transition"
            style={{
              backgroundColor: ichimokuVisible ? 'rgba(33, 150, 243, 0.18)' : 'rgba(255,255,255,0.06)',
              color: ichimokuVisible ? '#2196f3' : '#94a3b8',
              border: `1px solid ${ichimokuVisible ? 'rgba(33, 150, 243, 0.45)' : 'rgba(255,255,255,0.1)'}`,
            }}
          >
            一目
          </button>
          <span className="mx-0.5 self-center text-[10px] text-white/20">|</span>
          {(Object.keys(BB_CONFIG) as BBKey[]).map((key) => (
            <button
              key={key}
              type="button"
              onClick={() => toggleBB(key)}
              className="rounded-full px-2.5 py-0.5 text-[11px] font-medium transition"
              style={{
                backgroundColor: bbVisible[key] ? BB_CONFIG[key].color + '22' : 'rgba(255,255,255,0.06)',
                color: bbVisible[key] ? BB_CONFIG[key].color : '#94a3b8',
                border: `1px solid ${bbVisible[key] ? BB_CONFIG[key].color + '55' : 'rgba(255,255,255,0.1)'}`,
              }}
            >
              {BB_CONFIG[key].label}
            </button>
          ))}
        </div>
      </div>
      <div ref={containerRef} />
      {candles.length > 0 && (
        <>
          <MACDChart candles={candles} />
          <RSIChart candles={candles} />
          <StochasticsChart candles={candles} />
        </>
      )}
    </div>
  )
}
