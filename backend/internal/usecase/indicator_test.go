package usecase

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestIndicatorCalculator_Calculate(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	// Generate 50 candles (needed for SMA50)
	candles := make([]entity.Candle, 50)
	for i := range candles {
		candles[i] = entity.Candle{
			Close: float64(100 + i),
			Time:  int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	calc := NewIndicatorCalculator(repo)

	result, err := calc.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.SymbolID != 7 {
		t.Fatalf("expected symbolID 7, got %d", result.SymbolID)
	}

	if result.SMA20 == nil {
		t.Fatal("SMA20 should not be nil with 50 data points")
	}

	if result.SMA50 == nil {
		t.Fatal("SMA50 should not be nil with 50 data points")
	}

	if result.RSI14 == nil {
		t.Fatal("RSI14 should not be nil with 50 data points")
	}

	if *result.RSI14 < 0 || *result.RSI14 > 100 {
		t.Fatalf("RSI should be 0-100, got %f", *result.RSI14)
	}
}

func TestIndicatorCalculator_InsufficientData(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	// Only 5 candles - not enough for any meaningful indicator
	candles := make([]entity.Candle, 5)
	for i := range candles {
		candles[i] = entity.Candle{
			Close: float64(100 + i),
			Time:  int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	calc := NewIndicatorCalculator(repo)

	result, err := calc.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SMA20 requires 20 data points, so should be nil
	if result.SMA20 != nil {
		t.Fatalf("SMA20 should be nil with only 5 data points, got %f", *result.SMA20)
	}
}

func TestIndicatorCalculator_JSONSafe(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	// Only 5 candles - most indicators will be nil
	candles := make([]entity.Candle, 5)
	for i := range candles {
		candles[i] = entity.Candle{
			Close: float64(100 + i),
			Time:  int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	calc := NewIndicatorCalculator(repo)
	result, err := calc.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// NaN fields are nil, so JSON serialization should succeed
	_, err = json.Marshal(result)
	if err != nil {
		t.Fatalf("JSON marshal should not fail: %v", err)
	}
}
