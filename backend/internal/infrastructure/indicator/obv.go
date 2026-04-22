package indicator

import "math"

// OBV computes On-Balance Volume as a single cumulative scalar anchored at
// zero on the first input bar. Direction rule:
//
//	close[i] > close[i-1] -> OBV += volume[i]
//	close[i] < close[i-1] -> OBV -= volume[i]
//	close[i] == close[i-1] -> OBV unchanged
//
// Returns NaN on fewer than 2 bars or on mismatched input lengths.
//
// OBV's absolute value is meaningless (zero is arbitrary), so callers should
// consume it through OBVSlope or compare to a prior OBV reading rather than
// against a static threshold.
func OBV(closes, volumes []float64) float64 {
	if len(closes) < 2 || len(closes) != len(volumes) {
		return math.NaN()
	}
	obv := 0.0
	for i := 1; i < len(closes); i++ {
		switch {
		case closes[i] > closes[i-1]:
			obv += volumes[i]
		case closes[i] < closes[i-1]:
			obv -= volumes[i]
		}
	}
	return obv
}

// OBVSlope returns (OBV_now − OBV_{n−window}) where OBV is the running
// cumulative volume series defined in OBV. A positive result means buying
// volume has exceeded selling volume over the last `window` bars; a negative
// result means the reverse. The magnitude is volume-scaled, so callers
// threshold it as "slope > 0" / "slope < 0" rather than comparing to an
// absolute cutoff across assets.
//
// Returns NaN when window <= 0, when there are fewer than window+1 bars,
// or when input slices are mismatched.
func OBVSlope(closes, volumes []float64, window int) float64 {
	if window <= 0 {
		return math.NaN()
	}
	n := len(closes)
	if n != len(volumes) || n < window+1 {
		return math.NaN()
	}
	// Build the OBV series so we can diff against the state `window` bars
	// back. Implementation is O(n); the live pipeline feeds ~500 bars, so
	// this stays trivial.
	series := make([]float64, n)
	series[0] = 0
	for i := 1; i < n; i++ {
		series[i] = series[i-1]
		switch {
		case closes[i] > closes[i-1]:
			series[i] += volumes[i]
		case closes[i] < closes[i-1]:
			series[i] -= volumes[i]
		}
	}
	return series[n-1] - series[n-1-window]
}
