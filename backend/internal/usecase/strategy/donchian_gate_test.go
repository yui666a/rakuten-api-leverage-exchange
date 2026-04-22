package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// TestConfigurableStrategy_DonchianGateBlocksBreakoutBuy is the PR-11 wiring
// confirmation test. With DonchianPeriod > 0 and lastPrice at or below
// Donchian20Upper, a breakout BUY must be blocked with a reason citing
// Donchian. Guards against the silent-no-op trap (cycle08 pattern) where a
// profile field compiles cleanly but never influences a signal decision.
func TestConfigurableStrategy_DonchianGateBlocksBreakoutBuy(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.DonchianPeriod = 20
	// Keep other breakout gates permissive so only the Donchian gate can fire.
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	// IndicatorSet that would fire a breakout BUY (squeeze released, price
	// above BB upper with volume) but whose lastPrice is exactly equal to
	// the Donchian20 upper — the gate uses strict `>` so equality blocks.
	// BBUpper is 118 (breakout threshold) and Donchian is 125 (gate block).
	ind := makeBreakoutBuyReadyIndicators()
	lastPrice := 120.0
	don := 125.0 // strictly above lastPrice, so gate blocks
	ind.Donchian20Upper = &don

	sig, err := s.Evaluate(context.Background(), &ind, nil, lastPrice, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig == nil {
		t.Fatalf("nil signal")
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when price <= Donchian upper, got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if !containsSubstring(sig.Reason, "Donchian") {
		t.Fatalf("expected reason to mention Donchian, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_DonchianGateAllowsBreakoutBuy: when lastPrice is
// strictly above Donchian20Upper, the gate passes and the breakout BUY fires.
func TestConfigurableStrategy_DonchianGateAllowsBreakoutBuy(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.DonchianPeriod = 20
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeBreakoutBuyReadyIndicators()
	lastPrice := 125.0
	don := 120.0 // lastPrice strictly above Donchian upper
	ind.Donchian20Upper = &don

	sig, err := s.Evaluate(context.Background(), &ind, nil, lastPrice, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig == nil {
		t.Fatalf("nil signal")
	}
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("expected breakout BUY, got %v (reason=%q)", sig.Action, sig.Reason)
	}
}

// TestConfigurableStrategy_DonchianGateMissingDonchianCountsAsFail is the
// nil-indicator edge: a profile that activates the gate must NOT emit a BUY
// when Donchian20Upper is nil (warmup). Matches the ADX / Stoch convention.
func TestConfigurableStrategy_DonchianGateMissingDonchianCountsAsFail(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.DonchianPeriod = 20
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeBreakoutBuyReadyIndicators()
	ind.Donchian20Upper = nil // warmup

	sig, err := s.Evaluate(context.Background(), &ind, nil, 125.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD on nil Donchian, got %v", sig.Action)
	}
	if !containsSubstring(sig.Reason, "Donchian") {
		t.Fatalf("expected Donchian block reason, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_DonchianGateZeroIsDisabled: the default 0-value on
// DonchianPeriod must not touch the signal path. A breakout BUY scenario with
// Donchian20Upper set high enough to block (if the gate were on) must still
// emit BUY when the gate is 0.
func TestConfigurableStrategy_DonchianGateZeroIsDisabled(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.DonchianPeriod = 0 // gate disabled
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeBreakoutBuyReadyIndicators()
	// Even though Donchian20Upper > lastPrice (would block if the gate were
	// active), the gate is disabled so BUY must still fire.
	don := 999.0
	ind.Donchian20Upper = &don

	sig, err := s.Evaluate(context.Background(), &ind, nil, 125.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("gate=0 should pass through; expected BUY, got %v (reason=%q)", sig.Action, sig.Reason)
	}
}

// TestConfigurableStrategy_DonchianGateBlocksBreakoutSell mirrors the BUY
// gate for the SELL direction. lastPrice at or above Donchian20Lower must
// block a breakout SELL when the gate is active.
func TestConfigurableStrategy_DonchianGateBlocksBreakoutSell(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Breakout.Enabled = true
	profile.SignalRules.Breakout.DonchianPeriod = 20
	profile.SignalRules.Breakout.ADXMin = 0
	profile.SignalRules.Breakout.RequireMACDConfirm = false

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeBreakoutSellReadyIndicators()
	lastPrice := 80.0
	don := 80.0 // equal — gate uses strict `<`
	ind.Donchian20Lower = &don

	sig, err := s.Evaluate(context.Background(), &ind, nil, lastPrice, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig == nil {
		t.Fatalf("nil signal")
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when price >= Donchian lower, got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if !containsSubstring(sig.Reason, "Donchian") {
		t.Fatalf("expected reason to mention Donchian, got %q", sig.Reason)
	}
}

// makeBreakoutBuyReadyIndicators builds an IndicatorSet that falls into the
// BREAKOUT stance (RecentSqueeze released upward with volume) and would fire
// a BUY from evaluateBreakout when evaluated at lastPrice=120. Only Donchian
// decisions can intervene in tests that set DonchianPeriod > 0.
//
// BB upper is 118 so the caller's lastPrice=120 is strictly above; callers
// vary Donchian20Upper to probe the gate.
func makeBreakoutBuyReadyIndicators() entity.IndicatorSet {
	sma20 := 110.0
	sma50 := 105.0
	ema12 := 112.0
	ema26 := 109.0
	rsi := 55.0
	hist := 2.0
	bbUpper := 118.0
	bbLower := 100.0
	bbMiddle := 110.0
	volRatio := 2.0 // above default BreakoutVolumeRatio=1.5
	squeeze := true
	return entity.IndicatorSet{
		SymbolID:      10,
		SMA20:         &sma20,
		SMA50:         &sma50,
		EMA12:         &ema12,
		EMA26:         &ema26,
		RSI14:         &rsi,
		Histogram:     &hist,
		BBUpper:       &bbUpper,
		BBLower:       &bbLower,
		BBMiddle:      &bbMiddle,
		VolumeRatio:   &volRatio,
		RecentSqueeze: &squeeze,
		Timestamp:     time.Now().Unix(),
	}
}

// makeBreakoutSellReadyIndicators mirrors the BUY helper for the SELL
// direction. BB lower is 82 so lastPrice=80 is strictly below.
func makeBreakoutSellReadyIndicators() entity.IndicatorSet {
	sma20 := 95.0
	sma50 := 100.0
	ema12 := 93.0
	ema26 := 97.0
	rsi := 45.0
	hist := -2.0
	bbUpper := 100.0
	bbLower := 82.0
	bbMiddle := 90.0
	volRatio := 2.0
	squeeze := true
	return entity.IndicatorSet{
		SymbolID:      10,
		SMA20:         &sma20,
		SMA50:         &sma50,
		EMA12:         &ema12,
		EMA26:         &ema26,
		RSI14:         &rsi,
		Histogram:     &hist,
		BBUpper:       &bbUpper,
		BBLower:       &bbLower,
		BBMiddle:      &bbMiddle,
		VolumeRatio:   &volRatio,
		RecentSqueeze: &squeeze,
		Timestamp:     time.Now().Unix(),
	}
}
