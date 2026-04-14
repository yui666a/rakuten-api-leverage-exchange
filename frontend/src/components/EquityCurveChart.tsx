import { useEffect, useRef, useMemo } from 'react'
import {
  createChart,
  BaselineSeries,
  createSeriesMarkers,
  LineStyle,
  type IChartApi,
  type BaselineData,
  type SeriesMarker,
  type Time,
} from 'lightweight-charts'
import type { BacktestTrade } from '../lib/api'

type EquityCurveChartProps = {
  trades: BacktestTrade[]
  initialBalance: number
  periodFrom: number // unix ms
  periodTo: number // unix ms
}

/** Convert unix milliseconds to lightweight-charts Time (seconds). */
function toChartTime(ms: number): Time {
  return Math.floor(ms / 1000) as unknown as Time
}

export function EquityCurveChart({
  trades,
  initialBalance,
  periodFrom,
  periodTo,
}: EquityCurveChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)

  // Sort trades by exit time and compute equity curve
  const { equityData, markers } = useMemo(() => {
    const sorted = [...trades].sort((a, b) => a.exitTime - b.exitTime)

    // Build equity data points: start with initial balance, then accumulate PnL at each exit
    const points: BaselineData<Time>[] = []

    // Starting point
    points.push({ time: toChartTime(periodFrom), value: initialBalance })

    let equity = initialBalance
    for (const trade of sorted) {
      equity += trade.pnl
      points.push({ time: toChartTime(trade.exitTime), value: equity })
    }

    // End point if last trade exit is before periodTo
    if (sorted.length > 0 && sorted[sorted.length - 1].exitTime < periodTo) {
      points.push({ time: toChartTime(periodTo), value: equity })
    }

    // Deduplicate by time (keep last value for each timestamp)
    const seen = new Map<number, number>()
    for (let i = 0; i < points.length; i++) {
      seen.set(points[i].time as unknown as number, i)
    }
    const deduped = [...seen.values()].sort((a, b) => a - b).map((i) => points[i])

    // Build markers for entries and exits
    const mkrs: SeriesMarker<Time>[] = []
    for (const trade of sorted) {
      // Entry marker
      mkrs.push({
        time: toChartTime(trade.entryTime),
        position: 'belowBar',
        shape: trade.side === 'BUY' ? 'arrowUp' : 'arrowDown',
        color: trade.side === 'BUY' ? '#00d4aa' : '#ff4757',
        text: trade.side === 'BUY' ? 'BUY' : 'SELL',
        size: 0.8,
      })
      // Exit marker
      mkrs.push({
        time: toChartTime(trade.exitTime),
        position: 'aboveBar',
        shape: 'circle',
        color: trade.pnl >= 0 ? '#00d4aa' : '#ff4757',
        text: 'EXIT',
        size: 0.6,
      })
    }
    // Sort markers by time (required by lightweight-charts)
    mkrs.sort(
      (a, b) => (a.time as unknown as number) - (b.time as unknown as number),
    )

    return { equityData: deduped, markers: mkrs }
  }, [trades, initialBalance, periodFrom, periodTo])

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
      height: containerRef.current.clientHeight || 400,
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
      },
      rightPriceScale: {
        borderColor: '#2a2a4e',
      },
    })

    chartRef.current = chart

    // Baseline series: green above initial balance, red below
    const series = chart.addSeries(BaselineSeries, {
      baseValue: { type: 'price', price: initialBalance },
      topLineColor: '#00d4aa',
      topFillColor1: 'rgba(0, 212, 170, 0.28)',
      topFillColor2: 'rgba(0, 212, 170, 0.05)',
      bottomLineColor: '#ff4757',
      bottomFillColor1: 'rgba(255, 71, 87, 0.05)',
      bottomFillColor2: 'rgba(255, 71, 87, 0.28)',
      lineWidth: 2,
      priceLineVisible: false,
      lastValueVisible: true,
    })

    series.setData(equityData)

    // Horizontal dashed line at initial balance
    series.createPriceLine({
      price: initialBalance,
      color: 'rgba(255, 255, 255, 0.3)',
      lineWidth: 1,
      lineStyle: LineStyle.Dashed,
      axisLabelVisible: true,
      title: 'Initial',
    })

    // Trade markers
    if (markers.length > 0) {
      createSeriesMarkers(series, markers)
    }

    chart.timeScale().fitContent()

    // ResizeObserver for responsive width
    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const { width, height } = entry.contentRect
        chart.applyOptions({ width, height: height || 400 })
      }
    })
    resizeObserver.observe(containerRef.current)

    return () => {
      resizeObserver.disconnect()
      chart.remove()
      chartRef.current = null
    }
  }, [equityData, markers, initialBalance])

  return <div ref={containerRef} className="h-full w-full" />
}
