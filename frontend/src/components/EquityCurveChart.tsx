import { useEffect, useRef, useMemo, useCallback, useState } from 'react'
import {
  createChart,
  BaselineSeries,
  createSeriesMarkers,
  LineStyle,
  type IChartApi,
  type BaselineData,
  type SeriesMarker,
  type Time,
  type ISeriesMarkersPluginApi,
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

/** Max markers to render at once to keep the chart readable. */
const MAX_VISIBLE_MARKERS = 200

export function EquityCurveChart({
  trades,
  initialBalance,
  periodFrom,
  periodTo,
}: EquityCurveChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const markersPluginRef = useRef<ISeriesMarkersPluginApi<Time> | null>(null)
  const [showMarkers, setShowMarkers] = useState(true)

  // Sort trades once and compute equity curve + all markers
  const { equityData, allMarkers, sortedTrades } = useMemo(() => {
    const sorted = [...trades].sort((a, b) => a.exitTime - b.exitTime)

    // Build equity data points
    const points: BaselineData<Time>[] = []
    points.push({ time: toChartTime(periodFrom), value: initialBalance })

    let equity = initialBalance
    for (const trade of sorted) {
      equity += trade.pnl
      points.push({ time: toChartTime(trade.exitTime), value: equity })
    }

    if (sorted.length > 0 && sorted[sorted.length - 1].exitTime < periodTo) {
      points.push({ time: toChartTime(periodTo), value: equity })
    }

    // Deduplicate by time (keep last value for each timestamp)
    const seen = new Map<number, number>()
    for (let i = 0; i < points.length; i++) {
      seen.set(points[i].time as unknown as number, i)
    }
    const deduped = [...seen.values()]
      .sort((a, b) => a - b)
      .map((i) => points[i])

    // Build all markers (sorted by time)
    const mkrs: SeriesMarker<Time>[] = []
    for (const trade of sorted) {
      mkrs.push({
        time: toChartTime(trade.entryTime),
        position: 'belowBar',
        shape: trade.side === 'BUY' ? 'arrowUp' : 'arrowDown',
        color: trade.side === 'BUY' ? '#00d4aa' : '#ff4757',
        text: trade.side === 'BUY' ? 'BUY' : 'SELL',
        size: 0.8,
      })
      mkrs.push({
        time: toChartTime(trade.exitTime),
        position: 'aboveBar',
        shape: 'circle',
        color: trade.pnl >= 0 ? '#00d4aa' : '#ff4757',
        text: 'EXIT',
        size: 0.6,
      })
    }
    mkrs.sort(
      (a, b) => (a.time as unknown as number) - (b.time as unknown as number),
    )

    return { equityData: deduped, allMarkers: mkrs, sortedTrades: sorted }
  }, [trades, initialBalance, periodFrom, periodTo])

  // Filter markers to only those within a given time range, with a cap
  const updateVisibleMarkers = useCallback(
    (from: number, to: number) => {
      if (!markersPluginRef.current) return

      if (!showMarkers) {
        markersPluginRef.current.setMarkers([])
        return
      }

      const visible = allMarkers.filter((m) => {
        const t = m.time as unknown as number
        return t >= from && t <= to
      })

      if (visible.length <= MAX_VISIBLE_MARKERS) {
        markersPluginRef.current.setMarkers(visible)
      } else {
        // Downsample: evenly pick markers to stay under the cap
        const step = visible.length / MAX_VISIBLE_MARKERS
        const sampled: SeriesMarker<Time>[] = []
        for (let i = 0; i < MAX_VISIBLE_MARKERS; i++) {
          sampled.push(visible[Math.floor(i * step)])
        }
        markersPluginRef.current.setMarkers(sampled)
      }
    },
    [allMarkers, showMarkers],
  )

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

    series.createPriceLine({
      price: initialBalance,
      color: 'rgba(255, 255, 255, 0.3)',
      lineWidth: 1,
      lineStyle: LineStyle.Dashed,
      axisLabelVisible: true,
      title: 'Initial',
    })

    // Create markers plugin (initially empty — populated after visible range is set)
    const markersPlugin = createSeriesMarkers(series, [])
    markersPluginRef.current = markersPlugin

    // Set initial visible range to last 3 months
    const threeMonthsMs = 90 * 24 * 60 * 60
    const lastDataTimeSec =
      sortedTrades.length > 0
        ? Math.floor(sortedTrades[sortedTrades.length - 1].exitTime / 1000)
        : Math.floor(periodTo / 1000)
    const rangeFrom = lastDataTimeSec - threeMonthsMs
    const rangeTo = lastDataTimeSec

    chart
      .timeScale()
      .setVisibleRange({ from: rangeFrom as unknown as Time, to: rangeTo as unknown as Time })

    // Show markers for the initial range
    updateVisibleMarkers(rangeFrom, rangeTo)

    // Update markers dynamically on scroll / zoom
    const onVisibleRangeChange = () => {
      const range = chart.timeScale().getVisibleRange()
      if (!range) return
      updateVisibleMarkers(
        range.from as unknown as number,
        range.to as unknown as number,
      )
    }
    chart
      .timeScale()
      .subscribeVisibleTimeRangeChange(onVisibleRangeChange)

    // ResizeObserver for responsive width
    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const { width, height } = entry.contentRect
        chart.applyOptions({ width, height: height || 400 })
      }
    })
    resizeObserver.observe(containerRef.current)

    return () => {
      chart
        .timeScale()
        .unsubscribeVisibleTimeRangeChange(onVisibleRangeChange)
      resizeObserver.disconnect()
      chart.remove()
      chartRef.current = null
      markersPluginRef.current = null
    }
  }, [equityData, allMarkers, initialBalance, sortedTrades, periodTo, updateVisibleMarkers])

  // Re-apply markers when toggle changes
  useEffect(() => {
    if (!chartRef.current) return
    const range = chartRef.current.timeScale().getVisibleRange()
    if (!range) return
    updateVisibleMarkers(
      range.from as unknown as number,
      range.to as unknown as number,
    )
  }, [showMarkers, updateVisibleMarkers])

  return (
    <div className="flex h-full flex-col">
      <div className="mb-1 flex justify-end">
        <button
          type="button"
          onClick={() => setShowMarkers((v) => !v)}
          className="rounded-full px-2.5 py-0.5 text-[11px] font-medium transition"
          style={{
            backgroundColor: showMarkers ? 'rgba(0, 212, 170, 0.14)' : 'rgba(255,255,255,0.06)',
            color: showMarkers ? '#00d4aa' : '#94a3b8',
            border: `1px solid ${showMarkers ? 'rgba(0, 212, 170, 0.45)' : 'rgba(255,255,255,0.1)'}`,
          }}
        >
          Markers
        </button>
      </div>
      <div ref={containerRef} className="min-h-0 flex-1" />
    </div>
  )
}
