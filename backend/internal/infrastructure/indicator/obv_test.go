package indicator

import (
	"math"
	"testing"
)

func TestOBV_AccumulatesOnUpDays(t *testing.T) {
	// Close goes up every bar -> volume keeps accumulating positive.
	closes := []float64{10, 11, 12, 13}
	volumes := []float64{100, 200, 150, 300}

	got := OBV(closes, volumes)
	// OBV[0] = 0 (seed), then +200, +150, +300 -> 650
	want := 650.0
	if got != want {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestOBV_SubtractsOnDownDays(t *testing.T) {
	closes := []float64{10, 9, 8, 7}
	volumes := []float64{100, 200, 150, 300}

	got := OBV(closes, volumes)
	// OBV[0] = 0, then -200, -150, -300 -> -650
	want := -650.0
	if got != want {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestOBV_UnchangedOnFlatDays(t *testing.T) {
	closes := []float64{10, 10, 10, 10}
	volumes := []float64{100, 200, 150, 300}

	got := OBV(closes, volumes)
	if got != 0 {
		t.Errorf("flat closes should leave OBV at 0, got %v", got)
	}
}

func TestOBV_MixedDirections(t *testing.T) {
	closes := []float64{10, 11, 10, 12, 11}
	volumes := []float64{100, 200, 150, 300, 250}

	got := OBV(closes, volumes)
	// 0 + 200 (up) - 150 (down) + 300 (up) - 250 (down) = 100
	want := 100.0
	if got != want {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestOBV_InsufficientData(t *testing.T) {
	got := OBV([]float64{10}, []float64{100})
	if !math.IsNaN(got) {
		t.Errorf("single-bar input should return NaN, got %v", got)
	}
	if got := OBV(nil, nil); !math.IsNaN(got) {
		t.Errorf("nil input should return NaN, got %v", got)
	}
}

func TestOBV_MismatchedLengths(t *testing.T) {
	got := OBV([]float64{10, 11, 12}, []float64{100, 200})
	if !math.IsNaN(got) {
		t.Errorf("mismatched length should return NaN, got %v", got)
	}
}

func TestOBVSlope_UpTrend(t *testing.T) {
	// OBV history walking up: 0, 200, 350, 650 — slope over last 3 bars is
	// (650 - 200) / 2 (slope-per-bar basis is not normalized here; we just
	// want a signed magnitude the gate can threshold).
	closes := []float64{10, 11, 12, 13}
	volumes := []float64{100, 200, 150, 300}

	got := OBVSlope(closes, volumes, 3)
	// OBV_now - OBV_{n-3} = 650 - 0 = 650 (window includes seed=0)
	if got <= 0 {
		t.Errorf("expected positive slope, got %v", got)
	}
}

func TestOBVSlope_DownTrend(t *testing.T) {
	closes := []float64{10, 9, 8, 7}
	volumes := []float64{100, 200, 150, 300}

	got := OBVSlope(closes, volumes, 3)
	if got >= 0 {
		t.Errorf("expected negative slope, got %v", got)
	}
}

func TestOBVSlope_InsufficientData(t *testing.T) {
	got := OBVSlope([]float64{10, 11}, []float64{100, 200}, 5)
	if !math.IsNaN(got) {
		t.Errorf("insufficient bars should return NaN, got %v", got)
	}
}

func TestOBVSlope_InvalidWindow(t *testing.T) {
	closes := []float64{10, 11, 12}
	volumes := []float64{100, 200, 150}
	for _, w := range []int{0, -1} {
		got := OBVSlope(closes, volumes, w)
		if !math.IsNaN(got) {
			t.Errorf("window=%d should return NaN, got %v", w, got)
		}
	}
}
