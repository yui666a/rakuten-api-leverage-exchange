package backtest

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestParseSignalSource(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   string
	}{
		{
			name:   "trend follow buy variant 1",
			reason: "trend follow: EMA12 > EMA26, SMA aligned, RSI not overbought, MACD confirmed",
			want:   "trend_follow",
		},
		{
			name:   "trend follow sell variant",
			reason: "trend follow: SMA20 < SMA50, RSI not oversold",
			want:   "trend_follow",
		},
		{
			name:   "contrarian buy",
			reason: "contrarian: RSI oversold, expecting bounce",
			want:   "contrarian",
		},
		{
			name:   "contrarian sell with macd nuance",
			reason: "contrarian: RSI overbought, expecting pullback, MACD not strongly against",
			want:   "contrarian",
		},
		{
			name:   "breakout buy",
			reason: "breakout: price above BB upper with volume confirmation",
			want:   "breakout",
		},
		{
			name:   "breakout sell",
			reason: "breakout: price below BB lower with volume confirmation",
			want:   "breakout",
		},
		{
			name:   "case insensitive prefix",
			reason: "Trend Follow: something",
			want:   "trend_follow",
		},
		{
			name:   "empty reason",
			reason: "",
			want:   "unknown",
		},
		{
			name:   "unknown prefix",
			reason: "manual: operator override",
			want:   "unknown",
		},
		{
			name:   "leading whitespace is tolerated",
			reason: "  trend follow: EMA12 > EMA26",
			want:   "trend_follow",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSignalSource(tc.reason)
			if got != tc.want {
				t.Fatalf("parseSignalSource(%q) = %q, want %q", tc.reason, got, tc.want)
			}
		})
	}
}

func TestComputeBreakdown_MixedWinLoss(t *testing.T) {
	// 3 wins, 2 losses
	trades := []entity.BacktestTradeRecord{
		{PnL: 100},
		{PnL: 50},
		{PnL: -40},
		{PnL: 30},
		{PnL: -60},
	}
	got := computeBreakdown(trades)

	if got.Trades != 5 {
		t.Fatalf("Trades = %d, want 5", got.Trades)
	}
	if got.WinTrades != 3 {
		t.Fatalf("WinTrades = %d, want 3", got.WinTrades)
	}
	if got.LossTrades != 2 {
		t.Fatalf("LossTrades = %d, want 2", got.LossTrades)
	}
	if got.WinRate != 60.0 {
		t.Fatalf("WinRate = %v, want 60.0", got.WinRate)
	}
	if got.TotalPnL != 80.0 { // 100+50-40+30-60
		t.Fatalf("TotalPnL = %v, want 80.0", got.TotalPnL)
	}
	if got.AvgPnL != 16.0 { // 80/5
		t.Fatalf("AvgPnL = %v, want 16.0", got.AvgPnL)
	}
	// PF = (100+50+30) / (40+60) = 180/100 = 1.8
	if math.Abs(got.ProfitFactor-1.8) > 1e-9 {
		t.Fatalf("ProfitFactor = %v, want 1.8", got.ProfitFactor)
	}
}

func TestComputeBreakdown_ZeroPnLCountsAsWin(t *testing.T) {
	// 既存 reporter.go の規約: PnL >= 0 は win に数える
	trades := []entity.BacktestTradeRecord{
		{PnL: 0},
		{PnL: -10},
	}
	got := computeBreakdown(trades)
	if got.WinTrades != 1 || got.LossTrades != 1 {
		t.Fatalf("wins/losses = %d/%d, want 1/1", got.WinTrades, got.LossTrades)
	}
}

func TestComputeBreakdown_EmptyTrades(t *testing.T) {
	got := computeBreakdown(nil)
	if got.Trades != 0 {
		t.Fatalf("Trades = %d, want 0", got.Trades)
	}
	if got.WinRate != 0 || got.TotalPnL != 0 || got.AvgPnL != 0 || got.ProfitFactor != 0 {
		t.Fatalf("non-zero fields on empty input: %+v", got)
	}
}

func TestComputeBreakdown_AllWinsNoProfitFactor(t *testing.T) {
	// no losses -> ProfitFactor stays 0 (matches reporter.go convention)
	trades := []entity.BacktestTradeRecord{{PnL: 10}, {PnL: 5}}
	got := computeBreakdown(trades)
	if got.ProfitFactor != 0 {
		t.Fatalf("ProfitFactor = %v, want 0 (no losses)", got.ProfitFactor)
	}
	if got.WinRate != 100 {
		t.Fatalf("WinRate = %v, want 100", got.WinRate)
	}
}

func TestComputeBreakdown_AllLosses(t *testing.T) {
	trades := []entity.BacktestTradeRecord{{PnL: -10}, {PnL: -5}}
	got := computeBreakdown(trades)
	if got.WinTrades != 0 || got.LossTrades != 2 {
		t.Fatalf("wins/losses = %d/%d, want 0/2", got.WinTrades, got.LossTrades)
	}
	if got.WinRate != 0 {
		t.Fatalf("WinRate = %v, want 0", got.WinRate)
	}
	if got.ProfitFactor != 0 {
		t.Fatalf("ProfitFactor = %v, want 0 (no wins)", got.ProfitFactor)
	}
}

func TestBuildBreakdown_ByExitReason(t *testing.T) {
	trades := []entity.BacktestTradeRecord{
		{PnL: 10, ReasonExit: "take_profit"},
		{PnL: -5, ReasonExit: "stop_loss"},
		{PnL: 3, ReasonExit: "take_profit"},
		{PnL: -2, ReasonExit: "reverse_signal"},
	}
	got := BuildBreakdown(trades, func(t entity.BacktestTradeRecord) string { return t.ReasonExit })

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3; got keys: %v", len(got), got)
	}
	if got["take_profit"].Trades != 2 || got["take_profit"].TotalPnL != 13 {
		t.Fatalf("take_profit bucket = %+v", got["take_profit"])
	}
	if got["stop_loss"].Trades != 1 || got["stop_loss"].TotalPnL != -5 {
		t.Fatalf("stop_loss bucket = %+v", got["stop_loss"])
	}
	if got["reverse_signal"].Trades != 1 || got["reverse_signal"].TotalPnL != -2 {
		t.Fatalf("reverse_signal bucket = %+v", got["reverse_signal"])
	}

	// 合計 Trades = 元配列の長さ
	sumTrades := 0
	for _, b := range got {
		sumTrades += b.Trades
	}
	if sumTrades != len(trades) {
		t.Fatalf("sum Trades = %d, want %d", sumTrades, len(trades))
	}
}

func TestBuildBreakdown_BySignalSource(t *testing.T) {
	trades := []entity.BacktestTradeRecord{
		{PnL: 10, ReasonEntry: "trend follow: EMA12 > EMA26"},
		{PnL: -5, ReasonEntry: "contrarian: RSI oversold"},
		{PnL: 3, ReasonEntry: "breakout: price above BB upper"},
		{PnL: 7, ReasonEntry: "trend follow: SMA20 > SMA50"},
		{PnL: -1, ReasonEntry: ""},
	}
	got := BuildBreakdown(trades, func(t entity.BacktestTradeRecord) string {
		return parseSignalSource(t.ReasonEntry)
	})

	if got["trend_follow"].Trades != 2 || got["trend_follow"].TotalPnL != 17 {
		t.Fatalf("trend_follow bucket = %+v", got["trend_follow"])
	}
	if got["contrarian"].Trades != 1 {
		t.Fatalf("contrarian bucket = %+v", got["contrarian"])
	}
	if got["breakout"].Trades != 1 {
		t.Fatalf("breakout bucket = %+v", got["breakout"])
	}
	if got["unknown"].Trades != 1 {
		t.Fatalf("unknown bucket = %+v", got["unknown"])
	}
}

func TestBuildBreakdown_EmptyInput(t *testing.T) {
	got := BuildBreakdown(nil, func(t entity.BacktestTradeRecord) string { return t.ReasonExit })
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}
