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

func TestSummaryReporter_BuildSummary_IncludesDrawdownTimeInMarketExpectancy(t *testing.T) {
	reporter := NewSummaryReporter()
	cfg := entity.BacktestConfig{
		FromTimestamp:  1_000,
		ToTimestamp:    10_000,
		InitialBalance: 1000,
	}
	// 5 equity points: seed + 4 bars.
	// peak 1000 -> trough 950 (-5%) -> recover 1000 -> new peak 1100 -> 1050 (-4.5% unrecovered)
	equity := []EquityPoint{
		{Timestamp: 1_000, Equity: 1000}, // seed
		{Timestamp: 2_000, Equity: 950},  // trough of recovered DD (5%)
		{Timestamp: 3_000, Equity: 1000}, // recovered (= peak)
		{Timestamp: 4_000, Equity: 1100}, // new peak
		{Timestamp: 5_000, Equity: 1050}, // -4.5% DD unrecovered at end
	}
	// Two trades cover bars 2-3 (in-market) and bar 5 (in-market). Bar 4
	// (timestamp 4_000) is flat. Bar timestamps = 2_000, 3_000, 4_000, 5_000.
	trades := []entity.BacktestTradeRecord{
		{TradeID: 1, EntryTime: 2_000, ExitTime: 3_000, PnL: 50},
		{TradeID: 2, EntryTime: 5_000, ExitTime: 5_000, PnL: -10},
	}
	s := reporter.BuildSummary(cfg, 1050, trades, equity)

	if len(s.DrawdownPeriods) != 1 {
		t.Fatalf("DrawdownPeriods = %d, want 1; got %+v", len(s.DrawdownPeriods), s.DrawdownPeriods)
	}
	if s.DrawdownThreshold != DefaultDrawdownThreshold {
		t.Fatalf("DrawdownThreshold = %v, want %v", s.DrawdownThreshold, DefaultDrawdownThreshold)
	}
	if s.UnrecoveredDrawdown == nil {
		t.Fatalf("UnrecoveredDrawdown should be set at run end")
	}
	if s.TimeInMarketRatio < 0.74 || s.TimeInMarketRatio > 0.76 {
		t.Fatalf("TimeInMarketRatio = %v, want ~0.75", s.TimeInMarketRatio)
	}
	if s.LongestFlatStreakBars != 1 {
		t.Fatalf("LongestFlatStreakBars = %d, want 1", s.LongestFlatStreakBars)
	}
	if s.ExpectancyPerTrade != 20 {
		t.Fatalf("ExpectancyPerTrade = %v, want 20", s.ExpectancyPerTrade)
	}
	if s.AvgWinJPY != 50 || s.AvgLossJPY != 10 {
		t.Fatalf("avg win/loss = %v/%v", s.AvgWinJPY, s.AvgLossJPY)
	}
}

func TestSummaryReporter_BuildSummary_IncludesBreakdowns(t *testing.T) {
	reporter := NewSummaryReporter()
	cfg := entity.BacktestConfig{
		FromTimestamp:  1000,
		ToTimestamp:    5000,
		InitialBalance: 1000,
	}
	trades := []entity.BacktestTradeRecord{
		{TradeID: 1, EntryTime: 1000, ExitTime: 1600, PnL: 100,
			ReasonEntry: "trend follow: EMA12 > EMA26",
			ReasonExit:  "take_profit"},
		{TradeID: 2, EntryTime: 2000, ExitTime: 2600, PnL: -50,
			ReasonEntry: "contrarian: RSI overbought",
			ReasonExit:  "stop_loss"},
		{TradeID: 3, EntryTime: 3000, ExitTime: 3600, PnL: 30,
			ReasonEntry: "breakout: price above BB upper",
			ReasonExit:  "reverse_signal"},
		{TradeID: 4, EntryTime: 4000, ExitTime: 4600, PnL: 20,
			ReasonEntry: "trend follow: SMA20 > SMA50",
			ReasonExit:  "take_profit"},
	}
	equityPoints := []EquityPoint{
		{Timestamp: 1000, Equity: 1000},
		{Timestamp: 4600, Equity: 1100},
	}

	s := reporter.BuildSummary(cfg, 1100, trades, equityPoints)

	// Invariants to guard against cross-wire bugs:
	// 1) Sum of bucket trade counts must equal TotalTrades.
	sumExit := 0
	for _, b := range s.ByExitReason {
		sumExit += b.Trades
	}
	if sumExit != s.TotalTrades {
		t.Fatalf("ByExitReason total %d != TotalTrades %d (map=%v)", sumExit, s.TotalTrades, s.ByExitReason)
	}
	sumSig := 0
	for _, b := range s.BySignalSource {
		sumSig += b.Trades
	}
	if sumSig != s.TotalTrades {
		t.Fatalf("BySignalSource total %d != TotalTrades %d (map=%v)", sumSig, s.TotalTrades, s.BySignalSource)
	}

	// 2) Expected buckets present.
	if s.ByExitReason["take_profit"].Trades != 2 {
		t.Fatalf("take_profit bucket trades = %d, want 2", s.ByExitReason["take_profit"].Trades)
	}
	if s.ByExitReason["stop_loss"].Trades != 1 {
		t.Fatalf("stop_loss bucket trades = %d, want 1", s.ByExitReason["stop_loss"].Trades)
	}
	if s.ByExitReason["reverse_signal"].Trades != 1 {
		t.Fatalf("reverse_signal bucket trades = %d, want 1", s.ByExitReason["reverse_signal"].Trades)
	}
	if s.BySignalSource["trend_follow"].Trades != 2 {
		t.Fatalf("trend_follow bucket trades = %d, want 2", s.BySignalSource["trend_follow"].Trades)
	}

	// 3) Aggregates inside a bucket.
	// trend_follow: pnl 100+20 = 120, 2 wins, WR 100%, PF 0 (no losses)
	tf := s.BySignalSource["trend_follow"]
	if tf.TotalPnL != 120 || tf.WinTrades != 2 || tf.LossTrades != 0 {
		t.Fatalf("trend_follow bucket = %+v", tf)
	}
}
