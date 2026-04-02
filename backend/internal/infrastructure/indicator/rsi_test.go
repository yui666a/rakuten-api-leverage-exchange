package indicator

import (
	"math"
	"testing"
)

func TestRSI_AllGains(t *testing.T) {
	prices := make([]float64, 15)
	for i := range prices {
		prices[i] = float64(i + 1)
	}
	result := RSI(prices, 14)
	if result != 100 {
		t.Fatalf("expected RSI=100 for all gains, got %f", result)
	}
}

func TestRSI_AllLosses(t *testing.T) {
	prices := make([]float64, 15)
	for i := range prices {
		prices[i] = float64(15 - i)
	}
	result := RSI(prices, 14)
	if result != 0 {
		t.Fatalf("expected RSI=0 for all losses, got %f", result)
	}
}

func TestRSI_InsufficientData(t *testing.T) {
	prices := []float64{10, 20, 30}
	result := RSI(prices, 14)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for insufficient data, got %f", result)
	}
}

func TestRSI_Range(t *testing.T) {
	prices := []float64{44, 44.34, 44.09, 43.61, 44.33, 44.83, 45.10, 45.42, 45.84, 46.08, 45.89, 46.03, 45.61, 46.28, 46.28}
	result := RSI(prices, 14)
	if result < 0 || result > 100 {
		t.Fatalf("RSI should be between 0 and 100, got %f", result)
	}
}

func TestRSI_FlatPrices(t *testing.T) {
	prices := make([]float64, 15)
	for i := range prices {
		prices[i] = 10
	}
	result := RSI(prices, 14)
	if result != 50 {
		t.Fatalf("expected RSI=50 for flat prices, got %f", result)
	}
}

func TestRSI_MidRange(t *testing.T) {
	prices := []float64{10, 11, 10, 11, 10, 11, 10, 11, 10, 11, 10, 11, 10, 11, 10}
	result := RSI(prices, 14)
	if math.Abs(result-50) > 5 {
		t.Fatalf("expected RSI near 50 for alternating prices, got %f", result)
	}
}
