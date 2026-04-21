package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// TestConfigurableStrategy_ContrarianStochEntryGateBlocks validates PR-7
// wiring for the contrarian BUY stochastics gate. A profile with
// StochEntryMax = 10 must block a contrarian BUY when %K = 25 (not oversold
// enough) even though RSI is deep into oversold territory. This is the
// silent-no-op regression guard (cycle08 pattern).
func TestConfigurableStrategy_ContrarianStochEntryGateBlocks(t *testing.T) {
	profile := productionProfile(t)
	// Ensure contrarian is enabled; disable the ADX gate so only the Stoch
	// gate is in play.
	profile.SignalRules.Contrarian.Enabled = true
	profile.SignalRules.Contrarian.ADXMax = 0
	profile.SignalRules.Contrarian.StochEntryMax = 10

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeContrarianBuyReadyIndicators()
	stochK := 25.0
	ind.StochK14_3 = &stochK

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig == nil {
		t.Fatalf("nil signal")
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if !containsSubstring(sig.Reason, "Stoch") {
		t.Fatalf("expected reason to mention Stoch, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_ContrarianStochEntryGateAllows: with %K deep in
// oversold territory the gate passes and the contrarian BUY fires.
func TestConfigurableStrategy_ContrarianStochEntryGateAllows(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Contrarian.Enabled = true
	profile.SignalRules.Contrarian.ADXMax = 0
	profile.SignalRules.Contrarian.StochEntryMax = 20

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeContrarianBuyReadyIndicators()
	stochK := 5.0 // deeply oversold
	ind.StochK14_3 = &stochK

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig == nil {
		t.Fatalf("nil signal")
	}
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("expected contrarian BUY, got %v (reason=%q)", sig.Action, sig.Reason)
	}
}

// TestConfigurableStrategy_ContrarianStochExitGateBlocks: mirror for the
// SELL direction.
func TestConfigurableStrategy_ContrarianStochExitGateBlocks(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Contrarian.Enabled = true
	profile.SignalRules.Contrarian.ADXMax = 0
	profile.SignalRules.Contrarian.StochExitMin = 90

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeContrarianSellReadyIndicators()
	stochK := 75.0 // overbought but below StochExitMin
	ind.StochK14_3 = &stochK

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %v (reason=%q)", sig.Action, sig.Reason)
	}
	if !containsSubstring(sig.Reason, "Stoch") {
		t.Fatalf("expected reason to mention Stoch, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_ContrarianStochGateMissingStochCountsAsFail covers
// the "unknown %K => block" behaviour mirroring ADX gate semantics.
func TestConfigurableStrategy_ContrarianStochGateMissingStochCountsAsFail(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Contrarian.Enabled = true
	profile.SignalRules.Contrarian.ADXMax = 0
	profile.SignalRules.Contrarian.StochEntryMax = 20

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeContrarianBuyReadyIndicators()
	ind.StochK14_3 = nil // unknown

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD on unknown Stoch, got %v", sig.Action)
	}
	if !containsSubstring(sig.Reason, "Stoch") {
		t.Fatalf("expected Stoch block reason, got %q", sig.Reason)
	}
}

// TestConfigurableStrategy_StochGateZeroIsDisabled: the default 0-value on
// StochEntryMax / StochExitMin must behave as "gate disabled" so legacy
// profiles are unaffected. This is the production-safety invariant.
func TestConfigurableStrategy_StochGateZeroIsDisabled(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.Contrarian.Enabled = true
	profile.SignalRules.Contrarian.ADXMax = 0
	profile.SignalRules.Contrarian.StochEntryMax = 0
	profile.SignalRules.Contrarian.StochExitMin = 0

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	ind := makeContrarianBuyReadyIndicators()
	// Intentionally omit StochK14_3 — a disabled gate must not care.

	sig, err := s.Evaluate(context.Background(), &ind, nil, 100.0, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Action != entity.SignalActionBuy {
		t.Fatalf("gate=0 should pass through; expected BUY, got %v (reason=%q)", sig.Action, sig.Reason)
	}
}

// makeContrarianBuyReadyIndicators builds an IndicatorSet that falls into
// the CONTRARIAN stance (RSI <= oversold threshold) and would produce a BUY
// from evaluateContrarian. The Stoch gate is the only thing that can
// intervene.
func makeContrarianBuyReadyIndicators() entity.IndicatorSet {
	sma20 := 100.5
	sma50 := 100.0 // near each other so stance may lean on RSI extremes
	ema12 := 100.6
	ema26 := 100.2
	rsi := 15.0 // well under any typical oversold threshold
	hist := 0.0
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

// makeContrarianSellReadyIndicators mirrors the above for the SELL direction.
func makeContrarianSellReadyIndicators() entity.IndicatorSet {
	sma20 := 100.0
	sma50 := 100.5
	ema12 := 100.3
	ema26 := 100.6
	rsi := 85.0 // well above any typical overbought threshold
	hist := 0.0
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
