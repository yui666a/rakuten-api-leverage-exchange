package positionsize

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestSizer_FixedModeReturnsRequested(t *testing.T) {
	t.Parallel()
	s := New(nil, fineDefaults())
	got := s.Compute(Input{
		RequestedAmount: 0.1,
		EntryPrice:      12000,
		StopLossPercent: 14,
		Equity:          100000,
	})
	if !almostEqual(got.Amount, 0.1, 1e-9) {
		t.Fatalf("fixed mode should return requested amount, got %v", got.Amount)
	}
	if got.Mode != "fixed" {
		t.Fatalf("mode = %q, want fixed", got.Mode)
	}
}

func TestSizer_FixedModeByExplicitMode(t *testing.T) {
	t.Parallel()
	cfg := &entity.PositionSizingConfig{Mode: "fixed"}
	s := New(cfg, fineDefaults())
	got := s.Compute(Input{
		RequestedAmount: 0.2,
		EntryPrice:      12000,
		StopLossPercent: 14,
		Equity:          100000,
	})
	if !almostEqual(got.Amount, 0.2, 1e-9) {
		t.Fatalf("fixed mode should pass through, got %v", got.Amount)
	}
}

// fineDefaults disables lot-step rounding so formulas can be tested directly.
func fineDefaults() Defaults {
	d := DefaultDefaults()
	d.LotStep = 0
	d.MinLot = 0
	return d
}

func TestSizer_RiskPctBaseFormula(t *testing.T) {
	t.Parallel()
	// equity=100,000 JPY, risk 1% = 1,000 JPY
	// SL distance = 12,000 * 14% = 1,680 JPY per LTC
	// lot = 1,000 / 1,680 ≈ 0.5952 LTC
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
	}
	s := New(cfg, fineDefaults())
	got := s.Compute(Input{
		RequestedAmount: 0.1, // ignored in risk_pct mode
		EntryPrice:      12000,
		StopLossPercent: 14,
		Equity:          100000,
	})
	want := 0.5952
	if !almostEqual(got.Amount, want, 0.001) {
		t.Fatalf("amount = %v, want ~%v", got.Amount, want)
	}
	if got.Mode != "risk_pct" {
		t.Fatalf("mode = %q, want risk_pct", got.Mode)
	}
}

func TestSizer_RiskPctScalesWithEquity(t *testing.T) {
	t.Parallel()
	cfg := &entity.PositionSizingConfig{Mode: "risk_pct", RiskPerTradePct: 1.0}
	s := New(cfg, fineDefaults())
	small := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000})
	big := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 1000000})
	ratio := big.Amount / small.Amount
	if !almostEqual(ratio, 10.0, 0.05) {
		t.Fatalf("equity 10x should scale amount 10x, got ratio %v (small=%v big=%v)", ratio, small.Amount, big.Amount)
	}
}

func TestSizer_MaxPositionPctCap(t *testing.T) {
	t.Parallel()
	// equity=100,000 JPY, cap = 20% = 20,000 JPY notional
	// at entry 12,000: max lot = 20000/12000 = 1.6667
	cfg := &entity.PositionSizingConfig{
		Mode:                   "risk_pct",
		RiskPerTradePct:        5.0, // would want 0.5952 * 5 ≈ 2.976 without cap
		MaxPositionPctOfEquity: 20.0,
	}
	s := New(cfg, fineDefaults())
	got := s.Compute(Input{
		RequestedAmount: 0.1,
		EntryPrice:      12000,
		StopLossPercent: 14,
		Equity:          100000,
	})
	want := 20000.0 / 12000.0
	if got.Amount > want+0.001 {
		t.Fatalf("amount %v exceeded max_position cap %v", got.Amount, want)
	}
}

func TestSizer_ATRAdjustScalesDown(t *testing.T) {
	t.Parallel()
	// current atr pct = 4% vs target 2% → scale = 0.5
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
		ATRAdjust:       true,
		TargetATRPct:    2.0,
		ATRScaleMin:     0.5,
		ATRScaleMax:     2.0,
	}
	s := New(cfg, fineDefaults())
	baseline := s.Compute(Input{
		RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000,
	})
	scaled := s.Compute(Input{
		RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000,
		CurrentATR: 480, // 4% of 12000
	})
	if !almostEqual(scaled.Amount, baseline.Amount*0.5, 0.01) {
		t.Fatalf("ATR 4%% (2x target) should scale by 0.5; baseline=%v scaled=%v", baseline.Amount, scaled.Amount)
	}
}

func TestSizer_ATRAdjustClampsToMin(t *testing.T) {
	t.Parallel()
	// atr pct = 8% vs target 2% → raw scale 0.25, but clamp min 0.5
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
		ATRAdjust:       true,
		TargetATRPct:    2.0,
		ATRScaleMin:     0.5,
		ATRScaleMax:     2.0,
	}
	s := New(cfg, fineDefaults())
	baseline := s.Compute(Input{
		RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000,
	})
	clamped := s.Compute(Input{
		RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000,
		CurrentATR: 960,
	})
	if !almostEqual(clamped.Amount, baseline.Amount*0.5, 0.01) {
		t.Fatalf("ATR scale should be clamped at min 0.5; baseline=%v clamped=%v", baseline.Amount, clamped.Amount)
	}
}

func TestSizer_DrawdownTierAHalves(t *testing.T) {
	t.Parallel()
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
		DDScaleDown: entity.DrawdownScaleConfig{
			TierAPct: 10, TierAScale: 0.5,
			TierBPct: 15, TierBScale: 0.25,
		},
	}
	s := New(cfg, fineDefaults())
	base := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000})
	dd := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000, CurrentDrawdownPct: 12})
	if !almostEqual(dd.Amount, base.Amount*0.5, 0.01) {
		t.Fatalf("DD=12%% should halve amount; base=%v dd=%v", base.Amount, dd.Amount)
	}
}

func TestSizer_DrawdownTierBQuarters(t *testing.T) {
	t.Parallel()
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
		DDScaleDown: entity.DrawdownScaleConfig{
			TierAPct: 10, TierAScale: 0.5,
			TierBPct: 15, TierBScale: 0.25,
		},
	}
	s := New(cfg, fineDefaults())
	base := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000})
	dd := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000, CurrentDrawdownPct: 16})
	if !almostEqual(dd.Amount, base.Amount*0.25, 0.01) {
		t.Fatalf("DD=16%% should scale by 0.25; base=%v dd=%v", base.Amount, dd.Amount)
	}
}

func TestSizer_LotStepRoundsDown(t *testing.T) {
	t.Parallel()
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
		LotStep:         0.01,
		MinLot:          0.01,
	}
	s := New(cfg, fineDefaults())
	got := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000})
	// 0.5952 -> floor(/0.01)*0.01 = 0.59
	if !almostEqual(got.Amount, 0.59, 1e-9) {
		t.Fatalf("lot_step=0.01 should floor 0.5952 -> 0.59, got %v", got.Amount)
	}
}

func TestSizer_BelowMinLotReturnsZero(t *testing.T) {
	t.Parallel()
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 0.01, // tiny
		MinLot:          0.5,
		LotStep:         0.01,
	}
	s := New(cfg, fineDefaults())
	got := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000})
	if got.Amount != 0 {
		t.Fatalf("tiny risk budget below min_lot should reject with 0, got %v", got.Amount)
	}
	if got.SkipReason == "" {
		t.Fatalf("expected skip reason to be set")
	}
}

func TestSizer_NilConfigDefaultsToFixed(t *testing.T) {
	t.Parallel()
	s := New(nil, fineDefaults())
	got := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000})
	if got.Mode != "fixed" || !almostEqual(got.Amount, 0.1, 1e-9) {
		t.Fatalf("nil config should behave as fixed pass-through, got mode=%q amount=%v", got.Mode, got.Amount)
	}
}

func TestSizer_ConfidenceScales(t *testing.T) {
	t.Parallel()
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
	}
	s := New(cfg, fineDefaults())
	weak := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000, Confidence: 0.6, MinConfidence: 0.6})
	strong := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 100000, Confidence: 1.0, MinConfidence: 0.6})
	if strong.Amount <= weak.Amount {
		t.Fatalf("higher confidence should produce larger lot; weak=%v strong=%v", weak.Amount, strong.Amount)
	}
}

func TestSizer_ZeroEquityReturnsZero(t *testing.T) {
	t.Parallel()
	cfg := &entity.PositionSizingConfig{Mode: "risk_pct", RiskPerTradePct: 1.0}
	s := New(cfg, fineDefaults())
	got := s.Compute(Input{RequestedAmount: 0.1, EntryPrice: 12000, StopLossPercent: 14, Equity: 0})
	if got.Amount != 0 {
		t.Fatalf("zero equity should produce zero lot, got %v", got.Amount)
	}
}
