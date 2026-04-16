import { useEffect, useRef } from 'react'
import { createChart, LineSeries, type IChartApi, type ISeriesApi, type LineData, type Time } from 'lightweight-charts'
import type { Candle } from '../lib/api'

type RSIChartProps = {
  candles: Candle[]
}

function calcRSI(closes: number[], period: number): (number | null)[] {
  const result: (number | null)[] = []
  if (closes.length < period + 1) {
    return new Array(closes.length).fill(null)
  }

  let avgGain = 0
  let avgLoss = 0

  // Initial average over first `period` changes
  for (let i = 1; i <= period; i++) {
    const change = closes[i] - closes[i - 1]
    if (change >= 0) avgGain += change
    else avgLoss -= change
  }
  avgGain /= period
  avgLoss /= period

  // Fill nulls for first period entries
  for (let i = 0; i <= period; i++) {
    result.push(i < period ? null : (avgLoss === 0 ? 100 : 100 - 100 / (1 + avgGain / avgLoss)))
  }

  // Smoothed RSI for remaining
  for (let i = period + 1; i < closes.length; i++) {
    const change = closes[i] - closes[i - 1]
    const gain = change >= 0 ? change : 0
    const loss = change < 0 ? -change : 0
    avgGain = (avgGain * (period - 1) + gain) / period
    avgLoss = (avgLoss * (period - 1) + loss) / period
    result.push(avgLoss === 0 ? 100 : 100 - 100 / (1 + avgGain / avgLoss))
  }

  return result
}

export function RSIChart({ candles }: RSIChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const rsiSeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const overboughtRef = useRef<ISeriesApi<'Line'> | null>(null)
  const oversoldRef = useRef<ISeriesApi<'Line'> | null>(null)
  const midRef = useRef<ISeriesApi<'Line'> | null>(null)

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
      height: 120,
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
      },
      rightPriceScale: {
        scaleMargins: { top: 0.05, bottom: 0.05 },
      },
    })

    const rsiSeries = chart.addSeries(LineSeries, {
      color: '#a78bfa',
      lineWidth: 1,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    // Overbought line (70)
    const overbought = chart.addSeries(LineSeries, {
      color: 'rgba(255, 71, 87, 0.4)',
      lineWidth: 1,
      lineStyle: 2,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    // Oversold line (30)
    const oversold = chart.addSeries(LineSeries, {
      color: 'rgba(0, 212, 170, 0.4)',
      lineWidth: 1,
      lineStyle: 2,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    // Middle line (50)
    const mid = chart.addSeries(LineSeries, {
      color: 'rgba(255, 255, 255, 0.15)',
      lineWidth: 1,
      lineStyle: 2,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    chartRef.current = chart
    rsiSeriesRef.current = rsiSeries
    overboughtRef.current = overbought
    oversoldRef.current = oversold
    midRef.current = mid

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
    if (!chartRef.current || !rsiSeriesRef.current || !overboughtRef.current || !oversoldRef.current || !midRef.current || candles.length === 0) return

    const closes = candles.map((c) => c.close)
    const times = candles.map((c) => Math.floor(c.time / 1000) as Time)
    const rsi = calcRSI(closes, 14)

    const rsiData: LineData<Time>[] = []
    for (let i = 0; i < rsi.length; i++) {
      if (rsi[i] !== null) {
        rsiData.push({ time: times[i], value: rsi[i]! })
      }
    }

    rsiSeriesRef.current.setData(rsiData)

    // Reference lines — only need first and last time
    if (rsiData.length >= 2) {
      const firstTime = rsiData[0].time
      const lastTime = rsiData[rsiData.length - 1].time
      overboughtRef.current.setData([
        { time: firstTime, value: 70 },
        { time: lastTime, value: 70 },
      ])
      oversoldRef.current.setData([
        { time: firstTime, value: 30 },
        { time: lastTime, value: 30 },
      ])
      midRef.current.setData([
        { time: firstTime, value: 50 },
        { time: lastTime, value: 50 },
      ])
    }

    chartRef.current.timeScale().fitContent()
  }, [candles])

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="mb-1 flex items-center gap-2">
        <span className="text-[11px] font-medium text-text-secondary">RSI</span>
        <span className="text-[10px] text-text-secondary/60">(14)</span>
        <div className="ml-auto flex items-center gap-3 text-[10px]">
          <span className="flex items-center gap-1"><span className="inline-block h-0.5 w-3 rounded" style={{ backgroundColor: '#a78bfa' }} />RSI</span>
          <span className="text-text-secondary/50">70 / 50 / 30</span>
        </div>
      </div>
      <div ref={containerRef} />
    </div>
  )
}
