package indicator

import (
	"math"
	"testing"
)

func TestDonchian_BasicRange(t *testing.T) {
	highs := []float64{10, 12, 14, 11, 13, 15, 12, 10, 11, 16, 14}
	lows := []float64{8, 9, 10, 9, 10, 11, 9, 7, 8, 12, 11}

	// Donchian(5) over the last 5 bars: highs[6..10] = {12,10,11,16,14}, lows = {9,7,8,12,11}
	// Upper = 16, Lower = 7, Middle = (16+7)/2 = 11.5
	upper, lower, middle := Donchian(highs, lows, 5)

	if upper != 16 {
		t.Errorf("upper: want 16, got %v", upper)
	}
	if lower != 7 {
		t.Errorf("lower: want 7, got %v", lower)
	}
	if middle != 11.5 {
		t.Errorf("middle: want 11.5, got %v", middle)
	}
}

func TestDonchian_ExcludesCurrentBar(t *testing.T) {
	// Donchian breakout conventions vary. We use the "inclusive of current"
	// convention so the caller can compare lastPrice > upper directly to
	// detect an upside breakout on the current bar.
	highs := []float64{5, 6, 7, 8, 100}
	lows := []float64{1, 2, 3, 4, 50}

	upper, lower, middle := Donchian(highs, lows, 5)
	if upper != 100 || lower != 1 || middle != 50.5 {
		t.Errorf("inclusive window: got upper=%v lower=%v middle=%v, want 100/1/50.5", upper, lower, middle)
	}
}

func TestDonchian_PeriodEqualsLength(t *testing.T) {
	highs := []float64{10, 12, 14}
	lows := []float64{8, 9, 10}

	upper, lower, middle := Donchian(highs, lows, 3)
	if upper != 14 || lower != 8 || middle != 11 {
		t.Errorf("full-window: got upper=%v lower=%v middle=%v, want 14/8/11", upper, lower, middle)
	}
}

func TestDonchian_InsufficientData(t *testing.T) {
	highs := []float64{10, 12}
	lows := []float64{8, 9}

	upper, lower, middle := Donchian(highs, lows, 5)
	if !math.IsNaN(upper) || !math.IsNaN(lower) || !math.IsNaN(middle) {
		t.Errorf("insufficient data: want NaN triplet, got %v / %v / %v", upper, lower, middle)
	}
}

func TestDonchian_MismatchedLengths(t *testing.T) {
	highs := []float64{10, 12, 14, 11}
	lows := []float64{8, 9, 10} // shorter

	upper, lower, middle := Donchian(highs, lows, 3)
	if !math.IsNaN(upper) || !math.IsNaN(lower) || !math.IsNaN(middle) {
		t.Errorf("mismatched lengths: want NaN triplet, got %v / %v / %v", upper, lower, middle)
	}
}

func TestDonchian_InvalidPeriod(t *testing.T) {
	highs := []float64{10, 12, 14}
	lows := []float64{8, 9, 10}

	for _, p := range []int{0, -1} {
		upper, lower, middle := Donchian(highs, lows, p)
		if !math.IsNaN(upper) || !math.IsNaN(lower) || !math.IsNaN(middle) {
			t.Errorf("period=%d: want NaN triplet, got %v / %v / %v", p, upper, lower, middle)
		}
	}
}

func TestDonchian_FlatWindow(t *testing.T) {
	// If the window is perfectly flat, upper == lower == middle and no
	// breakout is possible. Make sure we do not NaN out on the edge.
	highs := []float64{5, 5, 5, 5, 5}
	lows := []float64{5, 5, 5, 5, 5}

	upper, lower, middle := Donchian(highs, lows, 5)
	if upper != 5 || lower != 5 || middle != 5 {
		t.Errorf("flat window: want 5/5/5, got %v/%v/%v", upper, lower, middle)
	}
}
