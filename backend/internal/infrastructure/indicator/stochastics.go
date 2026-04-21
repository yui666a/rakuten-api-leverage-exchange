package indicator

import "math"

// Stochastics computes the fast/slow stochastic oscillator.
//
// Returns (%K, %D) where:
//   - %K = SMA over dSmoothing of the raw stochastic
//     rawK = (close - lowestLow_k) / (highestHigh_k - lowestLow_k) * 100
//     The raw %K is smoothed by dSmoothing to yield the reported %K ("slow %K");
//     this matches the standard TradingView / FE calcStochastics behaviour where
//     the displayed %K is already the 3-period smoothing of the raw stochastic.
//   - %D = SMA over dPeriod of %K.
//
// NaN is returned when input lengths mismatch, any period is non-positive, or
// there are not enough bars to evaluate the last window. A flat window
// (highestHigh == lowestLow) reports 50 so callers can treat it as "neutral"
// without a divide-by-zero special case — same convention as the FE.
//
// Standard parameters: kPeriod=14, dSmoothing=3, dPeriod=3.
func Stochastics(highs, lows, closes []float64, kPeriod, dSmoothing, dPeriod int) (percentK, percentD float64) {
	n := len(closes)
	if kPeriod <= 0 || dSmoothing <= 0 || dPeriod <= 0 {
		return math.NaN(), math.NaN()
	}
	if n != len(highs) || n != len(lows) {
		return math.NaN(), math.NaN()
	}
	// Need kPeriod bars for the first raw %K, then dSmoothing-1 more to
	// produce the first slow %K, then dPeriod-1 more to produce %D.
	required := kPeriod + dSmoothing - 1 + dPeriod - 1
	if n < required {
		return math.NaN(), math.NaN()
	}

	rawK := make([]float64, 0, dSmoothing+dPeriod)
	// We only need the last dSmoothing+dPeriod-1 raw %K values to compute
	// the final slow %K (dSmoothing-length SMA) and %D (dPeriod-length SMA
	// over slow %K). Walk the tail just far enough.
	startRaw := n - (dSmoothing + dPeriod - 1)
	for i := startRaw; i < n; i++ {
		hi := math.Inf(-1)
		lo := math.Inf(1)
		for j := i - kPeriod + 1; j <= i; j++ {
			if highs[j] > hi {
				hi = highs[j]
			}
			if lows[j] < lo {
				lo = lows[j]
			}
		}
		rng := hi - lo
		if rng == 0 {
			rawK = append(rawK, 50)
			continue
		}
		rawK = append(rawK, (closes[i]-lo)/rng*100)
	}

	// Slow %K series: SMA of rawK over dSmoothing, sliding window of size
	// dPeriod so %D can be averaged from it.
	slowK := make([]float64, dPeriod)
	for i := 0; i < dPeriod; i++ {
		sum := 0.0
		for j := 0; j < dSmoothing; j++ {
			sum += rawK[i+j]
		}
		slowK[i] = sum / float64(dSmoothing)
	}

	sumD := 0.0
	for _, v := range slowK {
		sumD += v
	}
	return slowK[len(slowK)-1], sumD / float64(dPeriod)
}

// StochasticRSI applies the stochastic oscillator formula to a series of RSI
// values. Returns the latest Stoch-RSI reading scaled to 0-100 (matching the
// convention of the underlying stochastic), or NaN on insufficient data /
// invalid parameters.
//
// Method:
//  1. Compute RSI[i] for every bar with at least rsiPeriod+1 preceding prices.
//  2. Over a stochPeriod-length window of RSI values, report
//     (rsi_now - min) / (max - min) * 100.
//
// A flat RSI window (max == min) returns 50 for the same reason as
// Stochastics above.
//
// Standard: rsiPeriod=14, stochPeriod=14.
func StochasticRSI(prices []float64, rsiPeriod, stochPeriod int) float64 {
	if rsiPeriod <= 0 || stochPeriod <= 0 {
		return math.NaN()
	}
	// Need rsiPeriod+1 for the first RSI, then stochPeriod-1 more RSI
	// readings to fill the window.
	if len(prices) < rsiPeriod+stochPeriod {
		return math.NaN()
	}

	rsis := make([]float64, 0, stochPeriod)
	// Produce the last stochPeriod RSI values. RSI is path-dependent so we
	// re-run it over growing prefixes; expensive but fine at stochPeriod=14.
	startRSI := len(prices) - stochPeriod
	for end := startRSI; end < len(prices); end++ {
		r := RSI(prices[:end+1], rsiPeriod)
		if math.IsNaN(r) {
			return math.NaN()
		}
		rsis = append(rsis, r)
	}

	minR := math.Inf(1)
	maxR := math.Inf(-1)
	for _, r := range rsis {
		if r < minR {
			minR = r
		}
		if r > maxR {
			maxR = r
		}
	}
	if maxR == minR {
		return 50
	}
	return (rsis[len(rsis)-1] - minR) / (maxR - minR) * 100
}
