package indicator

import (
	"math"
	"testing"
)

func TestCMF_AllBuyingPressure(t *testing.T) {
	// Close == high for every bar -> MFM = 1 for every bar -> CMF = 1.
	highs := []float64{10, 11, 12, 13, 14}
	lows := []float64{9, 10, 11, 12, 13}
	closes := []float64{10, 11, 12, 13, 14}
	volumes := []float64{100, 100, 100, 100, 100}

	got := CMF(highs, lows, closes, volumes, 3)
	// Last 3 bars: MFM=1 for each; CMF = (300)/300 = 1.0
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("want CMF=1.0, got %v", got)
	}
}

func TestCMF_AllSellingPressure(t *testing.T) {
	// Close == low for every bar -> MFM = -1 -> CMF = -1.
	highs := []float64{10, 11, 12, 13, 14}
	lows := []float64{9, 10, 11, 12, 13}
	closes := []float64{9, 10, 11, 12, 13}
	volumes := []float64{100, 100, 100, 100, 100}

	got := CMF(highs, lows, closes, volumes, 3)
	if math.Abs(got-(-1.0)) > 1e-9 {
		t.Errorf("want CMF=-1.0, got %v", got)
	}
}

func TestCMF_Neutral(t *testing.T) {
	// Close in the middle every bar -> MFM = 0 -> CMF = 0.
	highs := []float64{10, 11, 12, 13, 14}
	lows := []float64{8, 9, 10, 11, 12}
	closes := []float64{9, 10, 11, 12, 13} // midpoint
	volumes := []float64{100, 100, 100, 100, 100}

	got := CMF(highs, lows, closes, volumes, 3)
	if math.Abs(got) > 1e-9 {
		t.Errorf("want CMF≈0, got %v", got)
	}
}

func TestCMF_FlatBarPassedThroughAsZero(t *testing.T) {
	// A perfectly flat bar (high==low) has no information. We treat MFM as
	// 0 for that bar so CMF does not NaN out on a single dead candle.
	highs := []float64{10, 5, 12}
	lows := []float64{9, 5, 11}
	closes := []float64{10, 5, 12}
	volumes := []float64{100, 100, 100}

	got := CMF(highs, lows, closes, volumes, 3)
	if math.IsNaN(got) {
		t.Errorf("flat bar in the window should not produce NaN; got NaN")
	}
}

func TestCMF_InsufficientData(t *testing.T) {
	highs := []float64{10, 11}
	lows := []float64{9, 10}
	closes := []float64{10, 11}
	volumes := []float64{100, 100}

	got := CMF(highs, lows, closes, volumes, 5)
	if !math.IsNaN(got) {
		t.Errorf("insufficient bars should return NaN, got %v", got)
	}
}

func TestCMF_InvalidPeriod(t *testing.T) {
	highs := []float64{10, 11, 12}
	lows := []float64{9, 10, 11}
	closes := []float64{10, 11, 12}
	volumes := []float64{100, 100, 100}

	for _, p := range []int{0, -1} {
		got := CMF(highs, lows, closes, volumes, p)
		if !math.IsNaN(got) {
			t.Errorf("period=%d should return NaN, got %v", p, got)
		}
	}
}

func TestCMF_MismatchedLengths(t *testing.T) {
	highs := []float64{10, 11, 12}
	lows := []float64{9, 10}
	closes := []float64{10, 11, 12}
	volumes := []float64{100, 100, 100}

	got := CMF(highs, lows, closes, volumes, 3)
	if !math.IsNaN(got) {
		t.Errorf("mismatched lengths should return NaN, got %v", got)
	}
}

func TestCMF_ZeroVolumeWindow(t *testing.T) {
	// Sum of volumes is zero — avoids divide-by-zero by returning 0 (neutral).
	highs := []float64{10, 11, 12}
	lows := []float64{9, 10, 11}
	closes := []float64{10, 10.5, 11}
	volumes := []float64{0, 0, 0}

	got := CMF(highs, lows, closes, volumes, 3)
	if got != 0 {
		t.Errorf("zero-volume window should return 0, got %v", got)
	}
}
