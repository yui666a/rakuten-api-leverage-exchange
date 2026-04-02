package indicator

import (
	"math"
	"testing"
)

func TestSMA_Basic(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50}
	result := SMA(prices, 5)
	if result != 30 {
		t.Fatalf("expected SMA=30, got %f", result)
	}
}

func TestSMA_Period3(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50}
	result := SMA(prices, 3)
	expected := 40.0
	if result != expected {
		t.Fatalf("expected SMA=%f, got %f", expected, result)
	}
}

func TestSMA_InsufficientData(t *testing.T) {
	prices := []float64{10, 20}
	result := SMA(prices, 5)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for insufficient data, got %f", result)
	}
}

func TestSMA_EmptyInput(t *testing.T) {
	result := SMA([]float64{}, 5)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for empty input, got %f", result)
	}
}

func TestSMASeries(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50, 60}
	result := SMASeries(prices, 3)
	expected := []float64{20, 30, 40, 50}
	if len(result) != len(expected) {
		t.Fatalf("expected %d values, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if math.Abs(v-expected[i]) > 0.0001 {
			t.Fatalf("index %d: expected %f, got %f", i, expected[i], v)
		}
	}
}
