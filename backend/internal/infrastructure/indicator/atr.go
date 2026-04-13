package indicator

import "math"

// ATR computes the Average True Range using Wilder's smoothing.
// candles must be in chronological order (oldest first).
// Each candle is [high, low, close].
// Returns NaN if insufficient data.
func ATR(highs, lows, closes []float64, period int) float64 {
	n := len(highs)
	if n < period+1 || len(lows) != n || len(closes) != n {
		return math.NaN()
	}

	// Compute True Range series
	trs := make([]float64, n-1)
	for i := 1; i < n; i++ {
		tr1 := highs[i] - lows[i]
		tr2 := math.Abs(highs[i] - closes[i-1])
		tr3 := math.Abs(lows[i] - closes[i-1])
		trs[i-1] = math.Max(tr1, math.Max(tr2, tr3))
	}

	// Initial ATR: simple average of first `period` true ranges
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	// Wilder's smoothing for remaining
	for i := period; i < len(trs); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}
