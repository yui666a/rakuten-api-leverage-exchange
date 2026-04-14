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

func TestOptimizer_Refine(t *testing.T) {
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

	baseInput := RunInput{
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
	}

	ranges := []ParamRange{
		{Name: "stop_loss_percent", Min: 3, Max: 7, Step: 2},
		{Name: "take_profit_percent", Min: 6, Max: 12, Step: 3},
	}

	optimizer := NewOptimizer(NewBacktestRunner())

	// Phase 2a: coarse search
	coarseResults, err := optimizer.Optimize(context.Background(), baseInput, ranges, OptimizeConfig{
		MaxEvals: 20,
		TopN:     5,
		Seed:     42,
	})
	if err != nil {
		t.Fatalf("coarse optimize error: %v", err)
	}
	if len(coarseResults) == 0 {
		t.Fatal("expected non-empty coarse results")
	}

	// Phase 2b: refinement
	refinedResults, err := optimizer.Refine(context.Background(), baseInput, coarseResults, ranges, RefineConfig{
		TopN:     3,
		StepDiv:  4.0,
		MaxEvals: 50,
		Workers:  2,
	})
	if err != nil {
		t.Fatalf("refine error: %v", err)
	}
	if len(refinedResults) == 0 {
		t.Fatal("expected non-empty refined results")
	}

	// Verify sorted by Sharpe desc
	for i := 1; i < len(refinedResults); i++ {
		if refinedResults[i].Summary.SharpeRatio > refinedResults[i-1].Summary.SharpeRatio {
			t.Fatalf("results not sorted by Sharpe desc at index %d", i)
		}
	}
}

func TestBuildNeighborhoodRanges(t *testing.T) {
	center := map[string]float64{
		"stop_loss_percent":   5.0,
		"take_profit_percent": 10.0,
	}
	origRanges := []ParamRange{
		{Name: "stop_loss_percent", Min: 1, Max: 10, Step: 2},
		{Name: "take_profit_percent", Min: 3, Max: 20, Step: 3},
	}

	result := buildNeighborhoodRanges(center, origRanges, 4.0)

	if len(result) != 2 {
		t.Fatalf("expected 2 ranges, got %d", len(result))
	}

	// stop_loss: center=5, step=2, so neighborhood [3, 7], fineStep=0.5
	sl := result[0]
	if sl.Min != 3 || sl.Max != 7 {
		t.Fatalf("stop_loss range: expected [3, 7], got [%v, %v]", sl.Min, sl.Max)
	}
	if sl.Step != 0.5 {
		t.Fatalf("stop_loss step: expected 0.5, got %v", sl.Step)
	}

	// take_profit: center=10, step=3, so neighborhood [7, 13], fineStep=0.75
	tp := result[1]
	if tp.Min != 7 || tp.Max != 13 {
		t.Fatalf("take_profit range: expected [7, 13], got [%v, %v]", tp.Min, tp.Max)
	}
	if tp.Step != 0.75 {
		t.Fatalf("take_profit step: expected 0.75, got %v", tp.Step)
	}
}

func TestBuildNeighborhoodRanges_Clamping(t *testing.T) {
	center := map[string]float64{"stop_loss_percent": 1.5}
	origRanges := []ParamRange{
		{Name: "stop_loss_percent", Min: 1, Max: 10, Step: 2},
	}

	result := buildNeighborhoodRanges(center, origRanges, 4.0)
	sl := result[0]
	// center=1.5, step=2 -> neighborhood max(1, 1.5-2)=1, min(10, 1.5+2)=3.5
	if sl.Min != 1 {
		t.Fatalf("clamped min: expected 1, got %v", sl.Min)
	}
	if sl.Max != 3.5 {
		t.Fatalf("clamped max: expected 3.5, got %v", sl.Max)
	}
}

func TestDeduplicateCombos(t *testing.T) {
	combos := []map[string]float64{
		{"a": 1.0, "b": 2.0},
		{"a": 1.0, "b": 2.0},
		{"a": 1.0, "b": 3.0},
	}
	result := deduplicateCombos(combos)
	if len(result) != 2 {
		t.Fatalf("expected 2 unique combos, got %d", len(result))
	}
}
