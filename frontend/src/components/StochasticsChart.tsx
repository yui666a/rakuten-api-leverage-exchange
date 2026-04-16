import { useEffect, useRef } from 'react'
import { createChart, LineSeries, type IChartApi, type ISeriesApi, type LineData, type Time } from 'lightweight-charts'
import type { Candle } from '../lib/api'

type StochasticsChartProps = {
  candles: Candle[]
}

function calcStochastics(
  highs: number[],
  lows: number[],
  closes: number[],
  kPeriod: number,
  dPeriod: number,
): { percentK: (number | null)[]; percentD: (number | null)[] } {
  const len = closes.length
  const percentK: (number | null)[] = []

  // %K = (Close - Lowest Low) / (Highest High - Lowest Low) * 100
  for (let i = 0; i < len; i++) {
    if (i < kPeriod - 1) {
      percentK.push(null)
    } else {
      let maxH = -Infinity
      let minL = Infinity
      for (let j = i - kPeriod + 1; j <= i; j++) {
        if (highs[j] > maxH) maxH = highs[j]
        if (lows[j] < minL) minL = lows[j]
      }
      const range = maxH - minL
      percentK.push(range === 0 ? 50 : ((closes[i] - minL) / range) * 100)
    }
  }

  // %D = SMA of %K over dPeriod
  const percentD: (number | null)[] = []
  for (let i = 0; i < len; i++) {
    if (percentK[i] === null) {
      percentD.push(null)
      continue
    }
    let sum = 0
    let count = 0
    for (let j = i; j >= 0 && count < dPeriod; j--) {
      if (percentK[j] !== null) {
        sum += percentK[j]!
        count++
      } else {
        break
      }
    }
    percentD.push(count >= dPeriod ? sum / dPeriod : null)
  }

  return { percentK, percentD }
}

export function StochasticsChart({ candles }: StochasticsChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const kSeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const dSeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const overboughtRef = useRef<ISeriesApi<'Line'> | null>(null)
  const oversoldRef = useRef<ISeriesApi<'Line'> | null>(null)

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

    const kSeries = chart.addSeries(LineSeries, {
      color: '#00bfff',
      lineWidth: 1,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    const dSeries = chart.addSeries(LineSeries, {
      color: '#ff6e40',
      lineWidth: 1,
      lineStyle: 2,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    const overbought = chart.addSeries(LineSeries, {
      color: 'rgba(255, 71, 87, 0.4)',
      lineWidth: 1,
      lineStyle: 2,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    const oversold = chart.addSeries(LineSeries, {
      color: 'rgba(0, 212, 170, 0.4)',
      lineWidth: 1,
      lineStyle: 2,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    chartRef.current = chart
    kSeriesRef.current = kSeries
    dSeriesRef.current = dSeries
    overboughtRef.current = overbought
    oversoldRef.current = oversold

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
    if (!chartRef.current || !kSeriesRef.current || !dSeriesRef.current || !overboughtRef.current || !oversoldRef.current || candles.length === 0) return

    const highs = candles.map((c) => c.high)
    const lows = candles.map((c) => c.low)
    const closes = candles.map((c) => c.close)
    const times = candles.map((c) => Math.floor(c.time / 1000) as Time)
    const { percentK, percentD } = calcStochastics(highs, lows, closes, 14, 3)

    const kData: LineData<Time>[] = []
    const dData: LineData<Time>[] = []

    for (let i = 0; i < closes.length; i++) {
      if (percentK[i] !== null) {
        kData.push({ time: times[i], value: percentK[i]! })
      }
      if (percentD[i] !== null) {
        dData.push({ time: times[i], value: percentD[i]! })
      }
    }

    kSeriesRef.current.setData(kData)
    dSeriesRef.current.setData(dData)

    if (kData.length >= 2) {
      const firstTime = kData[0].time
      const lastTime = kData[kData.length - 1].time
      overboughtRef.current.setData([
        { time: firstTime, value: 80 },
        { time: lastTime, value: 80 },
      ])
      oversoldRef.current.setData([
        { time: firstTime, value: 20 },
        { time: lastTime, value: 20 },
      ])
    }

    chartRef.current.timeScale().fitContent()
  }, [candles])

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="mb-1 flex items-center gap-2">
        <span className="text-[11px] font-medium text-text-secondary">Stochastics</span>
        <span className="text-[10px] text-text-secondary/60">(14, 3)</span>
        <div className="ml-auto flex items-center gap-3 text-[10px]">
          <span className="flex items-center gap-1"><span className="inline-block h-0.5 w-3 rounded" style={{ backgroundColor: '#00bfff' }} />%K</span>
          <span className="flex items-center gap-1"><span className="inline-block h-0.5 w-3 rounded" style={{ backgroundColor: '#ff6e40' }} />%D</span>
          <span className="text-text-secondary/50">80 / 20</span>
        </div>
      </div>
      <div ref={containerRef} />
    </div>
  )
}
