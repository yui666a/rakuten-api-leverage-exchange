package indicator

import "math"

// Donchian computes the Donchian Channel (highest high, lowest low, and their
// midpoint) over the most recent `period` bars.
//
// Returns (upper, lower, middle) where:
//   - upper:  highest high within the last `period` bars (inclusive of the
//     current bar — callers wanting the "N-prior bars excluding today"
//     convention should pass highs[:len-1] / lows[:len-1]).
//   - lower:  lowest low within the same window.
//   - middle: arithmetic midpoint (upper+lower)/2.
//
// NaN is returned for all three when period <= 0, the input slices have
// mismatched lengths, or there are fewer than `period` bars available.
//
// The caller's intended use is a breakout gate on top of the existing BB-based
// breakout: `lastPrice > upper` → upside confirmation, `lastPrice < lower` →
// downside confirmation. Keeping the inclusive-of-current-bar convention makes
// that comparison direct without the caller having to slice the input again.
func Donchian(highs, lows []float64, period int) (upper, lower, middle float64) {
	if period <= 0 {
		return math.NaN(), math.NaN(), math.NaN()
	}
	if len(highs) != len(lows) || len(highs) < period {
		return math.NaN(), math.NaN(), math.NaN()
	}

	start := len(highs) - period
	upper = math.Inf(-1)
	lower = math.Inf(1)
	for i := start; i < len(highs); i++ {
		if highs[i] > upper {
			upper = highs[i]
		}
		if lows[i] < lower {
			lower = lows[i]
		}
	}
	middle = (upper + lower) / 2
	return upper, lower, middle
}
