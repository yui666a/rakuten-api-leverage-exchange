package indicator

import "math"

// IchimokuResult carries the five-line Ichimoku cloud snapshot for the latest
// bar. All fields are NaN when there is not enough history to define them:
//   - Tenkan: needs 9 bars
//   - Kijun:  needs 26 bars
//   - SenkouA / SenkouB: at the latest bar the "future" projection is what was
//     computed 26 bars ago (because A/B are plotted +26 ahead). That means
//     we can fill SenkouA/B at bar i only when i-26 >= 0 AND the source
//     bar i-26 had a valid Tenkan (9+26=35 bars) / SenkouB (52+26=78 bars).
//   - Chikou: close plotted 26 bars back. At the latest bar Chikou simply
//     equals the latest close (there is no "future close" available).
//     Exposed for completeness; the Strategy layer currently doesn't use it.
type IchimokuResult struct {
	Tenkan  float64
	Kijun   float64
	SenkouA float64
	SenkouB float64
	Chikou  float64
}

// highLowMid returns the midpoint of the highest-high / lowest-low over the
// trailing `period` bars ending at `index` (inclusive). Returns NaN when the
// window is incomplete or inputs mismatch.
func highLowMid(highs, lows []float64, period, index int) float64 {
	if period <= 0 || index < period-1 || len(highs) != len(lows) {
		return math.NaN()
	}
	if index >= len(highs) {
		return math.NaN()
	}
	hi := math.Inf(-1)
	lo := math.Inf(1)
	for j := index - period + 1; j <= index; j++ {
		if highs[j] > hi {
			hi = highs[j]
		}
		if lows[j] < lo {
			lo = lows[j]
		}
	}
	return (hi + lo) / 2
}

// Ichimoku computes the five Ichimoku lines for the latest bar.
//
// Parameters follow the canonical Ichimoku Kinkō Hyō definition:
//   - tenkanPeriod = 9
//   - kijunPeriod = 26 (also doubles as the Senkou projection offset)
//   - senkouBPeriod = 52
//
// Returns an IchimokuResult whose fields are NaN when the warmup is not yet
// long enough to define that specific line.
//
// The "displayed" Senkou Span A/B at the latest bar come from a source bar
// kijunPeriod (26) bars in the past — i.e. what was projected forward 26
// bars ago and is now arriving. This mirrors the FE calcIchimoku behaviour
// where senkouA[i] for i >= kijunPeriod derives from tenkan/kijun at
// i-kijunPeriod, and senkouB[i] uses highLowMid over senkouBPeriod ending
// at i-kijunPeriod.
func Ichimoku(highs, lows, closes []float64, tenkanPeriod, kijunPeriod, senkouBPeriod int) IchimokuResult {
	out := IchimokuResult{
		Tenkan:  math.NaN(),
		Kijun:   math.NaN(),
		SenkouA: math.NaN(),
		SenkouB: math.NaN(),
		Chikou:  math.NaN(),
	}
	n := len(closes)
	if tenkanPeriod <= 0 || kijunPeriod <= 0 || senkouBPeriod <= 0 {
		return out
	}
	if n != len(highs) || n != len(lows) {
		return out
	}
	if n == 0 {
		return out
	}
	last := n - 1

	out.Tenkan = highLowMid(highs, lows, tenkanPeriod, last)
	out.Kijun = highLowMid(highs, lows, kijunPeriod, last)

	// Senkou Span A/B: displayed at `last` come from source bar `last - kijunPeriod`.
	srcIdx := last - kijunPeriod
	if srcIdx >= 0 {
		t := highLowMid(highs, lows, tenkanPeriod, srcIdx)
		k := highLowMid(highs, lows, kijunPeriod, srcIdx)
		if !math.IsNaN(t) && !math.IsNaN(k) {
			out.SenkouA = (t + k) / 2
		}
		out.SenkouB = highLowMid(highs, lows, senkouBPeriod, srcIdx)
	}

	// Chikou Span: at the latest bar there is no "future close" yet, so it
	// simply equals the latest close. (Plot offset is handled on the FE.)
	out.Chikou = closes[last]
	return out
}

// IchimokuCloudPosition classifies price position relative to the cloud.
//   - "above": price strictly above both SenkouA and SenkouB (bullish)
//   - "below": price strictly below both SenkouA and SenkouB (bearish)
//   - "inside": price is between the two spans (neutral / consolidation)
//   - "": cloud not yet defined (NaN spans) — caller should treat as "unknown"
//
// The function takes the price explicitly (rather than pulling the latest
// close) so callers can evaluate it against any reference price — tick
// close, bar mid, or a what-if value.
func IchimokuCloudPosition(price float64, ic IchimokuResult) string {
	if math.IsNaN(ic.SenkouA) || math.IsNaN(ic.SenkouB) {
		return ""
	}
	upper := math.Max(ic.SenkouA, ic.SenkouB)
	lower := math.Min(ic.SenkouA, ic.SenkouB)
	switch {
	case price > upper:
		return "above"
	case price < lower:
		return "below"
	default:
		return "inside"
	}
}
