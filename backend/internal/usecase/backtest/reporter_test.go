package backtest

import (
	"math"
	"testing"
	"time"

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
	equityPoints := []EquityPoint{
		{Timestamp: 1000, Equity: 1000},
		{Timestamp: 1600, Equity: 1100},
		{Timestamp: 2600, Equity: 1050},
	}
	s := reporter.BuildSummary(cfg, 1050, trades, equityPoints)

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

func TestSummaryReporter_MaxDrawdownFromEquityPoints(t *testing.T) {
	reporter := NewSummaryReporter()
	cfg := entity.BacktestConfig{
		FromTimestamp:  1000,
		ToTimestamp:    4000,
		InitialBalance: 1000,
	}

	s := reporter.BuildSummary(
		cfg,
		950,
		nil,
		[]EquityPoint{
			{Timestamp: 1000, Equity: 1000},
			{Timestamp: 2000, Equity: 1200}, // peak
			{Timestamp: 3000, Equity: 900},  // trough -> 25% DD
			{Timestamp: 4000, Equity: 950},
		},
	)
	if math.Abs(s.MaxDrawdown-0.25) > 1e-9 {
		t.Fatalf("expected max drawdown 0.25, got %f", s.MaxDrawdown)
	}
	if math.Abs(s.MaxDrawdownBalance-900) > 1e-9 {
		t.Fatalf("expected drawdown trough 900, got %f", s.MaxDrawdownBalance)
	}
}

func TestSummaryReporter_SharpeFromDailyCloseEquity(t *testing.T) {
	reporter := NewSummaryReporter()
	cfg := entity.BacktestConfig{
		FromTimestamp:  1_777_000_000_000,
		ToTimestamp:    1_777_500_000_000,
		InitialBalance: 1000,
	}
	s := reporter.BuildSummary(
		cfg,
		1300,
		nil,
		[]EquityPoint{
			// day1 close -> 1000
			{Timestamp: mustJSTMillis(2026, 4, 10, 23, 59), Equity: 1000},
			// day2 close -> 1100 (return +10%)
			{Timestamp: mustJSTMillis(2026, 4, 11, 23, 59), Equity: 1100},
			// day3 close -> 1210 (return +10%)
			{Timestamp: mustJSTMillis(2026, 4, 12, 23, 59), Equity: 1210},
		},
	)
	if s.SharpeRatio != 0 {
		t.Fatalf("expected zero sharpe for zero-variance daily returns, got %f", s.SharpeRatio)
	}
}

func mustJSTMillis(y int, m time.Month, d, h, min int) int64 {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	return time.Date(y, m, d, h, min, 0, 0, loc).UnixMilli()
}
