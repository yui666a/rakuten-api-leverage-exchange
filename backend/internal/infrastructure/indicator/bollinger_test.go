package indicator

import (
	"math"
	"testing"
)

func TestBollingerBands_InsufficientData(t *testing.T) {
	upper, middle, lower, bw := BollingerBands([]float64{1, 2, 3}, 20, 2.0)
	if !math.IsNaN(upper) || !math.IsNaN(middle) || !math.IsNaN(lower) || !math.IsNaN(bw) {
		t.Fatal("expected NaN for insufficient data")
	}
}

func TestBollingerBands_ConstantPrices(t *testing.T) {
	prices := make([]float64, 20)
	for i := range prices {
		prices[i] = 100.0
	}
	upper, middle, lower, bw := BollingerBands(prices, 20, 2.0)
	if middle != 100.0 {
		t.Fatalf("expected middle=100, got %f", middle)
	}
	if upper != 100.0 || lower != 100.0 {
		t.Fatalf("expected upper=lower=100 for constant prices, got upper=%f lower=%f", upper, lower)
	}
	if bw != 0.0 {
		t.Fatalf("expected bandwidth=0 for constant prices, got %f", bw)
	}
}

func TestBollingerBands_KnownValues(t *testing.T) {
	// 5-period BB with multiplier 2.0, simple dataset
	prices := []float64{10, 11, 12, 11, 10}
	upper, middle, lower, bw := BollingerBands(prices, 5, 2.0)

	// SMA = 10.8
	expectedMiddle := 10.8
	if math.Abs(middle-expectedMiddle) > 0.01 {
		t.Fatalf("expected middle=%.2f, got %.4f", expectedMiddle, middle)
	}

	// stddev = sqrt(((10-10.8)^2 + (11-10.8)^2 + (12-10.8)^2 + (11-10.8)^2 + (10-10.8)^2) / 5)
	// = sqrt((0.64 + 0.04 + 1.44 + 0.04 + 0.64) / 5) = sqrt(0.56) ≈ 0.7483
	expectedStdDev := 0.7483
	expectedUpper := expectedMiddle + 2*expectedStdDev
	expectedLower := expectedMiddle - 2*expectedStdDev

	if math.Abs(upper-expectedUpper) > 0.01 {
		t.Fatalf("expected upper=%.4f, got %.4f", expectedUpper, upper)
	}
	if math.Abs(lower-expectedLower) > 0.01 {
		t.Fatalf("expected lower=%.4f, got %.4f", expectedLower, lower)
	}
	if bw <= 0 {
		t.Fatalf("expected positive bandwidth, got %f", bw)
	}
}
