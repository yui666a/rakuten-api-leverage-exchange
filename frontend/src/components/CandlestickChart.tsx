import { useEffect, useRef, useState, useCallback } from 'react'
import { createChart, CandlestickSeries, LineSeries, type IChartApi, type ISeriesApi, type CandlestickData, type LineData, type Time } from 'lightweight-charts'
import { useCandles, type CandleInterval } from '../hooks/useCandles'

type CandlestickChartProps = {
  symbolId: number
}

type MALineKey = 'sma20' | 'sma50' | 'ema12' | 'ema26'

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

export function CandlestickChart({ symbolId }: CandlestickChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const seriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const lineSeriesRefs = useRef<Partial<Record<MALineKey, ISeriesApi<'Line'>>>>({})

  const [interval, setInterval] = useState<CandleInterval>('PT15M')
  const { data: candlesData, isFetching } = useCandles(symbolId, interval)
  const candles = candlesData ?? []

  const [visible, setVisible] = useState<Record<MALineKey, boolean>>({
    sma20: false,
    sma50: false,
    ema12: false,
    ema26: false,
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
      chart.remove()
    }
  }, [])

  useEffect(() => {
    if (!seriesRef.current || candles.length === 0) return

    const data: CandlestickData<Time>[] = candles.map((c) => ({
      time: (Math.floor(c.time / 1000)) as Time,
      open: c.open,
      high: c.high,
      low: c.low,
      close: c.close,
    }))

    seriesRef.current.setData(data)
    chartRef.current?.timeScale().fitContent()
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

  const toggle = useCallback((key: MALineKey) => {
    setVisible((prev) => ({ ...prev, [key]: !prev[key] }))
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
          {isFetching && <span className="text-[10px] text-text-secondary">読み込み中...</span>}
        </div>
        <div className="flex gap-1.5">
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
        </div>
      </div>
      <div ref={containerRef} />
    </div>
  )
}
