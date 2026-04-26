package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// TestConfigurableStrategy_HTFIchimokuModeBlocksBuyInsideCloud is the PR-8
// wiring confirmation. With htf_filter.mode="ichimoku" a trend-follow BUY
// against a higher-timeframe cloud where price sits inside the cloud must
// be blocked ("inside cloud blocks buy"). This guards against silent-no-op
// regression for the new mode field.
func TestConfigurableStrategy_HTFIchimokuModeBlocksBuyInsideCloud(t *testing.T) {
	profile := productionProfile(t)
	profile.HTFFilter.Enabled = true
	profile.HTFFilter.BlockCounterTrend = true
	profile.HTFFilter.Mode = "ichimoku"
	// Disable the production ADX gate so only the HTF filter is in play.
	profile.SignalRules.TrendFollow.ADXMin = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	// Build a higher-TF indicator set whose cloud is 90..110 and last
	// price (100 on the primary TF) is inside it.
	higherTF := &entity.IndicatorSet{
		Ichimoku: makeCloud(110, 90),
	}

	sig, err := s.Evaluate(context.Background(), &ind, higherTF, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD inside cloud; got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if !containsSubstring(sig.Reason, "cloud") {
		t.Fatalf("expected reason to mention cloud; got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_HTFIchimokuModeAllowsBuyAboveCloud: price above
// cloud = HTF uptrend => BUY not blocked.
func TestConfigurableStrategy_HTFIchimokuModeAllowsBuyAboveCloud(t *testing.T) {
	profile := productionProfile(t)
	profile.HTFFilter.Enabled = true
	profile.HTFFilter.BlockCounterTrend = true
	profile.HTFFilter.Mode = "ichimoku"
	profile.SignalRules.TrendFollow.ADXMin = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	higherTF := &entity.IndicatorSet{
		Ichimoku: makeCloud(95, 85), // cloud well below lastPrice=100
	}

	sig, err := s.Evaluate(context.Background(), &ind, higherTF, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY above cloud; got %v (reason=%q)", sig.Action, sig.Reason)
	}
}

// TestConfigurableStrategy_HTFIchimokuModeMissingCloudDoesNothing: no cloud
// data => falls through, no block, no boost. This is the partial-warmup
// invariant: absence of data must never silently open a counter-trend BUY.
func TestConfigurableStrategy_HTFIchimokuModeMissingCloudDoesNothing(t *testing.T) {
	profile := productionProfile(t)
	profile.HTFFilter.Enabled = true
	profile.HTFFilter.BlockCounterTrend = true
	profile.HTFFilter.Mode = "ichimoku"
	profile.SignalRules.TrendFollow.ADXMin = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	higherTF := &entity.IndicatorSet{} // no Ichimoku snapshot

	sig, err := s.Evaluate(context.Background(), &ind, higherTF, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// The underlying indicator would have produced a BUY; with no HTF
	// data and mode=ichimoku the filter cannot decide, so we expect the
	// primary signal to pass through unchanged.
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("expected pass-through BUY; got %v (reason=%q)", sig.Action, sig.Reason)
	}
	// And the reason must not cite the cloud gate.
	if containsSubstring(sig.Reason, "cloud") {
		t.Fatalf("unexpected cloud-gate reason on missing data: %q", sig.Reason)
	}
}

// TestConfigurableStrategy_HTFEMAModeDefaultUnchanged: empty Mode (and
// "ema") must preserve legacy SMA-based behaviour so existing profiles
// keep working.
func TestConfigurableStrategy_HTFEMAModeDefaultUnchanged(t *testing.T) {
	profile := productionProfile(t)
	profile.HTFFilter.Enabled = true
	profile.HTFFilter.BlockCounterTrend = true
	profile.HTFFilter.Mode = "" // legacy
	profile.SignalRules.TrendFollow.ADXMin = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeTrendFollowReadyIndicators()
	down20 := 100.0
	down50 := 110.0 // SMAShort < SMALong => higher-TF downtrend
	higherTF := &entity.IndicatorSet{
		SMAShort: &down20,
		SMALong: &down50,
	}

	sig, err := s.Evaluate(context.Background(), &ind, higherTF, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD under legacy EMA-mode downtrend; got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if containsSubstring(sig.Reason, "cloud") {
		t.Fatalf("legacy mode must not mention cloud; got %q", sig.Reason)
	}
}

// makeCloud builds an IchimokuSnapshot whose SenkouA/SenkouB bound the
// cloud. Other lines are left nil because the HTF filter only consults A/B.
func makeCloud(senkouA, senkouB float64) *entity.IchimokuSnapshot {
	a := senkouA
	b := senkouB
	return &entity.IchimokuSnapshot{
		SenkouA: &a,
		SenkouB: &b,
	}
}
