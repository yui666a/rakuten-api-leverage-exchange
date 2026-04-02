package indicator

import (
	"math"
	"testing"
)

func TestMACD_Basic(t *testing.T) {
	prices := make([]float64, 35)
	for i := range prices {
		prices[i] = float64(100 + i)
	}
	macdLine, signalLine, histogram := MACD(prices, 12, 26, 9)
	if math.IsNaN(macdLine) {
		t.Fatal("expected valid MACD line, got NaN")
	}
	if math.IsNaN(signalLine) {
		t.Fatal("expected valid signal line, got NaN")
	}
	if math.IsNaN(histogram) {
		t.Fatal("expected valid histogram, got NaN")
	}
	if math.Abs(histogram-(macdLine-signalLine)) > 0.0001 {
		t.Fatalf("histogram should be MACD-Signal, got %f", histogram)
	}
}

func TestMACD_InsufficientData(t *testing.T) {
	prices := make([]float64, 10)
	macdLine, signalLine, histogram := MACD(prices, 12, 26, 9)
	if !math.IsNaN(macdLine) || !math.IsNaN(signalLine) || !math.IsNaN(histogram) {
		t.Fatal("expected NaN for insufficient data")
	}
}

func TestMACD_RisingPrices(t *testing.T) {
	prices := make([]float64, 40)
	for i := range prices {
		prices[i] = float64(100 + i*2)
	}
	macdLine, _, _ := MACD(prices, 12, 26, 9)
	if macdLine <= 0 {
		t.Fatalf("expected positive MACD for rising prices, got %f", macdLine)
	}
}

func TestMACD_FallingPrices(t *testing.T) {
	prices := make([]float64, 40)
	for i := range prices {
		prices[i] = float64(200 - i*2)
	}
	macdLine, _, _ := MACD(prices, 12, 26, 9)
	if macdLine >= 0 {
		t.Fatalf("expected negative MACD for falling prices, got %f", macdLine)
	}
}
