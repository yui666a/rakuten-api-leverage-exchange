import type { IChartApi, LogicalRange } from 'lightweight-charts'

// ChartSyncGroup keeps a set of lightweight-charts instances mirroring the same
// visible logical range. Used by CandlestickChart and the indicator sub-panels
// (MACD / RSI / Stochastics / ADX) so scrolling or zooming any one of them
// pans/zooms the others to match.
//
// Each member subscribes its own visible-range listener and forwards the range
// to the rest of the group. A re-entrancy guard (`syncing`) prevents the
// forwarded `setVisibleLogicalRange` calls from re-firing the listeners and
// causing an infinite loop.
export class ChartSyncGroup {
  private members = new Map<IChartApi, (range: LogicalRange | null) => void>()
  private syncing = false

  register(chart: IChartApi) {
    if (this.members.has(chart)) return
    const handler = (range: LogicalRange | null) => {
      if (this.syncing || range === null) return
      this.syncing = true
      try {
        for (const other of this.members.keys()) {
          if (other === chart) continue
          other.timeScale().setVisibleLogicalRange(range)
        }
      } finally {
        this.syncing = false
      }
    }
    chart.timeScale().subscribeVisibleLogicalRangeChange(handler)
    this.members.set(chart, handler)

    // New member should snap to whichever range the group is already showing.
    for (const other of this.members.keys()) {
      if (other === chart) continue
      const range = other.timeScale().getVisibleLogicalRange()
      if (range) {
        this.syncing = true
        try {
          chart.timeScale().setVisibleLogicalRange(range)
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
    chart.timeScale().unsubscribeVisibleLogicalRangeChange(handler)
    this.members.delete(chart)
  }
}
