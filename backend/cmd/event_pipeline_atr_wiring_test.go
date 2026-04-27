package main

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// TestNewEventDrivenPipeline_PropagatesATRMultipliers locks in the wiring
// fix: profile.Risk.{StopLossATRMultiplier,TrailingATRMultiplier} must reach
// the pipeline so the SetATRMultipliers call inside runEventLoop receives the
// profile value rather than 0.
//
// Before this PR the live pipeline never called SetATRMultipliers, which
// silently disabled ATR-based trailing distance for every promoted profile
// (production_ltc_60k.trailing_atr_multiplier=2.5 was effectively ignored).
// The regression surface is small but invisible: live HOLD/SL/TP looked
// correct on the dashboard while the trailing exit point was off by an
// order of magnitude vs the backtest the profile was tuned against.
func TestNewEventDrivenPipeline_PropagatesATRMultipliers(t *testing.T) {
	p := NewEventDrivenPipeline(
		EventDrivenPipelineConfig{
			SymbolID:              7,
			StopLossATRMultiplier: 1.5,
			TrailingATRMultiplier: 2.5,
		},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if got, want := p.stopLossATRMultiplier, 1.5; got != want {
		t.Errorf("stopLossATRMultiplier = %v, want %v", got, want)
	}
	if got, want := p.trailingATRMultiplier, 2.5; got != want {
		t.Errorf("trailingATRMultiplier = %v, want %v", got, want)
	}
}

// TestNewEventDrivenPipeline_ATRMultipliersZeroByDefault pins the legacy
// behaviour for profiles that leave the multipliers unset: the percent-only
// SL/trailing path must remain bit-identical so existing live runs without
// an ATR multiplier do not change behaviour after this PR.
func TestNewEventDrivenPipeline_ATRMultipliersZeroByDefault(t *testing.T) {
	p := NewEventDrivenPipeline(
		EventDrivenPipelineConfig{SymbolID: 7},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if p.stopLossATRMultiplier != 0 {
		t.Errorf("stopLossATRMultiplier = %v, want 0", p.stopLossATRMultiplier)
	}
	if p.trailingATRMultiplier != 0 {
		t.Errorf("trailingATRMultiplier = %v, want 0", p.trailingATRMultiplier)
	}
}

// TestSnapshotCarriesATRMultipliers ensures runEventLoop's snapshot copy
// (taken under lock) propagates the multipliers to the TickRiskHandler
// configuration site. snapshotLocked is the only path through which the
// SetATRMultipliers call site reads these values.
func TestSnapshotCarriesATRMultipliers(t *testing.T) {
	p := NewEventDrivenPipeline(
		EventDrivenPipelineConfig{
			SymbolID:              7,
			StopLossATRMultiplier: 1.5,
			TrailingATRMultiplier: 2.5,
		},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	snap := p.snapshot()
	if snap.stopLossATRMultiplier != 1.5 {
		t.Errorf("snap.stopLossATRMultiplier = %v, want 1.5", snap.stopLossATRMultiplier)
	}
	if snap.trailingATRMultiplier != 2.5 {
		t.Errorf("snap.trailingATRMultiplier = %v, want 2.5", snap.trailingATRMultiplier)
	}
}

func TestLiveStopLossATRMultiplier(t *testing.T) {
	tests := []struct {
		name    string
		profile *entity.StrategyProfile
		want    float64
	}{
		{name: "nil profile returns 0", profile: nil, want: 0},
		{
			name:    "zero multiplier returns 0",
			profile: &entity.StrategyProfile{Risk: entity.StrategyRiskConfig{StopLossATRMultiplier: 0}},
			want:    0,
		},
		{
			name:    "configured multiplier wins",
			profile: &entity.StrategyProfile{Risk: entity.StrategyRiskConfig{StopLossATRMultiplier: 1.5}},
			want:    1.5,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := liveStopLossATRMultiplier(tc.profile); got != tc.want {
				t.Errorf("liveStopLossATRMultiplier = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLiveTrailingATRMultiplier(t *testing.T) {
	tests := []struct {
		name    string
		profile *entity.StrategyProfile
		want    float64
	}{
		{name: "nil profile returns 0", profile: nil, want: 0},
		{
			name:    "zero multiplier returns 0",
			profile: &entity.StrategyProfile{Risk: entity.StrategyRiskConfig{TrailingATRMultiplier: 0}},
			want:    0,
		},
		{
			name:    "promoted production_ltc_60k value passes through",
			profile: &entity.StrategyProfile{Risk: entity.StrategyRiskConfig{TrailingATRMultiplier: 2.5}},
			want:    2.5,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := liveTrailingATRMultiplier(tc.profile); got != tc.want {
				t.Errorf("liveTrailingATRMultiplier = %v, want %v", got, tc.want)
			}
		})
	}
}
