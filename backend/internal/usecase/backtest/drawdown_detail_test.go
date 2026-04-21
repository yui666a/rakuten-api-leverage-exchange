package backtest

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestDetectDrawdowns_TwoDistinctEpisodes(t *testing.T) {
	// Two clean drawdowns:
	//   pk1=100 -> trough 88 (DD 12%) -> recover to 100 (new pk)
	//   pk2=110 -> trough 99 (DD 10%) -> recover to 110 (new pk)
	points := []EquityPoint{
		{Timestamp: 1, Equity: 100}, // pk1
		{Timestamp: 2, Equity: 88},  // trough1
		{Timestamp: 3, Equity: 100}, // recovered
		{Timestamp: 4, Equity: 110}, // pk2
		{Timestamp: 5, Equity: 99},  // trough2
		{Timestamp: 6, Equity: 110}, // recovered
	}
	got, unrec := DetectDrawdowns(points, 0.02)
	if unrec != nil {
		t.Fatalf("no unrecovered DD expected, got %+v", unrec)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 episodes, got %d: %+v", len(got), got)
	}
	if math.Abs(got[0].Depth-0.12) > 1e-9 {
		t.Fatalf("episode 0 depth = %v, want 0.12", got[0].Depth)
	}
	if got[0].FromTimestamp != 1 || got[0].ToTimestamp != 2 || got[0].RecoveredAt != 3 {
		t.Fatalf("episode 0 timestamps = %+v", got[0])
	}
	if got[0].DurationBars != 1 || got[0].RecoveryBars != 1 {
		t.Fatalf("episode 0 bar counts = %+v", got[0])
	}
	if math.Abs(got[1].Depth-0.1) > 1e-9 {
		t.Fatalf("episode 1 depth = %v, want 0.1", got[1].Depth)
	}
}

func TestDetectDrawdowns_Unrecovered(t *testing.T) {
	// DD that never recovers within the run.
	points := []EquityPoint{
		{Timestamp: 1, Equity: 100},
		{Timestamp: 2, Equity: 90}, // 10% DD
		{Timestamp: 3, Equity: 85}, // 15% DD, deeper
		{Timestamp: 4, Equity: 87}, // bounce but still below peak
	}
	got, unrec := DetectDrawdowns(points, 0.02)
	if unrec == nil {
		t.Fatalf("expected unrecovered DD")
	}
	if math.Abs(unrec.Depth-0.15) > 1e-9 {
		t.Fatalf("unrecovered depth = %v, want 0.15", unrec.Depth)
	}
	if unrec.RecoveredAt != 0 {
		t.Fatalf("RecoveredAt should be 0 when unrecovered: %v", unrec.RecoveredAt)
	}
	if unrec.RecoveryBars != -1 {
		t.Fatalf("RecoveryBars should be -1 when unrecovered: %v", unrec.RecoveryBars)
	}
	// An unrecovered DD is NOT also in the recovered list.
	if len(got) != 0 {
		t.Fatalf("recovered list should be empty, got %d", len(got))
	}
}

func TestDetectDrawdowns_BelowThresholdIsIgnored(t *testing.T) {
	// 1% dip should be skipped at default 2% threshold.
	points := []EquityPoint{
		{Timestamp: 1, Equity: 100},
		{Timestamp: 2, Equity: 99},
		{Timestamp: 3, Equity: 101},
	}
	got, unrec := DetectDrawdowns(points, 0.02)
	if len(got) != 0 || unrec != nil {
		t.Fatalf("no DD expected: recovered=%v unrecovered=%v", got, unrec)
	}
}

func TestDetectDrawdowns_Empty(t *testing.T) {
	got, unrec := DetectDrawdowns(nil, 0.02)
	if got != nil || unrec != nil {
		t.Fatalf("expected empty results, got %v / %v", got, unrec)
	}
}

// ---------- Time-in-market ----------

func TestComputeTimeInMarket_Full(t *testing.T) {
	// 10 bars, 4 non-overlapping trades covering bars [0..1], [3..3], [5..7].
	// inMarket bars = 2+1+3 = 6, ratio = 6/10 = 0.6.
	// Flat streaks between: 1 (bar 2), 1 (bar 4), 2 (bars 8,9) -> longest = 2.
	totalBars := 10
	barTimestamps := []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	trades := []entity.BacktestTradeRecord{
		{EntryTime: 0, ExitTime: 1},
		{EntryTime: 3, ExitTime: 3},
		{EntryTime: 5, ExitTime: 7},
	}

	ratio, longestFlat := ComputeTimeInMarket(trades, barTimestamps, totalBars)
	if math.Abs(ratio-0.6) > 1e-9 {
		t.Fatalf("ratio = %v, want 0.6", ratio)
	}
	if longestFlat != 2 {
		t.Fatalf("longestFlat = %d, want 2", longestFlat)
	}
}

func TestComputeTimeInMarket_NoTrades(t *testing.T) {
	ratio, longest := ComputeTimeInMarket(nil, []int64{0, 1, 2}, 3)
	if ratio != 0 {
		t.Fatalf("ratio = %v, want 0", ratio)
	}
	// All bars flat -> longest streak = 3.
	if longest != 3 {
		t.Fatalf("longestFlat = %d, want 3", longest)
	}
}

func TestComputeTimeInMarket_AlwaysInMarket(t *testing.T) {
	// One long trade covering the entire window.
	totalBars := 5
	barTimestamps := []int64{0, 1, 2, 3, 4}
	trades := []entity.BacktestTradeRecord{{EntryTime: 0, ExitTime: 4}}
	ratio, longest := ComputeTimeInMarket(trades, barTimestamps, totalBars)
	if ratio != 1.0 {
		t.Fatalf("ratio = %v, want 1.0", ratio)
	}
	if longest != 0 {
		t.Fatalf("longestFlat = %d, want 0", longest)
	}
}

func TestComputeTimeInMarket_EmptyBars(t *testing.T) {
	ratio, longest := ComputeTimeInMarket(nil, nil, 0)
	if ratio != 0 || longest != 0 {
		t.Fatalf("empty bars should be zero, got %v / %d", ratio, longest)
	}
}

// ---------- Expectancy ----------

func TestComputeExpectancy_MixedWR(t *testing.T) {
	// 3 wins (100, 80, 60) / 2 losses (-50, -30) -> WR=0.6, AvgWin=80, AvgLoss=40
	// Expectancy = 0.6*80 - 0.4*40 = 48 - 16 = 32
	trades := []entity.BacktestTradeRecord{
		{PnL: 100}, {PnL: -50}, {PnL: 80}, {PnL: -30}, {PnL: 60},
	}
	e, aw, al := ComputeExpectancy(trades)
	if math.Abs(e-32) > 1e-9 {
		t.Fatalf("Expectancy = %v, want 32", e)
	}
	if math.Abs(aw-80) > 1e-9 {
		t.Fatalf("AvgWin = %v, want 80", aw)
	}
	if math.Abs(al-40) > 1e-9 {
		t.Fatalf("AvgLoss = %v, want 40", al)
	}
}

func TestComputeExpectancy_NoTrades(t *testing.T) {
	e, aw, al := ComputeExpectancy(nil)
	if e != 0 || aw != 0 || al != 0 {
		t.Fatalf("empty input should be zero, got e=%v aw=%v al=%v", e, aw, al)
	}
}

func TestComputeExpectancy_AllWins(t *testing.T) {
	// No losses -> AvgLoss=0, Expectancy = 1*AvgWin = AvgWin
	trades := []entity.BacktestTradeRecord{{PnL: 100}, {PnL: 50}}
	e, aw, al := ComputeExpectancy(trades)
	if al != 0 {
		t.Fatalf("AvgLoss should be 0, got %v", al)
	}
	if aw != 75 {
		t.Fatalf("AvgWin = %v, want 75", aw)
	}
	if e != 75 {
		t.Fatalf("Expectancy = %v, want 75", e)
	}
}

func TestComputeExpectancy_AllLosses(t *testing.T) {
	trades := []entity.BacktestTradeRecord{{PnL: -20}, {PnL: -40}}
	e, aw, al := ComputeExpectancy(trades)
	if aw != 0 {
		t.Fatalf("AvgWin should be 0, got %v", aw)
	}
	if al != 30 {
		t.Fatalf("AvgLoss = %v, want 30 (absolute)", al)
	}
	if e != -30 {
		t.Fatalf("Expectancy = %v, want -30", e)
	}
}
