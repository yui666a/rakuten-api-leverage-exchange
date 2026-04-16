import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { createChart, CandlestickSeries, LineSeries, type IChartApi, type ISeriesApi, type CandlestickData, type LineData, type Time, type SeriesType, type ISeriesPrimitive, type SeriesAttachedParameter, type IPrimitivePaneView, type IPrimitivePaneRenderer } from 'lightweight-charts'
import type { CanvasRenderingTarget2D } from 'fancy-canvas'
import { useCandles, type CandleInterval } from '../hooks/useCandles'

type CandlestickChartProps = {
  symbolId: number
}

type MALineKey = 'sma20' | 'sma50' | 'ema12' | 'ema26'
type BBKey = 'bb1' | 'bb2' | 'bb3'
type BBLineKey = `${BBKey}Upper` | `${BBKey}Lower`

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
  ema12: { label: 'EMA(12)', color: '#00bfff' },
  ema26: { label: 'EMA(26)', color: '#a78bfa' },
}

const BB_CONFIG: Record<BBKey, { label: string; color: string; fillColor: string; multiplier: number }> = {
  bb1: { label: 'BB(1σ)', color: '#4fc3f7', fillColor: 'rgba(79, 195, 247, 0.12)', multiplier: 1 },
  bb2: { label: 'BB(2σ)', color: '#7c4dff', fillColor: 'rgba(124, 77, 255, 0.10)', multiplier: 2 },
  bb3: { label: 'BB(3σ)', color: '#ff6e40', fillColor: 'rgba(255, 110, 64, 0.08)', multiplier: 3 },
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

export function CandlestickChart({ symbolId }: CandlestickChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const seriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const lineSeriesRefs = useRef<Partial<Record<MALineKey, ISeriesApi<'Line'>>>>({})
  const bbSeriesRefs = useRef<Partial<Record<BBLineKey, ISeriesApi<'Line'>>>>({})
  const bbFillRefs = useRef<Partial<Record<BBKey, BollingerBandFillPrimitive>>>({})
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
    ema12: false,
    ema26: false,
  })

  const [bbVisible, setBBVisible] = useState<Record<BBKey, boolean>>({
    bb1: false,
    bb2: true,
    bb3: false,
  })

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

  const toggle = useCallback((key: MALineKey) => {
    setVisible((prev) => ({ ...prev, [key]: !prev[key] }))
  }, [])

  const toggleBB = useCallback((key: BBKey) => {
    setBBVisible((prev) => ({ ...prev, [key]: !prev[key] }))
  }, [])

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
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
        <div className="flex flex-wrap gap-1.5">
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
    </div>
  )
}
