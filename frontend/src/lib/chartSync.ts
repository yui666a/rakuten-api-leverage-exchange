import type { IChartApi, IRange, Time } from 'lightweight-charts'

// ChartSyncGroup keeps a set of lightweight-charts instances mirroring the same
// visible *time* range. Used by CandlestickChart and the indicator sub-panels
// (MACD / RSI / Stochastics / ADX) so scrolling or zooming any one of them
// pans/zooms the others to match.
//
// Time-based sync (instead of logical/index-based sync) is required because
// indicator series are shorter than the candle series — MACD's first ~33 points
// are null while EMA(26)+Signal(9) warm up, RSI's first 14 are null, ADX's
// first ~28 are null. If we synced by logical index, the same index in the
// indicator panel would refer to a different real time than in the candle
// panel, so the right edge of the indicator chart would land "in the past"
// relative to the candle chart. Time ranges are series-length independent and
// keep the panels aligned to the same wall-clock window.
//
// A re-entrancy guard (`syncing`) prevents the forwarded `setVisibleRange`
// calls from re-firing the listeners and causing an infinite loop.
export class ChartSyncGroup {
  private members = new Map<IChartApi, (range: IRange<Time> | null) => void>()
  private syncing = false

  register(chart: IChartApi) {
    if (this.members.has(chart)) return
    const handler = (range: IRange<Time> | null) => {
      if (this.syncing || range === null) return
      this.syncing = true
      try {
        for (const other of this.members.keys()) {
          if (other === chart) continue
          other.timeScale().setVisibleRange(range)
        }
      } finally {
        this.syncing = false
      }
    }
    chart.timeScale().subscribeVisibleTimeRangeChange(handler)
    this.members.set(chart, handler)

    // New member should snap to whichever range the group is already showing.
    for (const other of this.members.keys()) {
      if (other === chart) continue
      const range = other.timeScale().getVisibleRange()
      if (range) {
        this.syncing = true
        try {
          chart.timeScale().setVisibleRange(range)
        } finally {
          this.syncing = false
        }
        break
      }
    }
  }

  unregister(chart: IChartApi) {
    const handler = this.members.get(chart)
    if (!handler) return
    chart.timeScale().unsubscribeVisibleTimeRangeChange(handler)
    this.members.delete(chart)
  }
}
