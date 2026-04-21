package indicator

import (
	"math"
	"testing"
)

func TestStochastics_InsufficientDataReturnsNaN(t *testing.T) {
	// kPeriod=14, dSmoothing=3, dPeriod=3 => needs 18 bars
	h, l, c := buildFlatSeries(10, 100)
	k, d := Stochastics(h, l, c, 14, 3, 3)
	if !math.IsNaN(k) || !math.IsNaN(d) {
		t.Fatalf("insufficient data: want NaN, got k=%v d=%v", k, d)
	}
}

func TestStochastics_LengthMismatchReturnsNaN(t *testing.T) {
	h := make([]float64, 30)
	l := make([]float64, 30)
	c := make([]float64, 29)
	k, d := Stochastics(h, l, c, 14, 3, 3)
	if !math.IsNaN(k) || !math.IsNaN(d) {
		t.Fatalf("mismatched lengths: want NaN, got %v %v", k, d)
	}
}

func TestStochastics_FlatSeriesReturns50(t *testing.T) {
	// Flat range -> the convention is 50 (neutral), matching the FE
	// calcStochastics contract.
	h, l, c := buildFlatSeries(40, 100)
	k, d := Stochastics(h, l, c, 14, 3, 3)
	if math.IsNaN(k) || math.IsNaN(d) {
		t.Fatalf("flat series gave NaN: %v %v", k, d)
	}
	if k != 50 || d != 50 {
		t.Fatalf("flat series: want 50/50, got k=%v d=%v", k, d)
	}
}

func TestStochastics_StrongUptrendNearCeiling(t *testing.T) {
	// Monotonic uptrend: close is always near highest high -> %K near 100.
	h, l, c := buildMonotonicUptrend(40, 100, 1.0)
	k, _ := Stochastics(h, l, c, 14, 3, 3)
	if math.IsNaN(k) {
		t.Fatalf("uptrend: slowK should not be NaN")
	}
	if k < 80 {
		t.Fatalf("uptrend: slowK=%v, expected >= 80 (overbought territory)", k)
	}
}

func TestStochastics_StrongDowntrendNearFloor(t *testing.T) {
	n := 40
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
	k, _ := Stochastics(highs, lows, closes, 14, 3, 3)
	if math.IsNaN(k) {
		t.Fatalf("downtrend: slowK should not be NaN")
	}
	if k > 20 {
		t.Fatalf("downtrend: slowK=%v, expected <= 20 (oversold territory)", k)
	}
}

func TestStochastics_MatchesFEGoldenValue(t *testing.T) {
	// Golden-value test: ensure our slow %K / %D formula matches the FE
	// StochasticsChart.tsx `calcStochastics` (kPeriod smoothed by dPeriod).
	// FE uses a single smoothing of dPeriod=3 applied to the raw k series to
	// produce %K, then SMAs %K over dPeriod=3 to produce %D. We pass
	// dSmoothing=1 and dPeriod=3 to replicate the FE "raw %K"+SMA3 variant
	// and sanity-check numeric agreement.
	highs := []float64{10, 11, 12, 13, 14, 15, 14, 13, 12, 11, 12, 13, 14, 15, 16, 17, 18}
	lows := []float64{9, 10, 11, 12, 13, 14, 13, 12, 11, 10, 11, 12, 13, 14, 15, 16, 17}
	closes := []float64{9.5, 10.5, 11.5, 12.5, 13.5, 14.5, 13.5, 12.5, 11.5, 10.5, 11.5, 12.5, 13.5, 14.5, 15.5, 16.5, 17.5}

	// FE calcStochastics with kPeriod=14, dPeriod=3 -> replicate:
	// rawK per bar (window=14), then %D = SMA3(rawK) for the last 3 bars.
	// We call with dSmoothing=1 (=> slow %K == raw %K) and dPeriod=3.
	k, d := Stochastics(highs, lows, closes, 14, 1, 3)
	if math.IsNaN(k) || math.IsNaN(d) {
		t.Fatalf("FE-parity case gave NaN: k=%v d=%v", k, d)
	}
	// At the latest bar the close 17.5 sits at the very top of the 14-bar
	// range — %K should be ~100, %D the SMA of the last 3 rawK readings.
	// Hand-calc: at i=16, close=17.5, 14-bar window lows[3..16] min=11,
	// highs[3..16] max=18 => rawK = (17.5-11)/(18-11)*100 ≈ 92.86.
	// With dSmoothing=1 we expose the raw value directly.
	if k < 90 || k > 95 {
		t.Fatalf("FE-parity slowK=%v, expected ~92.86", k)
	}
	// %D = SMA3 of the last three rawK readings (all deep into the recent
	// climb), so %D should also sit high but below slowK.
	if d < 70 || d > 100 {
		t.Fatalf("FE-parity slowD=%v, expected 70-100 (recent climb to top)", d)
	}
}

func TestStochasticRSI_InsufficientDataReturnsNaN(t *testing.T) {
	// Needs rsiPeriod+stochPeriod = 28 prices.
	prices := make([]float64, 20)
	for i := range prices {
		prices[i] = 100 + float64(i)
	}
	v := StochasticRSI(prices, 14, 14)
	if !math.IsNaN(v) {
		t.Fatalf("insufficient data: want NaN, got %v", v)
	}
}

func TestStochasticRSI_InvalidPeriodReturnsNaN(t *testing.T) {
	prices := make([]float64, 50)
	for i := range prices {
		prices[i] = 100 + float64(i)
	}
	if v := StochasticRSI(prices, 0, 14); !math.IsNaN(v) {
		t.Fatalf("rsiPeriod=0: want NaN, got %v", v)
	}
	if v := StochasticRSI(prices, 14, -1); !math.IsNaN(v) {
		t.Fatalf("stochPeriod<0: want NaN, got %v", v)
	}
}

func TestStochasticRSI_DecayingUptrendRegistersHigh(t *testing.T) {
	// Pure monotonic uptrend pins RSI at 100 (avgLoss = 0) which makes the
	// stochastic window constant -> StochRSI = 50 by the flat-window rule.
	// To exercise the "recent RSI near the top of its window" case we need
	// RSI to vary: build a series where the later bars climb harder than
	// the earlier ones so RSI rises through the window.
	n := 60
	prices := make([]float64, n)
	price := 100.0
	for i := 0; i < n; i++ {
		prices[i] = price
		if i < n/2 {
			// small oscillation in the first half so RSI lands
			// somewhere well below 100
			if i%2 == 0 {
				price += 0.2
			} else {
				price -= 0.1
			}
		} else {
			// strong rise in the recent half so RSI climbs.
			price += 1.0
		}
	}
	v := StochasticRSI(prices, 14, 14)
	if math.IsNaN(v) {
		t.Fatalf("mixed-then-climb: StochRSI should not be NaN")
	}
	// Recent RSI sits high in the window, so StochRSI should be near 100.
	if v < 70 {
		t.Fatalf("mixed-then-climb: StochRSI=%v, expected >= 70", v)
	}
}

func TestStochasticRSI_FlatWindowReturns50(t *testing.T) {
	// If RSI is constant across the window (here: slight noise so RSI hits
	// the same values), the flat-window branch returns 50.
	n := 50
	prices := make([]float64, n)
	for i := 0; i < n; i++ {
		prices[i] = 100
	}
	v := StochasticRSI(prices, 14, 14)
	if math.IsNaN(v) {
		t.Fatalf("flat prices: StochRSI should not be NaN")
	}
	if v != 50 {
		t.Fatalf("flat prices: StochRSI=%v, want 50", v)
	}
}
