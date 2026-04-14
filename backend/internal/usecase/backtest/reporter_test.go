package backtest

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestSummaryReporter_BuildSummary(t *testing.T) {
	reporter := NewSummaryReporter()
	cfg := entity.BacktestConfig{
		FromTimestamp:  1000,
		ToTimestamp:    3000,
		InitialBalance: 1000,
	}
	trades := []entity.BacktestTradeRecord{
		{
			TradeID: 1, EntryTime: 1000, ExitTime: 1600,
			PnL: 100, CarryingCost: 10, SpreadCost: 4,
		},
		{
			TradeID: 2, EntryTime: 2000, ExitTime: 2600,
			PnL: -50, CarryingCost: 5, SpreadCost: 3,
		},
	}
	s := reporter.BuildSummary(cfg, 1050, trades)

	if s.TotalTrades != 2 || s.WinTrades != 1 || s.LossTrades != 1 {
		t.Fatalf("unexpected trade counts: %+v", s)
	}
	if math.Abs(s.TotalReturn-0.05) > 1e-9 {
		t.Fatalf("expected total return 0.05, got %f", s.TotalReturn)
	}
	if math.Abs(s.WinRate-50.0) > 1e-9 {
		t.Fatalf("expected win rate 50, got %f", s.WinRate)
	}
	if math.Abs(s.ProfitFactor-2.0) > 1e-9 {
		t.Fatalf("expected profit factor 2.0, got %f", s.ProfitFactor)
	}
	if s.TotalCarryingCost != 15 {
		t.Fatalf("expected carrying cost 15, got %f", s.TotalCarryingCost)
	}
	if s.TotalSpreadCost != 7 {
		t.Fatalf("expected spread cost 7, got %f", s.TotalSpreadCost)
	}
}
