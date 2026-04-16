package backtest

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

const (
	dayMillis  int64 = 24 * 60 * 60 * 1000
	weekMillis       = 7 * dayMillis
)

// makeTrade は ExitTime と PnL だけ指定可能な簡易ファクトリ。
func makeTrade(exitTime int64, pnl float64) entity.BacktestTradeRecord {
	return entity.BacktestTradeRecord{
		ExitTime: exitTime,
		PnL:      pnl,
	}
}

func TestComputeBiweeklyWinRate_EmptyTrades(t *testing.T) {
	got := ComputeBiweeklyWinRate(nil, 0, 20*dayMillis)
	if got != 0 {
		t.Fatalf("expected 0 for empty trades, got %v", got)
	}
}

func TestComputeBiweeklyWinRate_PeriodShorterThanWindow(t *testing.T) {
	trades := []entity.BacktestTradeRecord{
		makeTrade(1*dayMillis, 100),
		makeTrade(2*dayMillis, 100),
		makeTrade(3*dayMillis, 100),
	}
	// 期間が 13 日 < 14 日なのでウィンドウ不成立 → 0。
	got := ComputeBiweeklyWinRate(trades, 0, 13*dayMillis)
	if got != 0 {
		t.Fatalf("expected 0 for period < 14 days, got %v", got)
	}
}

func TestComputeBiweeklyWinRate_PeriodInverted(t *testing.T) {
	trades := []entity.BacktestTradeRecord{makeTrade(1*dayMillis, 100)}
	got := ComputeBiweeklyWinRate(trades, 100*dayMillis, 50*dayMillis)
	if got != 0 {
		t.Fatalf("expected 0 for inverted period, got %v", got)
	}
}

func TestComputeBiweeklyWinRate_SingleWindow100PercentWin(t *testing.T) {
	// 期間 = 14 日ちょうど → ウィンドウ 1 つ。
	// 3 件以上・全勝 → 100。
	trades := []entity.BacktestTradeRecord{
		makeTrade(1*dayMillis, 10),
		makeTrade(5*dayMillis, 20),
		makeTrade(10*dayMillis, 30),
	}
	got := ComputeBiweeklyWinRate(trades, 0, 14*dayMillis)
	if math.Abs(got-100) > 1e-9 {
		t.Fatalf("expected 100, got %v", got)
	}
}

func TestComputeBiweeklyWinRate_AllWindowsZeroWins(t *testing.T) {
	// 期間 16 日 → ウィンドウ 3 つ (start=0,1,2 day)。
	// 各ウィンドウ ([0,14)/[1,15)/[2,16)) に全滅トレードを 3 件以上入れる。
	var trades []entity.BacktestTradeRecord
	// day 3, 5, 7, 9, 11, 13 にトレードを配置 → 3 つのウィンドウ全てに >= 3 件入る。
	for _, d := range []int64{3, 5, 7, 9, 11, 13} {
		trades = append(trades, makeTrade(d*dayMillis, -5))
	}
	got := ComputeBiweeklyWinRate(trades, 0, 16*dayMillis)
	if math.Abs(got) > 1e-9 {
		t.Fatalf("expected 0 for all losses, got %v", got)
	}
}

func TestComputeBiweeklyWinRate_Mix2Wins1Loss(t *testing.T) {
	// 期間 14 日 → ウィンドウ 1 つ。2 勝 1 敗 → 200/3 ≈ 66.666...
	trades := []entity.BacktestTradeRecord{
		makeTrade(2*dayMillis, 10),
		makeTrade(5*dayMillis, 10),
		makeTrade(9*dayMillis, -5),
	}
	got := ComputeBiweeklyWinRate(trades, 0, 14*dayMillis)
	want := 200.0 / 3.0
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("expected ~%v, got %v", want, got)
	}
}

func TestComputeBiweeklyWinRate_SparseCoverageBelowFloor(t *testing.T) {
	// 期間 30 日 → ウィンドウ 17 個 (start = 0..16 day)。
	// 先頭 3 日間に全勝 3 件のみ配置 → [0,14)/[1,15)/[2,15)/[3,16) あたりだけ有効。
	// 有効ウィンドウ数を計算: トレードは day2, day3 に配置。
	// windowStart=s のウィンドウが有効なのは s<=2 かつ s+14>3 つまり s>-11 → s in {0,1,2}。
	// カバレッジ = 3/17 < 0.5 → 0 を返すべき。
	trades := []entity.BacktestTradeRecord{
		makeTrade(1*dayMillis, 10),
		makeTrade(2*dayMillis, 10),
		makeTrade(3*dayMillis, 10),
	}
	got := ComputeBiweeklyWinRate(trades, 0, 30*dayMillis)
	if got != 0 {
		t.Fatalf("expected 0 for coverage < 50%%, got %v", got)
	}
}

func TestComputeBiweeklyWinRate_SparseCoverageAtFloorAveragesWithPenaltyZeros(t *testing.T) {
	// 期間 15 日 → ウィンドウ 2 つ: [0,14) と [1,15)。
	// Window 1: 3 件全勝 (rate=100), Window 2: 1 件のみ (ペナルティ rate=0)。
	// カバレッジ = 1/2 = 0.5 → 閾値以上。
	// 平均 = (100 + 0) / 2 = 50。
	trades := []entity.BacktestTradeRecord{
		// window1 のみ: ExitTime < 1 day。3 件全勝。
		makeTrade(0*dayMillis+1, 10),
		makeTrade(0*dayMillis+2, 10),
		makeTrade(0*dayMillis+3, 10),
		// 両ウィンドウに入る境界上でない 1 件。
		// 14*day は window1 の end と一致 → window1 に入らず、window2 ([1d,15d)) に入る。
		makeTrade(14*dayMillis, 10),
	}
	// window1 のトレード (day0 + 1ms..3ms) は window2 ([1d,15d)) には含まれない。
	// window2 にはさらに 14d のトレード 1 件だけ → ペナルティ。
	got := ComputeBiweeklyWinRate(trades, 0, 15*dayMillis)
	if math.Abs(got-50) > 1e-9 {
		t.Fatalf("expected 50 (avg of 100 and penalty 0), got %v", got)
	}
}

func TestComputeBiweeklyWinRate_OffByOneWindowBoundary(t *testing.T) {
	// ウィンドウは半開区間 [start, end)。
	// 期間 15 日 → ウィンドウ 2 つ: window1=[0,14d), window2=[1d,15d)。
	// 境界ちょうど 14d のトレードは window1 に含まれず window2 のみに含まれる。
	// window1 (3 件全勝) / window2 (3 件, うち 14d 境界 1 件が負け → 2 勝 1 敗 = 66.67)
	trades := []entity.BacktestTradeRecord{
		// window1 のみ (ExitTime in [0, 14d))
		makeTrade(2*dayMillis, 10),
		makeTrade(5*dayMillis, 10),
		makeTrade(12*dayMillis, 10),
		// window2 にだけ入るトレード (ExitTime in [1d, 15d) だが [0,14d) にも入りうる)
		makeTrade(13*dayMillis+dayMillis/2, 10), // window1 と window2 両方に入る (12.5d, 13.5d で [0,14d) と [1d,15d) に含まれる)
		// 境界ちょうど: 14d は window1 に NOT 含まれ、window2 に含まれる (負け)
		makeTrade(14*dayMillis, -5),
	}
	// window1 ([0, 14d)) のトレード: day2, day5, day12, day13.5 → 4 件全勝 → rate 100
	// window2 ([1d, 15d)) のトレード: day2, day5, day12, day13.5, day14 → 5 件中 4 勝 1 敗 → 80
	// カバレッジ = 2/2 = 1.0 → 平均 = (100 + 80)/2 = 90
	got := ComputeBiweeklyWinRate(trades, 0, 15*dayMillis)
	if math.Abs(got-90) > 1e-9 {
		t.Fatalf("expected 90, got %v", got)
	}
}
