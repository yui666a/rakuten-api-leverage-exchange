package backtest

import (
	"context"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestBuildGrid(t *testing.T) {
	grid, err := buildGrid([]ParamRange{
		{Name: "stop_loss_percent", Min: 1, Max: 2, Step: 1},
		{Name: "take_profit_percent", Min: 3, Max: 4, Step: 1},
	})
	if err != nil {
		t.Fatalf("buildGrid error: %v", err)
	}
	if len(grid) != 4 {
		t.Fatalf("expected 4 combos, got %d", len(grid))
	}
}

func TestOptimizer_Optimize(t *testing.T) {
	primary := make([]entity.Candle, 0, 80)
	baseTime := int64(1_770_000_000_000)
	price := 100.0
	for i := 0; i < 80; i++ {
		price += math.Sin(float64(i)/5.0) * 1.2
		ts := baseTime + int64(i)*15*60*1000
		primary = append(primary, entity.Candle{
			Open:  price - 0.7,
			High:  price + 1.1,
			Low:   price - 1.1,
			Close: price,
			Time:  ts,
		})
	}

	optimizer := NewOptimizer(NewBacktestRunner())
	results, err := optimizer.Optimize(context.Background(), RunInput{
		Config: entity.BacktestConfig{
			Symbol:          "BTC_JPY",
			SymbolID:        7,
			PrimaryInterval: "PT15M",
			FromTimestamp:   primary[0].Time,
			ToTimestamp:     primary[len(primary)-1].Time,
			InitialBalance:  100000,
			SpreadPercent:   0.1,
			DailyCarryCost:  0.04,
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
	}, []ParamRange{
		{Name: "stop_loss_percent", Min: 3, Max: 5, Step: 1},
		{Name: "take_profit_percent", Min: 6, Max: 8, Step: 1},
	}, OptimizeConfig{
		MaxEvals: 10,
		TopN:     3,
		Seed:     1,
	})
	if err != nil {
		t.Fatalf("optimize error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected non-empty optimization results")
	}
	if len(results) > 3 {
		t.Fatalf("expected at most top 3 results, got %d", len(results))
	}
}
