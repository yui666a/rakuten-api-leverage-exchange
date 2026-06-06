package main

import (
	"context"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

// atrWiringExecutor is a minimal TickRiskExecutor stub for the wiring test:
// it serves one long position and records Close calls so the test can assert
// at which tick the trailing stop fired (which encodes the trailing distance,
// which in turn proves whether ATR reached the handler).
type atrWiringExecutor struct {
	positions []eventengine.Position
	closes    []string // reasons, in order
}

func (e *atrWiringExecutor) Positions() []eventengine.Position { return e.positions }

func (e *atrWiringExecutor) SelectSLTPExit(_ entity.OrderSide, _, _, _, _ float64) (float64, string, bool) {
	// Never hit SL/TP in this test so only the trailing branch can close.
	return 0, "", false
}

func (e *atrWiringExecutor) Close(_ int64, _ float64, reason string, _ int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error) {
	e.closes = append(e.closes, reason)
	return entity.OrderEvent{}, &entity.BacktestTradeRecord{}, nil
}

// TestNewEventDrivenPipeline_PropagatesRiskPolicy locks in the wiring:
// EventDrivenPipelineConfig.RiskPolicy must reach the pipeline's
// internal field so the snapshot taken inside runEventLoop carries the
// strategy-tuned values into the TickRiskHandler constructor. Before
// the RiskPolicy refactor the live pipeline took a bag of float64s and
// glued them onto the handler with SetATRMultipliers, which was easy
// to forget; the policy struct + constructor argument makes that
// failure mode a compile error.
func TestNewEventDrivenPipeline_PropagatesRiskPolicy(t *testing.T) {
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 14, ATRMultiplier: 1.5},
		TakeProfit: risk.TakeProfitSpec{Percent: 4},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
	}
	p := NewEventDrivenPipeline(
		EventDrivenPipelineConfig{SymbolID: 7, RiskPolicy: policy},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if p.riskPolicy != policy {
		t.Errorf("riskPolicy = %+v, want %+v", p.riskPolicy, policy)
	}
}

// TestSnapshotCarriesRiskPolicy ensures the snapshot copy used by
// runEventLoop preserves the policy bit-for-bit so the
// NewTickRiskHandlerWithPolicy call site sees the same struct the
// caller configured.
func TestSnapshotCarriesRiskPolicy(t *testing.T) {
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 14},
		TakeProfit: risk.TakeProfitSpec{Percent: 4},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
	}
	p := NewEventDrivenPipeline(
		EventDrivenPipelineConfig{SymbolID: 7, RiskPolicy: policy},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	snap := p.snapshot()
	if snap.riskPolicy != policy {
		t.Errorf("snap.riskPolicy = %+v, want %+v", snap.riskPolicy, policy)
	}
}

// TestPolicyView_MapsTrailingMode is the regression guard for the
// policyView translation between domain/risk and usecase/backtest.
// Mismatched constants would silently disable trailing in live (or
// turn percent-only profiles into ATR-only ones), which is exactly
// the class of bug the RiskPolicy refactor exists to prevent.
func TestPolicyView_MapsTrailingMode(t *testing.T) {
	tests := []struct {
		name string
		in   risk.RiskPolicy
		want backtest.PolicyView
	}{
		{
			name: "ATR trailing maps verbatim",
			in: risk.RiskPolicy{
				StopLoss:   risk.StopLossSpec{Percent: 14, ATRMultiplier: 1.5},
				TakeProfit: risk.TakeProfitSpec{Percent: 4},
				Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
			},
			want: backtest.PolicyView{
				StopLossPercent:       14,
				StopLossATRMultiplier: 1.5,
				TakeProfitPercent:     4,
				TrailingMode:          backtest.TrailingModeATR,
				TrailingATRMultiplier: 2.5,
			},
		},
		{
			name: "Percent trailing keeps legacy behaviour",
			in: risk.RiskPolicy{
				StopLoss:   risk.StopLossSpec{Percent: 14},
				TakeProfit: risk.TakeProfitSpec{Percent: 4},
				Trailing:   risk.TrailingSpec{Mode: risk.TrailingModePercent},
			},
			want: backtest.PolicyView{
				StopLossPercent:   14,
				TakeProfitPercent: 4,
				TrailingMode:      backtest.TrailingModePercent,
			},
		},
		{
			name: "Disabled trailing reaches the handler unchanged",
			in: risk.RiskPolicy{
				StopLoss:   risk.StopLossSpec{Percent: 14},
				TakeProfit: risk.TakeProfitSpec{Percent: 4},
				Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeDisabled},
			},
			want: backtest.PolicyView{
				StopLossPercent:   14,
				TakeProfitPercent: 4,
				TrailingMode:      backtest.TrailingModeDisabled,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := policyView(tc.in)
			if got != tc.want {
				t.Errorf("policyView() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestRegisterTickRiskHandler_FeedsATRFromIndicatorEvent is the regression
// guard for the live-only ATR feed bug
// (docs/investigation/2026-06-06-tick-sltp-atr-feed-bug.md): the live
// pipeline used to register the TickRiskHandler on EventTypeTick only, so
// UpdateATR was never called, currentATR stayed 0, and ATR trailing silently
// collapsed to the percent fallback — diverging live from backtest.
//
// This test exercises the REAL production wiring helper (registerTickRiskHandler)
// and the REAL handler constructor (NewTickRiskHandlerWithPolicy + policyView)
// through a real EventBus. An IndicatorEvent carrying ATR=400 is dispatched
// first; then a sequence of TickEvents whose pullback (700) sits between the
// percent distance (500) and the ATR distance (800). The position must survive
// — which is only possible if the ATR reached the handler via the Indicator
// registration. Drop the EventTypeIndicator line from registerTickRiskHandler
// and this test fails (the trailing stop fires early at the percent distance).
func TestRegisterTickRiskHandler_FeedsATRFromIndicatorEvent(t *testing.T) {
	exec := &atrWiringExecutor{
		positions: []eventengine.Position{{
			PositionID: 1, SymbolID: 1,
			Side:       entity.OrderSideBuy,
			EntryPrice: 10_000,
			Amount:     0.1,
		}},
	}
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 5}, // percent trailing distance = 500
		TakeProfit: risk.TakeProfitSpec{Percent: 10},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.0},
	}
	h := backtest.NewTickRiskHandlerWithPolicy("PT15M", exec, policyView(policy))

	bus := eventengine.NewEventBus()
	registerTickRiskHandler(bus, h)

	ctx := context.Background()

	// Feed ATR=400 via IndicatorEvent → ATR trailing distance becomes 800.
	atr := 400.0
	if err := bus.Dispatch(ctx, []entity.Event{entity.IndicatorEvent{
		SymbolID:  1,
		Interval:  "PT15M",
		Timestamp: 1,
		Primary:   entity.IndicatorSet{ATR: &atr},
	}}); err != nil {
		t.Fatalf("dispatch indicator: %v", err)
	}

	// Bar range kept wide so SL/TP never fires; only trailing can close.
	barLow, barHigh := 9_600.0, 10_999.0

	// Tick 1: push the high-water mark up to 10_900.
	if err := bus.Dispatch(ctx, []entity.Event{entity.TickEvent{
		SymbolID: 1, Interval: "PT15M", Timestamp: 100,
		Price: 10_900, BarLow: barLow, BarHigh: barHigh,
	}}); err != nil {
		t.Fatalf("dispatch tick1: %v", err)
	}

	// Tick 2: pull back to 10_200 → drop = 700. This is > percent distance
	// (500) but < ATR distance (800). With ATR fed, the position SURVIVES.
	// Without the Indicator registration, currentATR=0 collapses trailing to
	// the percent distance (500) and this tick closes the position.
	if err := bus.Dispatch(ctx, []entity.Event{entity.TickEvent{
		SymbolID: 1, Interval: "PT15M", Timestamp: 200,
		Price: 10_200, BarLow: barLow, BarHigh: barHigh,
	}}); err != nil {
		t.Fatalf("dispatch tick2: %v", err)
	}
	if len(exec.closes) != 0 {
		t.Fatalf("trailing fired at drop=700 (< ATR distance 800): ATR was NOT fed to the handler — Indicator registration missing. closes=%v", exec.closes)
	}

	// Tick 3: drop to 10_050 → drop = 850 >= 800, ATR trailing fires.
	if err := bus.Dispatch(ctx, []entity.Event{entity.TickEvent{
		SymbolID: 1, Interval: "PT15M", Timestamp: 300,
		Price: 10_050, BarLow: barLow, BarHigh: barHigh,
	}}); err != nil {
		t.Fatalf("dispatch tick3: %v", err)
	}
	if len(exec.closes) != 1 || exec.closes[0] != "trailing_stop" {
		t.Fatalf("expected one trailing_stop close at ATR distance, got %v", exec.closes)
	}
}
