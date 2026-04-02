package indicator

import (
	"math"
	"testing"
)

func TestEMA_Basic(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50}
	result := EMA(prices, 5)
	if math.IsNaN(result) {
		t.Fatal("expected valid EMA, got NaN")
	}
}

func TestEMA_InsufficientData(t *testing.T) {
	prices := []float64{10, 20}
	result := EMA(prices, 5)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for insufficient data, got %f", result)
	}
}

func TestEMA_FirstValueIsSMA(t *testing.T) {
	prices := []float64{10, 20, 30}
	result := EMA(prices, 3)
	if result != 20 {
		t.Fatalf("expected EMA=20 (same as SMA for period-length input), got %f", result)
	}
}

func TestEMA_MoreWeightOnRecent(t *testing.T) {
	prices := []float64{10, 10, 10, 10, 10, 50}
	emaVal := EMA(prices, 5)
	smaVal := SMA(prices, 5)
	if emaVal <= smaVal {
		t.Fatalf("expected EMA > SMA for rising prices, got EMA=%f SMA=%f", emaVal, smaVal)
	}
}

func TestEMASeries(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50, 60}
	result := EMASeries(prices, 3)
	if len(result) != 4 {
		t.Fatalf("expected 4 values, got %d", len(result))
	}
	if result[0] != 20 {
		t.Fatalf("first EMA should be SMA=20, got %f", result[0])
	}
	for i := 1; i < len(result); i++ {
		if result[i] <= result[i-1] {
			t.Fatalf("EMA should be rising at index %d: %f <= %f", i, result[i], result[i-1])
		}
	}
}
