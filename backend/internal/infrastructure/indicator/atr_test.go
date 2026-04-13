package indicator

import (
	"math"
	"testing"
)

func TestATR_InsufficientData(t *testing.T) {
	result := ATR([]float64{1, 2}, []float64{0.5, 1}, []float64{0.8, 1.5}, 14)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for insufficient data, got %f", result)
	}
}

func TestATR_MismatchedLengths(t *testing.T) {
	result := ATR([]float64{1, 2, 3}, []float64{0.5, 1}, []float64{0.8, 1.5, 2.5}, 2)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for mismatched lengths, got %f", result)
	}
}

func TestATR_KnownValues(t *testing.T) {
	// Simple 3-period ATR with known data
	highs := []float64{10, 12, 11, 13}
	lows := []float64{8, 9, 9, 10}
	closes := []float64{9, 11, 10, 12}

	result := ATR(highs, lows, closes, 3)
	if math.IsNaN(result) {
		t.Fatal("expected valid ATR, got NaN")
	}

	// TR[0] = max(12-9, |12-9|, |9-9|) = 3
	// TR[1] = max(11-9, |11-11|, |9-11|) = 2
	// TR[2] = max(13-10, |13-10|, |10-10|) = 3
	// Initial ATR (3-period): (3+2+3)/3 = 2.6667 (but only 3 TRs with period=3, no smoothing step)
	// Actually: first `period` TRs are 0..2 → (3+2)/2... let me recalculate
	// n=4, period=3. Need period+1=4 candles. trs has 3 entries.
	// trs[0]: max(12-9, |12-9|, |9-9|) = 3
	// trs[1]: max(11-9, |11-11|, |9-11|) = 2
	// trs[2]: max(13-10, |13-10|, |10-10|) = 3
	// Initial ATR = (3+2+3)/3 = 2.6667
	// No remaining TRs to smooth → ATR = 2.6667
	expected := (3.0 + 2.0 + 3.0) / 3.0
	if math.Abs(result-expected) > 0.01 {
		t.Fatalf("expected ATR=%.4f, got %.4f", expected, result)
	}
}

func TestATR_WithSmoothing(t *testing.T) {
	// 2-period ATR with 4 candles (3 TRs, smoothing applied once)
	highs := []float64{10, 12, 11, 14}
	lows := []float64{8, 9, 9, 11}
	closes := []float64{9, 11, 10, 13}

	result := ATR(highs, lows, closes, 2)
	if math.IsNaN(result) {
		t.Fatal("expected valid ATR, got NaN")
	}

	// trs[0]: max(12-9, |12-9|, |9-9|) = 3
	// trs[1]: max(11-9, |11-11|, |9-11|) = 2
	// trs[2]: max(14-11, |14-10|, |11-10|) = 4
	// Initial ATR (2-period): (3+2)/2 = 2.5
	// Smoothed: (2.5*1 + 4)/2 = 3.25
	expected := 3.25
	if math.Abs(result-expected) > 0.01 {
		t.Fatalf("expected ATR=%.4f, got %.4f", expected, result)
	}
}
