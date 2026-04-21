package indicator

import (
	"math"
	"testing"
)

// buildFlatSeries returns n candles that stay at the same price so every
// True Range component and every +DM/-DM is zero. ADX on this data must
// collapse to zero (or NaN when the divisor is zero).
func buildFlatSeries(n int, price float64) (highs, lows, closes []float64) {
	highs = make([]float64, n)
	lows = make([]float64, n)
	closes = make([]float64, n)
	for i := 0; i < n; i++ {
		highs[i] = price
		lows[i] = price
		closes[i] = price
	}
	return
}

// buildMonotonicUptrend synthesises a perfectly increasing price series so
// every bar has +DM > 0 and -DM == 0. ADX must asymptote toward a very
// strong trend reading (> 40 by the standard Wilder scale).
func buildMonotonicUptrend(n int, start, step float64) (highs, lows, closes []float64) {
	highs = make([]float64, n)
	lows = make([]float64, n)
	closes = make([]float64, n)
	price := start
	for i := 0; i < n; i++ {
		closes[i] = price
		highs[i] = price + 0.5 // intrabar high
		lows[i] = price - 0.5
		price += step
	}
	return
}

func TestADX_InsufficientDataReturnsNaN(t *testing.T) {
	// ADX needs 2*period+1 bars minimum (period bars to seed +DM/-DM/TR
	// averages, then another period bars to seed ADX itself). Anything
	// shorter must return NaN.
	h, l, c := buildFlatSeries(10, 100) // 10 bars for period=14
	adx, plus, minus := ADX(h, l, c, 14)
	if !math.IsNaN(adx) || !math.IsNaN(plus) || !math.IsNaN(minus) {
		t.Fatalf("insufficient data: want NaN, got adx=%v +di=%v -di=%v", adx, plus, minus)
	}
}

func TestADX_LengthMismatchReturnsNaN(t *testing.T) {
	h := make([]float64, 30)
	l := make([]float64, 30)
	c := make([]float64, 29)
	adx, plus, minus := ADX(h, l, c, 14)
	if !math.IsNaN(adx) || !math.IsNaN(plus) || !math.IsNaN(minus) {
		t.Fatalf("mismatched lengths: want NaN, got adx=%v", adx)
	}
}

func TestADX_FlatSeriesReturnsZeroOrNaN(t *testing.T) {
	h, l, c := buildFlatSeries(60, 100)
	adx, plus, minus := ADX(h, l, c, 14)
	// A perfectly flat series yields +DM = -DM = TR = 0 every bar. The
	// canonical DX formula has a zero denominator in this case — we handle
	// it by returning 0 (no trend, no information), not NaN, so callers can
	// still filter on "adx < threshold" without a special case.
	if math.IsNaN(adx) {
		t.Fatalf("flat series should return 0, got NaN adx")
	}
	if adx != 0 {
		t.Fatalf("flat series ADX = %v, want 0", adx)
	}
	if plus != 0 || minus != 0 {
		t.Fatalf("flat series +DI/-DI = %v/%v, want 0/0", plus, minus)
	}
}

func TestADX_StrongUptrendShowsPlusDIGreaterThanMinusDI(t *testing.T) {
	h, l, c := buildMonotonicUptrend(60, 100, 1.0)
	adx, plus, minus := ADX(h, l, c, 14)
	if math.IsNaN(adx) {
		t.Fatalf("strong trend gave NaN ADX")
	}
	if plus <= minus {
		t.Fatalf("uptrend expected +DI > -DI, got +DI=%v -DI=%v", plus, minus)
	}
	// A clean uptrend should register as a strong trend (Wilder: >25 is
	// "trending", >40 is "very strong"). We assert a conservative 25 so
	// future tweaks to the smoothing don't make this test flaky.
	if adx < 25 {
		t.Fatalf("strong uptrend ADX = %v, expected >= 25", adx)
	}
}

func TestADX_StrongDowntrendShowsMinusDIGreaterThanPlusDI(t *testing.T) {
	// Mirror of the uptrend test: use a monotonically decreasing series.
	n := 60
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	price := 200.0
	for i := 0; i < n; i++ {
		closes[i] = price
		highs[i] = price + 0.5
		lows[i] = price - 0.5
		price -= 1.0
	}
	adx, plus, minus := ADX(highs, lows, closes, 14)
	if minus <= plus {
		t.Fatalf("downtrend expected -DI > +DI, got +DI=%v -DI=%v", plus, minus)
	}
	if adx < 25 {
		t.Fatalf("strong downtrend ADX = %v, expected >= 25", adx)
	}
}

func TestADX_RangeBoundShowsLowADX(t *testing.T) {
	// Oscillating price between 99 and 101 (bandwidth 2, sideways) — ADX
	// should stay well below the 25 "trending" threshold.
	n := 80
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			closes[i] = 101
		} else {
			closes[i] = 99
		}
		highs[i] = closes[i] + 0.2
		lows[i] = closes[i] - 0.2
	}
	adx, _, _ := ADX(highs, lows, closes, 14)
	if math.IsNaN(adx) {
		t.Fatalf("range-bound series gave NaN ADX")
	}
	if adx >= 25 {
		t.Fatalf("range-bound ADX = %v, want < 25 (trend gate should not fire)", adx)
	}
}

func TestADX_Period1Deterministic(t *testing.T) {
	// Tiny sanity check: period=1 should reduce to momentum one-bar by
	// one-bar. Build a trivial series and verify no panic / NaN when the
	// setup is minimal but valid.
	h := []float64{100, 101, 102}
	l := []float64{99, 100, 101}
	c := []float64{100, 101, 102}
	adx, plus, minus := ADX(h, l, c, 1)
	if math.IsNaN(adx) || math.IsNaN(plus) || math.IsNaN(minus) {
		t.Fatalf("minimal valid input produced NaN: adx=%v +di=%v -di=%v", adx, plus, minus)
	}
}
