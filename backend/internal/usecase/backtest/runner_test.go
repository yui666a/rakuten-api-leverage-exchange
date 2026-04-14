package backtest

import (
	"context"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestMergeCandleEvents_PrefersHigherOnSameTimestamp(t *testing.T) {
	primary := []entity.Candle{
		{Time: 2000, Close: 2},
	}
	higher := []entity.Candle{
		{Time: 2000, Close: 20},
	}
	events := mergeCandleEvents(primary, higher, "PT15M", "PT1H", 7)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	first, ok := events[0].(entity.CandleEvent)
	if !ok {
		t.Fatalf("expected CandleEvent, got %T", events[0])
	}
	if first.Interval != "PT1H" {
		t.Fatalf("expected higher timeframe first, got %s", first.Interval)
	}
}

func TestBacktestRunner_Run(t *testing.T) {
	primary := make([]entity.Candle, 0, 80)
	higher := make([]entity.Candle, 0, 20)
	baseTime := int64(1_770_000_000_000)

	price := 100.0
	for i := 0; i < 80; i++ {
		price += math.Sin(float64(i)/7.0) * 0.8
		ts := baseTime + int64(i)*15*60*1000
		primary = append(primary, entity.Candle{
			Open:  price - 0.5,
			High:  price + 1.0,
			Low:   price - 1.0,
			Close: price,
			Time:  ts,
		})
	}
	for i := 0; i < 20; i++ {
		idx := i * 4
		ts := primary[idx].Time
		p := primary[idx].Close
		higher = append(higher, entity.Candle{
			Open:  p - 0.6,
			High:  p + 1.2,
			Low:   p - 1.2,
			Close: p,
			Time:  ts,
		})
	}

	runner := NewBacktestRunner()
	result, err := runner.Run(context.Background(), RunInput{
		Config: entity.BacktestConfig{
			Symbol:           "BTC_JPY",
			SymbolID:         7,
			PrimaryInterval:  "PT15M",
			HigherTFInterval: "PT1H",
			FromTimestamp:    primary[0].Time,
			ToTimestamp:      primary[len(primary)-1].Time,
			InitialBalance:   100000,
			SpreadPercent:    0.1,
			DailyCarryCost:   0.04,
		},
		RiskConfig: entity.RiskConfig{
			MaxPositionAmount:    1_000_000_000,
			MaxDailyLoss:         1_000_000_000,
			StopLossPercent:      5,
			TakeProfitPercent:    10,
			InitialCapital:       100000,
			MaxConsecutiveLosses: 0,
			CooldownMinutes:      0,
		},
		TradeAmount:    0.01,
		PrimaryCandles: primary,
		HigherCandles:  higher,
	})
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if result == nil {
		t.Fatal("result must not be nil")
	}
	if result.Summary.InitialBalance != 100000 {
		t.Fatalf("unexpected initial balance: %f", result.Summary.InitialBalance)
	}
	if result.Summary.FinalBalance <= 0 {
		t.Fatalf("final balance must be positive: %f", result.Summary.FinalBalance)
	}
	if len(result.ID) != 26 {
		t.Fatalf("expected ULID length 26, got %d id=%s", len(result.ID), result.ID)
	}
}
