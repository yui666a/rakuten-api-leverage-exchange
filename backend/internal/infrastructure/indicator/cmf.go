package indicator

import "math"

// CMF computes the Chaikin Money Flow oscillator over the most recent
// `period` bars. Standard period = 20. Result is bounded in [-1, 1]:
//
//	>0 = net buying pressure over the window
//	<0 = net selling pressure
//	0  = balanced or dead (no volume / flat bars)
//
// Formula:
//
//	MFM_i = ((close_i - low_i) - (high_i - close_i)) / (high_i - low_i)
//	MFV_i = MFM_i * volume_i
//	CMF   = sum(MFV over window) / sum(volume over window)
//
// Edge cases handled:
//   - high == low (flat bar): MFM treated as 0 so the bar contributes no
//     signal rather than NaN-ing the whole window. Matches the standard
//     Chaikin convention.
//   - sum(volume over window) == 0: return 0 to avoid divide-by-zero. The
//     caller's intent ("gate on CMF") is satisfied by a neutral reading.
//   - insufficient history, mismatched lengths, period <= 0: NaN.
func CMF(highs, lows, closes, volumes []float64, period int) float64 {
	if period <= 0 {
		return math.NaN()
	}
	n := len(closes)
	if n != len(highs) || n != len(lows) || n != len(volumes) || n < period {
		return math.NaN()
	}

	start := n - period
	var sumMFV, sumVol float64
	for i := start; i < n; i++ {
		rng := highs[i] - lows[i]
		var mfm float64
		if rng > 0 {
			mfm = ((closes[i] - lows[i]) - (highs[i] - closes[i])) / rng
		}
		sumMFV += mfm * volumes[i]
		sumVol += volumes[i]
	}
	if sumVol == 0 {
		return 0
	}
	return sumMFV / sumVol
}
