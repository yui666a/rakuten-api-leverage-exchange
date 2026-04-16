import { useEffect, useRef } from 'react'
import { createChart, LineSeries, HistogramSeries, type IChartApi, type ISeriesApi, type LineData, type HistogramData, type Time } from 'lightweight-charts'
import type { Candle } from '../lib/api'

type MACDChartProps = {
  candles: Candle[]
}

function calcEMA(values: number[], period: number): (number | null)[] {
  const result: (number | null)[] = []
  const k = 2 / (period + 1)
  for (let i = 0; i < values.length; i++) {
    if (i < period - 1) {
      result.push(null)
    } else if (i === period - 1) {
      let sum = 0
      for (let j = 0; j < period; j++) sum += values[j]
      result.push(sum / period)
    } else {
      const prev = result[i - 1]!
      result.push((values[i] - prev) * k + prev)
    }
  }
  return result
}

function calcMACD(closes: number[]): {
  macdLine: (number | null)[]
  signalLine: (number | null)[]
  histogram: (number | null)[]
} {
  const ema12 = calcEMA(closes, 12)
  const ema26 = calcEMA(closes, 26)

  // MACD line = EMA(12) - EMA(26)
  const macdLine: (number | null)[] = []
  for (let i = 0; i < closes.length; i++) {
    if (ema12[i] !== null && ema26[i] !== null) {
      macdLine.push(ema12[i]! - ema26[i]!)
    } else {
      macdLine.push(null)
    }
  }

  // Signal line = EMA(9) of MACD line
  // We need to compute EMA on non-null MACD values
  const macdValues: number[] = []
  const macdIndices: number[] = []
  for (let i = 0; i < macdLine.length; i++) {
    if (macdLine[i] !== null) {
      macdValues.push(macdLine[i]!)
      macdIndices.push(i)
    }
  }

  const signalEma = calcEMA(macdValues, 9)
  const signalLine: (number | null)[] = new Array(closes.length).fill(null)
  for (let i = 0; i < signalEma.length; i++) {
    signalLine[macdIndices[i]] = signalEma[i]
  }

  // Histogram = MACD - Signal
  const histogram: (number | null)[] = []
  for (let i = 0; i < closes.length; i++) {
    if (macdLine[i] !== null && signalLine[i] !== null) {
      histogram.push(macdLine[i]! - signalLine[i]!)
    } else {
      histogram.push(null)
    }
  }

  return { macdLine, signalLine, histogram }
}

export function MACDChart({ candles }: MACDChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const macdSeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const signalSeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const histSeriesRef = useRef<ISeriesApi<'Histogram'> | null>(null)

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
      height: 150,
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
      },
      rightPriceScale: {
        scaleMargins: { top: 0.1, bottom: 0.1 },
      },
    })

    const histSeries = chart.addSeries(HistogramSeries, {
      priceLineVisible: false,
      lastValueVisible: false,
    })

    const macdSeries = chart.addSeries(LineSeries, {
      color: '#2196f3',
      lineWidth: 1,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    const signalSeries = chart.addSeries(LineSeries, {
      color: '#ff5722',
      lineWidth: 1,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    chartRef.current = chart
    macdSeriesRef.current = macdSeries
    signalSeriesRef.current = signalSeries
    histSeriesRef.current = histSeries

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
    if (!chartRef.current || !macdSeriesRef.current || !signalSeriesRef.current || !histSeriesRef.current || candles.length === 0) return

    const closes = candles.map((c) => c.close)
    const times = candles.map((c) => Math.floor(c.time / 1000) as Time)
    const { macdLine, signalLine, histogram } = calcMACD(closes)

    const macdData: LineData<Time>[] = []
    const signalData: LineData<Time>[] = []
    const histData: HistogramData<Time>[] = []

    for (let i = 0; i < closes.length; i++) {
      if (macdLine[i] !== null) {
        macdData.push({ time: times[i], value: macdLine[i]! })
      }
      if (signalLine[i] !== null) {
        signalData.push({ time: times[i], value: signalLine[i]! })
      }
      if (histogram[i] !== null) {
        const val = histogram[i]!
        histData.push({
          time: times[i],
          value: val,
          color: val >= 0 ? 'rgba(0, 212, 170, 0.5)' : 'rgba(255, 71, 87, 0.5)',
        })
      }
    }

    macdSeriesRef.current.setData(macdData)
    signalSeriesRef.current.setData(signalData)
    histSeriesRef.current.setData(histData)
    chartRef.current.timeScale().fitContent()
  }, [candles])

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="mb-1 flex items-center gap-2">
        <span className="text-[11px] font-medium text-text-secondary">MACD</span>
        <span className="text-[10px] text-text-secondary/60">(12, 26, 9)</span>
        <div className="ml-auto flex items-center gap-3 text-[10px]">
          <span className="flex items-center gap-1"><span className="inline-block h-0.5 w-3 rounded" style={{ backgroundColor: '#2196f3' }} />MACD</span>
          <span className="flex items-center gap-1"><span className="inline-block h-0.5 w-3 rounded" style={{ backgroundColor: '#ff5722' }} />Signal</span>
          <span className="flex items-center gap-1"><span className="inline-block h-2.5 w-2 rounded-sm" style={{ backgroundColor: 'rgba(0, 212, 170, 0.5)' }} />Histogram</span>
        </div>
      </div>
      <div ref={containerRef} />
    </div>
  )
}
