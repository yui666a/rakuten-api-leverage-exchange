package main

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

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
