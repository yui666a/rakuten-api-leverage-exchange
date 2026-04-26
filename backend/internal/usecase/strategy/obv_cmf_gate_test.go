package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// TestConfigurableStrategy_OBVAlignmentBlocksTrendFollowBuy verifies the
// PR-9 wiring: when RequireOBVAlignment is true and OBVSlope is negative
// (net selling volume), a trend-follow BUY must be blocked. Silent-no-op
// regression guard.
func TestConfigurableStrategy_OBVAlignmentBlocksTrendFollowBuy(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.TrendFollow.RequireOBVAlignment = true
	// Disable ADX gate so only OBV can block.
	profile.SignalRules.TrendFollow.ADXMin = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	slope := -100.0
	ind.OBVSlope = &slope

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if !containsSubstring(sig.Reason, "OBV") {
		t.Fatalf("expected OBV in reason, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_OBVAlignmentAllowsTrendFollowBuy: with positive
// OBVSlope the gate passes and BUY fires.
func TestConfigurableStrategy_OBVAlignmentAllowsTrendFollowBuy(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.TrendFollow.RequireOBVAlignment = true
	profile.SignalRules.TrendFollow.ADXMin = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	slope := 100.0
	ind.OBVSlope = &slope

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY, got %v (reason=%q)", sig.Action, sig.Reason)
	}
}

// TestConfigurableStrategy_OBVAlignmentMissingCountsAsFail: nil OBVSlope
// during warmup fails the gate.
func TestConfigurableStrategy_OBVAlignmentMissingCountsAsFail(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.TrendFollow.RequireOBVAlignment = true
	profile.SignalRules.TrendFollow.ADXMin = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	ind.OBVSlope = nil

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD on nil OBVSlope, got %v", sig.Action)
	}
	if !containsSubstring(sig.Reason, "OBV") {
		t.Fatalf("expected OBV in reason, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_OBVAlignmentFalseIsDisabled: when the toggle is
// off, an adversarial OBVSlope must not affect the BUY path.
func TestConfigurableStrategy_OBVAlignmentFalseIsDisabled(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.TrendFollow.RequireOBVAlignment = false
	profile.SignalRules.TrendFollow.ADXMin = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	slope := -999.0
	ind.OBVSlope = &slope

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("gate=false should pass through; expected BUY, got %v (reason=%q)", sig.Action, sig.Reason)
	}
}

// TestConfigurableStrategy_CMFBuyGateBlocks is the PR-9 CMF wiring guard:
// when CMFBuyMin > 0 and CMF is below the threshold, a breakout BUY is
// blocked.
func TestConfigurableStrategy_CMFBuyGateBlocks(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.CMFBuyMin = 0.2
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false
	profile.SignalRules.Breakout.DonchianPeriod = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeBreakoutBuyReadyIndicators()
	cmf := 0.05
	ind.CMF = &cmf

	sig, err := s.Evaluate(context.Background(), &ind, nil, 120.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when CMF below threshold, got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if !containsSubstring(sig.Reason, "CMF") {
		t.Fatalf("expected CMF in reason, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_CMFBuyGateAllows: with CMF above threshold the
// breakout BUY fires.
func TestConfigurableStrategy_CMFBuyGateAllows(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.CMFBuyMin = 0.1
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false
	profile.SignalRules.Breakout.DonchianPeriod = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeBreakoutBuyReadyIndicators()
	cmf := 0.25
	ind.CMF = &cmf

	sig, err := s.Evaluate(context.Background(), &ind, nil, 120.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY with CMF above threshold, got %v (reason=%q)", sig.Action, sig.Reason)
	}
}

// TestConfigurableStrategy_CMFGateZeroIsDisabled: CMFBuyMin=0 must not
// touch the signal path even when CMF is strongly negative.
func TestConfigurableStrategy_CMFGateZeroIsDisabled(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.CMFBuyMin = 0 // disabled
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false
	profile.SignalRules.Breakout.DonchianPeriod = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeBreakoutBuyReadyIndicators()
	cmf := -0.9
	ind.CMF = &cmf

	sig, err := s.Evaluate(context.Background(), &ind, nil, 120.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("gate=0 should pass through; expected BUY, got %v (reason=%q)", sig.Action, sig.Reason)
	}
}

// TestConfigurableStrategy_CMFSellGateBlocks: the SELL-direction mirror of
// the BUY gate. With CMFSellMax = -0.2 and CMF = -0.05, the SELL must be
// blocked (|CMF| too small = not selling-pressure enough).
func TestConfigurableStrategy_CMFSellGateBlocks(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.CMFSellMax = -0.2
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false
	profile.SignalRules.Breakout.DonchianPeriod = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeBreakoutSellReadyIndicators()
	cmf := -0.05
	ind.CMF = &cmf

	sig, err := s.Evaluate(context.Background(), &ind, nil, 80.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if !containsSubstring(sig.Reason, "CMF") {
		t.Fatalf("expected CMF in reason, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_CMFGateMissingCountsAsFail: nil CMF during
// warmup fails the gate.
func TestConfigurableStrategy_CMFGateMissingCountsAsFail(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.CMFBuyMin = 0.1
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false
	profile.SignalRules.Breakout.DonchianPeriod = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeBreakoutBuyReadyIndicators()
	ind.CMF = nil

	sig, err := s.Evaluate(context.Background(), &ind, nil, 120.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD on nil CMF, got %v", sig.Action)
	}
	if !containsSubstring(sig.Reason, "CMF") {
		t.Fatalf("expected CMF in reason, got %q", sig.Reason)
	}
}
