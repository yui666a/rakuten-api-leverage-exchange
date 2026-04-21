package strategy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
)

// TestConfigurableStrategy_ADXGateBlocksBelowThreshold is the PR-6 wiring
// confirmation test. It loads production.json with a trend_follow.adx_min
// override applied in-memory, feeds an IndicatorSet whose ADX is below the
// threshold, and asserts the engine returns HOLD with "ADX below threshold"
// reason rather than firing a trend-follow signal. This guards against a
// silent "profile field does nothing" regression of the kind that bit
// cycle08 with stop_loss_atr_multiplier.
func TestConfigurableStrategy_ADXGateBlocksBelowThreshold(t *testing.T) {
	profile := productionProfile(t)
	// Force trend-follow ADX gate to 99 so any reasonable ADX fails it.
	profile.SignalRules.TrendFollow.ADXMin = 99

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	// Build an IndicatorSet that would otherwise trigger a trend-follow
	// buy (SMA20 > SMA50, EMA12 > EMA26, RSI moderate, positive
	// histogram) but ADX is only 25 — well below the 99 cap.
	ind := makeTrendFollowReadyIndicators()
	adx := 25.0
	ind.ADX14 = &adx

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig == nil {
		t.Fatalf("expected non-nil signal")
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if sig.Reason == "" || !containsSubstring(sig.Reason, "ADX") {
		t.Fatalf("expected HOLD reason to mention ADX, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_ADXGateAllowsAboveThreshold is the converse:
// with ADX=50 and gate=20, the same IndicatorSet should NOT be blocked by
// the gate (the subsequent evaluator may still HOLD for its own reasons,
// but the reason must not cite ADX).
func TestConfigurableStrategy_ADXGateAllowsAboveThreshold(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.TrendFollow.ADXMin = 20

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	adx := 50.0
	ind.ADX14 = &adx

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig == nil {
		t.Fatalf("nil signal")
	}
	if containsSubstring(sig.Reason, "ADX") {
		t.Fatalf("signal reason should not mention ADX when above threshold; got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_ADXGateMissingADXCountsAsFail covers the
// spec-documented "unknown ADX => block" behaviour.
func TestConfigurableStrategy_ADXGateMissingADXCountsAsFail(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.TrendFollow.ADXMin = 20

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	ind.ADX14 = nil // unknown

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD on unknown ADX; got %v", sig.Action)
	}
	if !containsSubstring(sig.Reason, "ADX") {
		t.Fatalf("expected ADX block reason, got %q", sig.Reason)
	}
}

// containsSubstring is a tiny test helper so tests stay explicit about what
// they check without pulling strings into the signal-path comparison.
func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && findSubstring(s, sub) >= 0
}

func findSubstring(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// makeTrendFollowReadyIndicators builds an IndicatorSet that would
// otherwise produce a trend-follow BUY, so the ADX gate is the only thing
// between us and a non-HOLD signal.
func makeTrendFollowReadyIndicators() entity.IndicatorSet {
	sma20 := 110.0
	sma50 := 100.0
	ema12 := 111.0
	ema26 := 108.0
	rsi := 55.0
	hist := 1.5
	return entity.IndicatorSet{
		SymbolID:  10,
		SMA20:     &sma20,
		SMA50:     &sma50,
		EMA12:     &ema12,
		EMA26:     &ema26,
		RSI14:     &rsi,
		Histogram: &hist,
		Timestamp: time.Now().Unix(),
	}
}

// productionProfilePathForADX / productionProfileForADX mirror the helpers
// in configurable_strategy_test but we keep a separate, no-deps copy here
// so this file is self-contained.
func productionProfileForADX(t *testing.T) *entity.StrategyProfile {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			loader := strategyprofile.NewLoader(filepath.Join(dir, "profiles"))
			profile, err := loader.Load("production")
			if err != nil {
				t.Fatalf("load profile: %v", err)
			}
			return profile
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate module root")
		}
		dir = parent
	}
}
