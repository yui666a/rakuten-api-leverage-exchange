import { useEffect, useRef } from 'react'
import { createChart, LineSeries, type IChartApi, type ISeriesApi, type LineData, type Time } from 'lightweight-charts'
import type { Candle } from '../lib/api'

type ADXChartProps = {
  candles: Candle[]
}

// calcADX is the FE-side companion to backend/internal/infrastructure/indicator/adx.go.
// It walks the full candle series once with Wilder-smoothed TR / +DM / -DM so the
// three plotted lines match the BE reading bar-for-bar when the BE indicator fires.
//
// Returns null-padded arrays so the returned slices align with `candles` by
// index — the consumer can push a LineData point whenever the value is not null.
function calcADX(
  highs: number[],
  lows: number[],
  closes: number[],
  period: number,
): { adx: (number | null)[]; plusDI: (number | null)[]; minusDI: (number | null)[] } {
  const n = closes.length
  const adx: (number | null)[] = new Array(n).fill(null)
  const plusDI: (number | null)[] = new Array(n).fill(null)
  const minusDI: (number | null)[] = new Array(n).fill(null)
  if (n < 2 * period + 1) return { adx, plusDI, minusDI }

  // Step 1: per-bar TR / +DM / -DM (index i refers to bar i+1's move from i).
  const trs: number[] = new Array(n - 1)
  const plusDMs: number[] = new Array(n - 1)
  const minusDMs: number[] = new Array(n - 1)
  for (let i = 1; i < n; i++) {
    const upMove = highs[i] - highs[i - 1]
    const downMove = lows[i - 1] - lows[i]
    let pDM = 0
    let mDM = 0
    if (upMove > downMove && upMove > 0) pDM = upMove
    if (downMove > upMove && downMove > 0) mDM = downMove
    plusDMs[i - 1] = pDM
    minusDMs[i - 1] = mDM
    const tr1 = highs[i] - lows[i]
    const tr2 = Math.abs(highs[i] - closes[i - 1])
    const tr3 = Math.abs(lows[i] - closes[i - 1])
    trs[i - 1] = Math.max(tr1, Math.max(tr2, tr3))
  }

  // Step 2: seed averages from the first `period` values.
  let sumTR = 0
  let sumP = 0
  let sumM = 0
  for (let i = 0; i < period; i++) {
    sumTR += trs[i]
    sumP += plusDMs[i]
    sumM += minusDMs[i]
  }
  let atr = sumTR / period
  let smP = sumP / period
  let smM = sumM / period

  const di = (dm: number, tr: number) => (tr <= 0 ? 0 : (100 * dm) / tr)
  const dx = (p: number, m: number) => {
    const denom = p + m
    if (denom <= 0) return 0
    return (100 * Math.abs(p - m)) / denom
  }

  // DX values collected to seed ADX.
  const dxValues: number[] = []
  dxValues.push(dx(di(smP, atr), di(smM, atr)))
  for (let i = period; i < trs.length; i++) {
    atr = (atr * (period - 1) + trs[i]) / period
    smP = (smP * (period - 1) + plusDMs[i]) / period
    smM = (smM * (period - 1) + minusDMs[i]) / period
    dxValues.push(dx(di(smP, atr), di(smM, atr)))
  }

  if (dxValues.length < period) return { adx, plusDI, minusDI }
  let seed = 0
  for (let i = 0; i < period; i++) seed += dxValues[i]
  let adxVal = seed / period

  // Fill output arrays. The first DX sits at candle index `period` (one bar
  // after the first per-bar TR), so the first ADX lands at index 2*period.
  // For earlier indices we also populate +DI / -DI where they have been
  // seeded (i.e. index >= period) to give a longer plot line.
  // Walk with a single pointer into the smoothed state — recomputing to stay
  // aligned with the seed pass above.
  sumTR = 0
  sumP = 0
  sumM = 0
  for (let i = 0; i < period; i++) {
    sumTR += trs[i]
    sumP += plusDMs[i]
    sumM += minusDMs[i]
  }
  atr = sumTR / period
  smP = sumP / period
  smM = sumM / period

  // +DI / -DI become defined at bar `period` (i.e. after the first seed).
  plusDI[period] = di(smP, atr)
  minusDI[period] = di(smM, atr)

  for (let i = period; i < trs.length; i++) {
    atr = (atr * (period - 1) + trs[i]) / period
    smP = (smP * (period - 1) + plusDMs[i]) / period
    smM = (smM * (period - 1) + minusDMs[i]) / period
    plusDI[i + 1] = di(smP, atr)
    minusDI[i + 1] = di(smM, atr)
  }

  // ADX: seeded at bar 2*period, then Wilder-smoothed.
  adx[2 * period] = adxVal
  for (let i = period, idx = 2 * period + 1; i < dxValues.length; i++, idx++) {
    if (idx >= n) break
    adxVal = (adxVal * (period - 1) + dxValues[i]) / period
    adx[idx] = adxVal
  }

  return { adx, plusDI, minusDI }
}

export function ADXChart({ candles }: ADXChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const adxSeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const plusDISeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const minusDISeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const threshold25Ref = useRef<ISeriesApi<'Line'> | null>(null)
  const threshold40Ref = useRef<ISeriesApi<'Line'> | null>(null)

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
      timeScale: { timeVisible: true, secondsVisible: false },
      rightPriceScale: { scaleMargins: { top: 0.05, bottom: 0.05 } },
    })

    const adxSeries = chart.addSeries(LineSeries, {
      color: '#ffd166',
      lineWidth: 2,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })
    const plusDISeries = chart.addSeries(LineSeries, {
      color: '#06d6a0',
      lineWidth: 1,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })
    const minusDISeries = chart.addSeries(LineSeries, {
      color: '#ef476f',
      lineWidth: 1,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })
    const t25 = chart.addSeries(LineSeries, {
      color: 'rgba(255, 255, 255, 0.25)',
      lineWidth: 1,
      lineStyle: 2,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })
    const t40 = chart.addSeries(LineSeries, {
      color: 'rgba(255, 214, 102, 0.35)',
      lineWidth: 1,
      lineStyle: 2,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    })

    chartRef.current = chart
    adxSeriesRef.current = adxSeries
    plusDISeriesRef.current = plusDISeries
    minusDISeriesRef.current = minusDISeries
    threshold25Ref.current = t25
    threshold40Ref.current = t40

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
    if (
      !chartRef.current ||
      !adxSeriesRef.current ||
      !plusDISeriesRef.current ||
      !minusDISeriesRef.current ||
      !threshold25Ref.current ||
      !threshold40Ref.current ||
      candles.length === 0
    ) {
      return
    }

    const highs = candles.map((c) => c.high)
    const lows = candles.map((c) => c.low)
    const closes = candles.map((c) => c.close)
    const times = candles.map((c) => Math.floor(c.time / 1000) as Time)
    const { adx, plusDI, minusDI } = calcADX(highs, lows, closes, 14)

    const adxData: LineData<Time>[] = []
    const plusData: LineData<Time>[] = []
    const minusData: LineData<Time>[] = []
    for (let i = 0; i < closes.length; i++) {
      if (adx[i] != null) adxData.push({ time: times[i], value: adx[i]! })
      if (plusDI[i] != null) plusData.push({ time: times[i], value: plusDI[i]! })
      if (minusDI[i] != null) minusData.push({ time: times[i], value: minusDI[i]! })
    }

    adxSeriesRef.current.setData(adxData)
    plusDISeriesRef.current.setData(plusData)
    minusDISeriesRef.current.setData(minusData)

    if (adxData.length >= 2) {
      const firstTime = adxData[0].time
      const lastTime = adxData[adxData.length - 1].time
      threshold25Ref.current.setData([
        { time: firstTime, value: 25 },
        { time: lastTime, value: 25 },
      ])
      threshold40Ref.current.setData([
        { time: firstTime, value: 40 },
        { time: lastTime, value: 40 },
      ])
    }

    chartRef.current.timeScale().fitContent()
  }, [candles])

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="mb-1 flex items-center gap-2">
        <span className="text-[11px] font-medium text-text-secondary">ADX</span>
        <span className="text-[10px] text-text-secondary/60">(14)</span>
        <div className="ml-auto flex items-center gap-3 text-[10px]">
          <span className="flex items-center gap-1">
            <span className="inline-block h-0.5 w-3 rounded" style={{ backgroundColor: '#ffd166' }} />ADX
          </span>
          <span className="flex items-center gap-1">
            <span className="inline-block h-0.5 w-3 rounded" style={{ backgroundColor: '#06d6a0' }} />+DI
          </span>
          <span className="flex items-center gap-1">
            <span className="inline-block h-0.5 w-3 rounded" style={{ backgroundColor: '#ef476f' }} />-DI
          </span>
          <span className="text-text-secondary/50">25 / 40</span>
        </div>
      </div>
      <div ref={containerRef} />
    </div>
  )
}
