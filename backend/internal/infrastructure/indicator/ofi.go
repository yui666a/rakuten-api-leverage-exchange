package indicator

import (
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// OFICalculator computes Order Flow Imbalance (OFI) over a rolling time
// window from a sequence of orderbook snapshots.
//
// Definition used here:
//
//	bidDepth(t) = TopNDepth(Bids, TopN)
//	askDepth(t) = TopNDepth(Asks, TopN)
//	delta(t)    = (bidDepth(t) - bidDepth(t-1)) - (askDepth(t) - askDepth(t-1))
//	OFI(window) = sum_{t in window} delta(t) / depth_at_window_start
//
// We normalise by the bid+ask top-N depth observed at the *start* of the
// window so the metric is dimensionless and comparable across symbols.
// Positive values mean bid pressure has grown faster than ask pressure
// over the window (taker-buy intent); negative the opposite.
//
// The calculator only stores observation snapshots — there is no caller-
// driven state outside the time window. Old observations are evicted when
// they fall outside windowMs from the latest Add.
type OFICalculator struct {
	topN     int
	windowMs int64

	obs []ofiObservation
}

type ofiObservation struct {
	timestamp int64
	bidDepth  float64
	askDepth  float64
}

// NewOFICalculator constructs an OFI calculator. windowMs is the rolling
// window in unix-millis (e.g. 10_000 for 10 s, 60_000 for 1 min). topN is
// the depth limit (passed through to TopNDepth).
//
// topN <= 0 falls back to 5; windowMs <= 0 falls back to 10_000 — the
// short-window default.
func NewOFICalculator(topN int, windowMs int64) *OFICalculator {
	if topN <= 0 {
		topN = 5
	}
	if windowMs <= 0 {
		windowMs = 10_000
	}
	return &OFICalculator{topN: topN, windowMs: windowMs}
}

// Add records a snapshot. Snapshots out of order (timestamp <= last) are
// silently ignored — OFI by definition needs monotonically advancing time.
func (o *OFICalculator) Add(ob entity.Orderbook) {
	if len(o.obs) > 0 && ob.Timestamp <= o.obs[len(o.obs)-1].timestamp {
		return
	}
	o.obs = append(o.obs, ofiObservation{
		timestamp: ob.Timestamp,
		bidDepth:  TopNDepth(ob.Bids, o.topN),
		askDepth:  TopNDepth(ob.Asks, o.topN),
	})
	o.evictBefore(ob.Timestamp - o.windowMs)
}

// Compute returns the normalised OFI for the current window. Returns
// (0, false) until at least 2 observations are present and the window-
// start depth is non-zero.
func (o *OFICalculator) Compute() (float64, bool) {
	if len(o.obs) < 2 {
		return 0, false
	}
	first := o.obs[0]
	denom := first.bidDepth + first.askDepth
	if denom <= 0 {
		return 0, false
	}
	bidDelta := o.obs[len(o.obs)-1].bidDepth - first.bidDepth
	askDelta := o.obs[len(o.obs)-1].askDepth - first.askDepth
	return (bidDelta - askDelta) / denom, true
}

// Reset drops all observations. Used by the IndicatorHandler when symbol or
// pipeline state changes invalidate the historical context.
func (o *OFICalculator) Reset() { o.obs = o.obs[:0] }

// Len reports how many snapshots are currently in the window. Exposed for
// tests and metrics.
func (o *OFICalculator) Len() int { return len(o.obs) }

// evictBefore drops observations strictly older than cutoff. Linear scan is
// fine: at typical 5 s persist throttle and 60 s window, the buffer holds
// ~12 entries.
func (o *OFICalculator) evictBefore(cutoff int64) {
	idx := 0
	for ; idx < len(o.obs); idx++ {
		if o.obs[idx].timestamp >= cutoff {
			break
		}
	}
	if idx > 0 {
		o.obs = append(o.obs[:0], o.obs[idx:]...)
	}
}
