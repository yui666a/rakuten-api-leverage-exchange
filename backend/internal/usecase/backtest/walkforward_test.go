package backtest

import (
	"math"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// -------------- window split --------------

func TestComputeWindows_Standard(t *testing.T) {
	// (36 months, IS=6, OOS=3, step=3) -> 10 windows
	from := time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	ws, err := ComputeWindows(from, to, 6, 3, 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ws) != 10 {
		t.Fatalf("windows = %d, want 10", len(ws))
	}
	// First window: IS=[0..6), OOS=[6..9)
	if !ws[0].InSampleFrom.Equal(from) {
		t.Fatalf("ws[0].InSampleFrom = %v, want %v", ws[0].InSampleFrom, from)
	}
	wantISEnd := time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC)
	if !ws[0].InSampleTo.Equal(wantISEnd) {
		t.Fatalf("ws[0].InSampleTo = %v, want %v", ws[0].InSampleTo, wantISEnd)
	}
	// Sliding: ws[1].InSampleFrom = ws[0].InSampleFrom + step
	wantW1IS := time.Date(2023, 7, 1, 0, 0, 0, 0, time.UTC)
	if !ws[1].InSampleFrom.Equal(wantW1IS) {
		t.Fatalf("ws[1].InSampleFrom = %v, want %v", ws[1].InSampleFrom, wantW1IS)
	}
	// OOS doesn't overlap IS: OOSFrom == InSampleTo
	if !ws[0].OOSFrom.Equal(ws[0].InSampleTo) {
		t.Fatalf("ws[0] OOSFrom must equal InSampleTo (adjacent, non-overlap)")
	}
	// Last window fits inside [from, to]
	last := ws[len(ws)-1]
	if last.OOSTo.After(to) {
		t.Fatalf("last window OOSTo %v exceeds to %v", last.OOSTo, to)
	}
}

func TestComputeWindows_TooShortReturnsError(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC) // 2 months
	if _, err := ComputeWindows(from, to, 6, 3, 3); err == nil {
		t.Fatalf("expected error for too-short span")
	}
}

func TestComputeWindows_InvalidInputs(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name          string
		in, oos, step int
	}{
		{"zero in", 0, 3, 3},
		{"negative oos", 3, -1, 3},
		{"zero step", 3, 3, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ComputeWindows(from, to, c.in, c.oos, c.step); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
		})
	}
}

// -------------- grid expansion --------------

func TestExpandGrid_CartesianProduct(t *testing.T) {
	got, err := ExpandGrid([]ParameterOverride{
		{Path: "strategy_risk.stop_loss_percent", Values: []float64{3, 5, 6}},
		{Path: "signal_rules.trend_follow.rsi_buy_max", Values: []float64{60, 65}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("combinations = %d, want 6", len(got))
	}
	seen := map[[2]float64]bool{}
	for _, combo := range got {
		key := [2]float64{combo["strategy_risk.stop_loss_percent"], combo["signal_rules.trend_follow.rsi_buy_max"]}
		if seen[key] {
			t.Fatalf("duplicate combo: %+v", combo)
		}
		seen[key] = true
	}
}

func TestExpandGrid_SingleParam(t *testing.T) {
	got, err := ExpandGrid([]ParameterOverride{
		{Path: "a", Values: []float64{1, 2, 3}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 combos, got %d", len(got))
	}
}

func TestExpandGrid_Empty(t *testing.T) {
	// No overrides => one empty combo (so "baseline with no tweaks" still runs).
	got, err := ExpandGrid(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("want [{}] for empty input, got %v", got)
	}
}

func TestExpandGrid_RejectsExplosion(t *testing.T) {
	// Four 10-value axes = 10,000 combos; should refuse over MAX_GRID_SIZE.
	over := []ParameterOverride{}
	for i := 0; i < 4; i++ {
		v := make([]float64, 10)
		for j := range v {
			v[j] = float64(j)
		}
		over = append(over, ParameterOverride{Path: "p", Values: v})
	}
	if _, err := ExpandGrid(over); err == nil {
		t.Fatalf("expected error on combinatorial explosion")
	}
}

func TestExpandGrid_RejectsEmptyValues(t *testing.T) {
	_, err := ExpandGrid([]ParameterOverride{{Path: "a", Values: nil}})
	if err == nil {
		t.Fatalf("expected error on empty values list")
	}
}

// -------------- apply overrides --------------

func TestApplyOverrides_RiskField(t *testing.T) {
	base := entity.StrategyProfile{
		Name: "base",
		Risk: entity.StrategyRiskConfig{StopLossPercent: 5, TakeProfitPercent: 10},
	}
	got, err := ApplyOverrides(base, map[string]float64{
		"strategy_risk.stop_loss_percent": 3,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Risk.StopLossPercent != 3 {
		t.Fatalf("StopLossPercent = %v, want 3", got.Risk.StopLossPercent)
	}
	if got.Risk.TakeProfitPercent != 10 {
		t.Fatalf("TakeProfitPercent = %v, want unchanged 10", got.Risk.TakeProfitPercent)
	}
	// Original must not be mutated.
	if base.Risk.StopLossPercent != 5 {
		t.Fatalf("base.Risk.StopLossPercent mutated: %v", base.Risk.StopLossPercent)
	}
}

func TestApplyOverrides_SignalField(t *testing.T) {
	base := entity.StrategyProfile{
		SignalRules: entity.SignalRulesConfig{
			TrendFollow: entity.TrendFollowConfig{RSIBuyMax: 70},
			Contrarian:  entity.ContrarianConfig{ADXMax: 0},
			Breakout:    entity.BreakoutConfig{ADXMin: 0},
		},
	}
	got, err := ApplyOverrides(base, map[string]float64{
		"signal_rules.trend_follow.rsi_buy_max": 65,
		"signal_rules.contrarian.adx_max":       20,
		"signal_rules.breakout.adx_min":         25,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.SignalRules.TrendFollow.RSIBuyMax != 65 {
		t.Fatalf("RSIBuyMax = %v", got.SignalRules.TrendFollow.RSIBuyMax)
	}
	if got.SignalRules.Contrarian.ADXMax != 20 {
		t.Fatalf("ADXMax = %v", got.SignalRules.Contrarian.ADXMax)
	}
	if got.SignalRules.Breakout.ADXMin != 25 {
		t.Fatalf("ADXMin = %v", got.SignalRules.Breakout.ADXMin)
	}
}

func TestApplyOverrides_UnknownPathReturnsError(t *testing.T) {
	base := entity.StrategyProfile{}
	_, err := ApplyOverrides(base, map[string]float64{"not.a.real.path": 5})
	if err == nil {
		t.Fatalf("expected error on unknown path")
	}
}

// -------------- objective --------------

func TestSelectByObjective(t *testing.T) {
	s := entity.BacktestSummary{
		TotalReturn:  0.1,
		SharpeRatio:  1.5,
		ProfitFactor: 1.2,
	}
	if v := SelectByObjective(s, "return"); v != 0.1 {
		t.Fatalf("return objective = %v, want 0.1", v)
	}
	if v := SelectByObjective(s, "sharpe"); v != 1.5 {
		t.Fatalf("sharpe objective = %v, want 1.5", v)
	}
	if v := SelectByObjective(s, "profit_factor"); v != 1.2 {
		t.Fatalf("profit_factor objective = %v, want 1.2", v)
	}
	// Unknown defaults to Return so a caller typo doesn't silently swap
	// the scoring axis.
	if v := SelectByObjective(s, "unknown_name"); v != 0.1 {
		t.Fatalf("unknown objective should fall back to return, got %v", v)
	}
}

func TestSelectByObjective_NaNStaysNaN(t *testing.T) {
	s := entity.BacktestSummary{TotalReturn: math.NaN()}
	if v := SelectByObjective(s, "return"); !math.IsNaN(v) {
		t.Fatalf("NaN round-trip expected, got %v", v)
	}
}
