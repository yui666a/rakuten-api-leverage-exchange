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

// TestComputeWindows_MonthEndDoesNotDrift is the regression lock for the
// Codex review finding on PR #115: stdlib time.AddDate would roll
// 2024-01-31 + 1mo into 2024-03-02, silently skipping February and
// drifting later windows later and later. After the clamping fix, the
// window set must still be anchored on the last day of each month.
func TestComputeWindows_MonthEndDoesNotDrift(t *testing.T) {
	// 2024-01-31 start, 1-month IS, 1-month OOS, 1-month step.
	// The first window ends at IS(2024-02-29) because 2024 is a leap
	// year; OOS then ends at 2024-03-31.
	from := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)
	ws, err := ComputeWindows(from, to, 1, 1, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ws) < 2 {
		t.Fatalf("want at least 2 windows, got %d", len(ws))
	}
	// w0: [2024-01-31 .. 2024-02-29] IS, [2024-02-29 .. 2024-03-31] OOS
	wantISEnd0 := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	wantOOSEnd0 := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)
	if !ws[0].InSampleTo.Equal(wantISEnd0) {
		t.Fatalf("w0.InSampleTo = %v, want %v (leap-year clamp)", ws[0].InSampleTo, wantISEnd0)
	}
	if !ws[0].OOSTo.Equal(wantOOSEnd0) {
		t.Fatalf("w0.OOSTo = %v, want %v", ws[0].OOSTo, wantOOSEnd0)
	}
	// w1 must start at 2024-02-29 (clamped) — with AddDate this would have
	// been 2024-03-02 and the test would fail.
	wantW1Start := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	if !ws[1].InSampleFrom.Equal(wantW1Start) {
		t.Fatalf("w1.InSampleFrom = %v, want %v", ws[1].InSampleFrom, wantW1Start)
	}
}

func TestComputeWindows_NonLeapYearClampsFebTo28(t *testing.T) {
	from := time.Date(2023, 1, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2023, 6, 30, 0, 0, 0, 0, time.UTC)
	ws, err := ComputeWindows(from, to, 1, 1, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantISEnd0 := time.Date(2023, 2, 28, 0, 0, 0, 0, time.UTC)
	if !ws[0].InSampleTo.Equal(wantISEnd0) {
		t.Fatalf("non-leap year clamp: InSampleTo = %v, want %v", ws[0].InSampleTo, wantISEnd0)
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

// TestExpandGrid_RejectsDuplicatePaths locks in the Codex PR-13 follow-up:
// if two overrides share the same Path, the later one silently overwrites
// the earlier one in every combo, so the caller's "N×M" grid actually
// produces only M distinct combos and its scoring signal is compromised.
func TestExpandGrid_RejectsDuplicatePaths(t *testing.T) {
	_, err := ExpandGrid([]ParameterOverride{
		{Path: "a", Values: []float64{1, 2}},
		{Path: "a", Values: []float64{3, 4}},
	})
	if err == nil {
		t.Fatalf("expected error on duplicate path")
	}
}

func TestExpandGrid_RejectsEmptyPath(t *testing.T) {
	_, err := ExpandGrid([]ParameterOverride{{Path: "", Values: []float64{1}}})
	if err == nil {
		t.Fatalf("expected error on empty path")
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

func TestApplyOverrides_RiskMaxFields(t *testing.T) {
	// Added after the Codex #117 re-review: the handler wires
	// MaxPositionAmount / MaxDailyLoss through RiskConfig but ApplyOverrides
	// used to silently reject those paths with an unknown-path error, so
	// a grid entry like `{"path":"strategy_risk.max_daily_loss"}` failed
	// at 400 instead of overriding the field.
	base := entity.StrategyProfile{
		Risk: entity.StrategyRiskConfig{
			MaxPositionAmount: 100000,
			MaxDailyLoss:      50000,
		},
	}
	got, err := ApplyOverrides(base, map[string]float64{
		"strategy_risk.max_position_amount": 200000,
		"strategy_risk.max_daily_loss":      25000,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Risk.MaxPositionAmount != 200000 {
		t.Fatalf("MaxPositionAmount = %v", got.Risk.MaxPositionAmount)
	}
	if got.Risk.MaxDailyLoss != 25000 {
		t.Fatalf("MaxDailyLoss = %v", got.Risk.MaxDailyLoss)
	}
}

func TestApplyOverrides_UnknownPathReturnsError(t *testing.T) {
	base := entity.StrategyProfile{}
	_, err := ApplyOverrides(base, map[string]float64{"not.a.real.path": 5})
	if err == nil {
		t.Fatalf("expected error on unknown path")
	}
}

// TestApplyOverrides_RegimeDetectorConfig is the PR-5 part F wiring
// guard: a router profile's detector_config must be reachable from a
// WFO grid so cycle40+ can sweep TrendADXMin / VolatileATRPercentMin /
// HysteresisBars across real candle data. Without this path, cycle39's
// "detector never emits anything but bull-trend on LTC 15m" would
// stay permanently blocked on a hand-edited profile loop.
func TestApplyOverrides_RegimeDetectorConfig(t *testing.T) {
	t.Run("creates nested config on a flat profile", func(t *testing.T) {
		base := entity.StrategyProfile{Name: "flat"}
		got, err := ApplyOverrides(base, map[string]float64{
			"regime_routing.detector_config.trend_adx_min":            25,
			"regime_routing.detector_config.volatile_atr_percent_min": 3.5,
		})
		if err != nil {
			t.Fatalf("ApplyOverrides: %v", err)
		}
		if got.RegimeRouting == nil || got.RegimeRouting.DetectorConfig == nil {
			t.Fatalf("nested config not allocated: %+v", got.RegimeRouting)
		}
		if got.RegimeRouting.DetectorConfig.TrendADXMin != 25 {
			t.Errorf("TrendADXMin = %v, want 25", got.RegimeRouting.DetectorConfig.TrendADXMin)
		}
		if got.RegimeRouting.DetectorConfig.VolatileATRPercentMin != 3.5 {
			t.Errorf("VolatileATRPercentMin = %v, want 3.5", got.RegimeRouting.DetectorConfig.VolatileATRPercentMin)
		}
	})

	t.Run("preserves existing default + overrides on a router base", func(t *testing.T) {
		base := entity.StrategyProfile{
			Name: "router",
			RegimeRouting: &entity.RegimeRoutingConfig{
				Default:   "child_default",
				Overrides: map[string]string{"bear-trend": "child_bear"},
			},
		}
		got, err := ApplyOverrides(base, map[string]float64{
			"regime_routing.detector_config.trend_adx_min": 15,
		})
		if err != nil {
			t.Fatalf("ApplyOverrides: %v", err)
		}
		if got.RegimeRouting.Default != "child_default" {
			t.Errorf("default lost: %q", got.RegimeRouting.Default)
		}
		if got.RegimeRouting.Overrides["bear-trend"] != "child_bear" {
			t.Errorf("overrides lost: %+v", got.RegimeRouting.Overrides)
		}
		if got.RegimeRouting.DetectorConfig.TrendADXMin != 15 {
			t.Errorf("TrendADXMin override lost: %v", got.RegimeRouting.DetectorConfig.TrendADXMin)
		}
	})

	t.Run("hysteresis_bars accepts integer-valued floats", func(t *testing.T) {
		base := entity.StrategyProfile{}
		got, err := ApplyOverrides(base, map[string]float64{
			"regime_routing.detector_config.hysteresis_bars": 5,
		})
		if err != nil {
			t.Fatalf("ApplyOverrides: %v", err)
		}
		if got.RegimeRouting.DetectorConfig.HysteresisBars != 5 {
			t.Errorf("HysteresisBars = %d, want 5", got.RegimeRouting.DetectorConfig.HysteresisBars)
		}
	})

	t.Run("hysteresis_bars rejects fractional grid values", func(t *testing.T) {
		base := entity.StrategyProfile{}
		_, err := ApplyOverrides(base, map[string]float64{
			"regime_routing.detector_config.hysteresis_bars": 2.5,
		})
		if err == nil {
			t.Fatal("expected error on fractional hysteresis_bars (silent truncation would corrupt the grid signal)")
		}
	})
}

// TestApplyOverrides_BreakoutDonchianPeriod is the PR-11 wiring guard:
// grid axes sweeping Donchian lookback (20 / 40 / 60 etc.) must land on
// SignalRules.Breakout.DonchianPeriod as an int, and fractional values
// must be rejected so an off-by-0.5 in a grid spec fails loudly.
func TestApplyOverrides_BreakoutDonchianPeriod(t *testing.T) {
	t.Run("integer value sets the field", func(t *testing.T) {
		base := entity.StrategyProfile{}
		got, err := ApplyOverrides(base, map[string]float64{
			"signal_rules.breakout.donchian_period": 20,
		})
		if err != nil {
			t.Fatalf("ApplyOverrides: %v", err)
		}
		if got.SignalRules.Breakout.DonchianPeriod != 20 {
			t.Errorf("DonchianPeriod = %d, want 20", got.SignalRules.Breakout.DonchianPeriod)
		}
	})

	t.Run("zero disables the gate", func(t *testing.T) {
		base := entity.StrategyProfile{
			SignalRules: entity.SignalRulesConfig{
				Breakout: entity.BreakoutConfig{DonchianPeriod: 20},
			},
		}
		got, err := ApplyOverrides(base, map[string]float64{
			"signal_rules.breakout.donchian_period": 0,
		})
		if err != nil {
			t.Fatalf("ApplyOverrides: %v", err)
		}
		if got.SignalRules.Breakout.DonchianPeriod != 0 {
			t.Errorf("DonchianPeriod = %d, want 0 (gate disabled)", got.SignalRules.Breakout.DonchianPeriod)
		}
	})

	t.Run("fractional value is rejected", func(t *testing.T) {
		base := entity.StrategyProfile{}
		_, err := ApplyOverrides(base, map[string]float64{
			"signal_rules.breakout.donchian_period": 20.5,
		})
		if err == nil {
			t.Fatal("expected error on fractional donchian_period (silent truncation would corrupt the grid signal)")
		}
	})

	t.Run("negative value is rejected", func(t *testing.T) {
		base := entity.StrategyProfile{}
		_, err := ApplyOverrides(base, map[string]float64{
			"signal_rules.breakout.donchian_period": -1,
		})
		if err == nil {
			t.Fatal("expected error on negative donchian_period")
		}
	})
}

// -------------- string overrides / combined grid --------------

func TestApplyStringOverrides_HTFMode(t *testing.T) {
	base := entity.StrategyProfile{HTFFilter: entity.HTFFilterConfig{Mode: "ema"}}
	got, err := ApplyStringOverrides(base, map[string]string{"htf_filter.mode": "ichimoku"})
	if err != nil {
		t.Fatalf("ApplyStringOverrides: %v", err)
	}
	if got.HTFFilter.Mode != "ichimoku" {
		t.Fatalf("Mode = %q, want ichimoku", got.HTFFilter.Mode)
	}
	if base.HTFFilter.Mode != "ema" {
		t.Fatalf("base mutated: %q", base.HTFFilter.Mode)
	}
}

func TestApplyStringOverrides_AllowedValues(t *testing.T) {
	base := entity.StrategyProfile{}
	for _, v := range []string{"", "ema", "ichimoku"} {
		if _, err := ApplyStringOverrides(base, map[string]string{"htf_filter.mode": v}); err != nil {
			t.Fatalf("value %q unexpectedly rejected: %v", v, err)
		}
	}
}

func TestApplyStringOverrides_RejectsBadValue(t *testing.T) {
	base := entity.StrategyProfile{}
	_, err := ApplyStringOverrides(base, map[string]string{"htf_filter.mode": "bollinger"})
	if err == nil {
		t.Fatal("expected error on unknown mode value")
	}
}

func TestApplyStringOverrides_RejectsUnknownPath(t *testing.T) {
	base := entity.StrategyProfile{}
	_, err := ApplyStringOverrides(base, map[string]string{"strategy_risk.label": "foo"})
	if err == nil {
		t.Fatal("expected error on unknown string override path")
	}
}

func TestApplyCombination_NumericAndString(t *testing.T) {
	base := entity.StrategyProfile{
		Risk:      entity.StrategyRiskConfig{StopLossPercent: 5},
		HTFFilter: entity.HTFFilterConfig{Mode: "ema"},
	}
	got, err := ApplyCombination(base, GridCombination{
		Numeric: map[string]float64{"strategy_risk.stop_loss_percent": 14},
		String:  map[string]string{"htf_filter.mode": "ichimoku"},
	})
	if err != nil {
		t.Fatalf("ApplyCombination: %v", err)
	}
	if got.Risk.StopLossPercent != 14 {
		t.Fatalf("StopLossPercent = %v", got.Risk.StopLossPercent)
	}
	if got.HTFFilter.Mode != "ichimoku" {
		t.Fatalf("Mode = %q", got.HTFFilter.Mode)
	}
}

func TestExpandCombinedGrid_NumericOnly(t *testing.T) {
	got, err := ExpandCombinedGrid([]ParameterOverride{
		{Path: "strategy_risk.stop_loss_percent", Values: []float64{3, 5}},
	}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 combos, got %d", len(got))
	}
	for _, c := range got {
		if len(c.String) != 0 {
			t.Fatalf("numeric-only grid produced string entries: %+v", c.String)
		}
	}
}

func TestExpandCombinedGrid_StringOnly(t *testing.T) {
	got, err := ExpandCombinedGrid(nil, []ParameterStringOverride{
		{Path: "htf_filter.mode", Values: []string{"ema", "ichimoku"}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 combos, got %d", len(got))
	}
	seen := map[string]bool{}
	for _, c := range got {
		seen[c.String["htf_filter.mode"]] = true
	}
	if !seen["ema"] || !seen["ichimoku"] {
		t.Fatalf("missing string values: %+v", seen)
	}
}

func TestExpandCombinedGrid_MixedCartesianProduct(t *testing.T) {
	got, err := ExpandCombinedGrid(
		[]ParameterOverride{
			{Path: "strategy_risk.stop_loss_percent", Values: []float64{4, 14}},
		},
		[]ParameterStringOverride{
			{Path: "htf_filter.mode", Values: []string{"ema", "ichimoku"}},
		},
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 combos (2x2), got %d", len(got))
	}
	type key struct {
		sl   float64
		mode string
	}
	seen := map[key]bool{}
	for _, c := range got {
		k := key{c.Numeric["strategy_risk.stop_loss_percent"], c.String["htf_filter.mode"]}
		if seen[k] {
			t.Fatalf("duplicate combo: %+v", k)
		}
		seen[k] = true
	}
	for _, sl := range []float64{4, 14} {
		for _, m := range []string{"ema", "ichimoku"} {
			if !seen[key{sl, m}] {
				t.Fatalf("missing combo sl=%v mode=%s", sl, m)
			}
		}
	}
}

func TestExpandCombinedGrid_EmptyBothIsBaseline(t *testing.T) {
	got, err := ExpandCombinedGrid(nil, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 baseline combo, got %d", len(got))
	}
	if len(got[0].Numeric) != 0 || len(got[0].String) != 0 {
		t.Fatalf("baseline combo should be empty: %+v", got[0])
	}
}

// TestExpandCombinedGrid_RejectsDuplicateAcrossTypes locks in the same
// contract ExpandGrid enforces for duplicate paths: even if one axis is
// numeric and the other is string, sharing a Path would silently let
// ApplyStringOverrides overwrite ApplyOverrides for that field.
func TestExpandCombinedGrid_RejectsDuplicateAcrossTypes(t *testing.T) {
	_, err := ExpandCombinedGrid(
		[]ParameterOverride{{Path: "htf_filter.mode", Values: []float64{0, 1}}},
		[]ParameterStringOverride{{Path: "htf_filter.mode", Values: []string{"ema", "ichimoku"}}},
	)
	if err == nil {
		t.Fatal("expected error on duplicate path across numeric + string axes")
	}
}

func TestExpandCombinedGrid_RejectsEmptyStringValue(t *testing.T) {
	_, err := ExpandCombinedGrid(nil, []ParameterStringOverride{
		{Path: "htf_filter.mode", Values: []string{"ema", ""}},
	})
	if err == nil {
		t.Fatal("expected error on empty string value")
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
