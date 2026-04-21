package indicator

import "math"

// ADX computes the Average Directional Index and the component +DI / -DI
// using Wilder's smoothing with the given period (standard period = 14).
//
// Returns (adx, +DI, -DI) where:
//   - adx: 0-100 trend-strength reading; <= 20 means "sideways / no trend",
//     >= 25 "trending", >= 40 "very strong".
//   - +DI: 0-100 up-pressure per bar (Wilder-smoothed).
//   - -DI: 0-100 down-pressure per bar (Wilder-smoothed).
//
// NaN is returned when there is not enough data (need at least
// 2*period+1 bars so both the +DM/-DM/TR averages and then ADX itself can
// be seeded) or when input slices have mismatched lengths.
//
// Implementation notes:
//   - +DM / -DM use the canonical "the bigger of the two directional
//     moves is taken; the smaller is zeroed; only positive directional
//     moves count" rule.
//   - When the seed period yields TR == 0 (perfectly flat market) the
//     algorithm would divide by zero in DI; in that case we return
//     (0, 0, 0) instead of NaN so callers can filter on "adx < N" without
//     a special case for the no-volatility edge.
func ADX(highs, lows, closes []float64, period int) (adx, plusDI, minusDI float64) {
	n := len(highs)
	if period <= 0 || n != len(lows) || n != len(closes) {
		return math.NaN(), math.NaN(), math.NaN()
	}
	if n < 2*period+1 {
		return math.NaN(), math.NaN(), math.NaN()
	}

	// Step 1: per-bar True Range, +DM, -DM.
	trs := make([]float64, n-1)
	plusDMs := make([]float64, n-1)
	minusDMs := make([]float64, n-1)
	for i := 1; i < n; i++ {
		upMove := highs[i] - highs[i-1]
		downMove := lows[i-1] - lows[i]
		pDM := 0.0
		mDM := 0.0
		if upMove > downMove && upMove > 0 {
			pDM = upMove
		}
		if downMove > upMove && downMove > 0 {
			mDM = downMove
		}
		plusDMs[i-1] = pDM
		minusDMs[i-1] = mDM

		tr1 := highs[i] - lows[i]
		tr2 := math.Abs(highs[i] - closes[i-1])
		tr3 := math.Abs(lows[i] - closes[i-1])
		trs[i-1] = math.Max(tr1, math.Max(tr2, tr3))
	}

	// Step 2: seed the Wilder-smoothed averages with a plain sum over the
	// first `period` bars.
	sumTR := 0.0
	sumPlusDM := 0.0
	sumMinusDM := 0.0
	for i := 0; i < period; i++ {
		sumTR += trs[i]
		sumPlusDM += plusDMs[i]
		sumMinusDM += minusDMs[i]
	}
	atr := sumTR / float64(period)
	smPlusDM := sumPlusDM / float64(period)
	smMinusDM := sumMinusDM / float64(period)

	// Helper closures for clarity.
	di := func(dm, tr float64) float64 {
		if tr <= 0 {
			return 0
		}
		return 100 * dm / tr
	}
	dx := func(p, m float64) float64 {
		denom := p + m
		if denom <= 0 {
			return 0
		}
		return 100 * math.Abs(p-m) / denom
	}

	// Step 3: walk the remaining bars, Wilder-smoothing TR/+DM/-DM and
	// accumulating DX values so we can average them into ADX.
	dxValues := make([]float64, 0, len(trs)-period+1)
	dxValues = append(dxValues, dx(di(smPlusDM, atr), di(smMinusDM, atr)))
	for i := period; i < len(trs); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
		smPlusDM = (smPlusDM*float64(period-1) + plusDMs[i]) / float64(period)
		smMinusDM = (smMinusDM*float64(period-1) + minusDMs[i]) / float64(period)
		dxValues = append(dxValues, dx(di(smPlusDM, atr), di(smMinusDM, atr)))
	}

	// Step 4: ADX seed = mean of first `period` DX values; then Wilder-
	// smooth the remaining DX values into ADX.
	if len(dxValues) < period {
		return math.NaN(), math.NaN(), math.NaN()
	}
	seed := 0.0
	for i := 0; i < period; i++ {
		seed += dxValues[i]
	}
	adx = seed / float64(period)
	for i := period; i < len(dxValues); i++ {
		adx = (adx*float64(period-1) + dxValues[i]) / float64(period)
	}

	plusDI = di(smPlusDM, atr)
	minusDI = di(smMinusDM, atr)
	return adx, plusDI, minusDI
}
