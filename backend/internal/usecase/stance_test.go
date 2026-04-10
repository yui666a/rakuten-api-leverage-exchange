package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestRuleBasedStanceResolver_RSIBelow25_Contrarian(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(20.0),
	})
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN, got %s", result.Stance)
	}
	if result.Source != "rule-based" {
		t.Fatalf("expected source rule-based, got %s", result.Source)
	}
}

func TestRuleBasedStanceResolver_RSIAbove75_Contrarian(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(80.0),
	})
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_SMAConverged_Hold(t *testing.T) {
	// divergence = |5000000 - 5000400| / 5000400 ≈ 0.00008 < 0.001
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5000000),
		SMA50: ptr(5000400),
		RSI14: ptr(50.0),
	})
	if result.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD for converged SMA, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_SMAUptrend_TrendFollow(t *testing.T) {
	// SMA20 > SMA50, divergence > 0.1%
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	})
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW for uptrend, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_SMADowntrend_TrendFollow(t *testing.T) {
	// SMA20 < SMA50, divergence > 0.1%
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(4900000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	})
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW for downtrend, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_NoIndicators_Hold(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{})
	if result.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD for no indicators, got %s", result.Stance)
	}
	if result.Reasoning != "insufficient indicator data" {
		t.Fatalf("expected 'insufficient indicator data', got %s", result.Reasoning)
	}
}

func TestRuleBasedStanceResolver_OverrideTakesPriority(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	resolver.SetOverride(entity.MarketStanceContrarian, "manual override", 60*time.Second)

	// Without override, this would be TREND_FOLLOW
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	})
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN from override, got %s", result.Stance)
	}
	if result.Source != "override" {
		t.Fatalf("expected source override, got %s", result.Source)
	}
	if result.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt to be set for override")
	}
}

func TestRuleBasedStanceResolver_ExpiredOverrideFallsBack(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// Set override with a very short TTL that's already expired
	resolver.SetOverride(entity.MarketStanceContrarian, "expired override", 0)

	// Should fall back to rules: TREND_FOLLOW
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	})
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW after expired override, got %s", result.Stance)
	}
	if result.Source != "rule-based" {
		t.Fatalf("expected source rule-based after expired override, got %s", result.Source)
	}
}

func TestRuleBasedStanceResolver_ClearOverride(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	resolver.SetOverride(entity.MarketStanceContrarian, "to be cleared", 60*time.Second)
	resolver.ClearOverride()

	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	})
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW after clearing override, got %s", result.Stance)
	}

	override := resolver.GetOverride()
	if override != nil {
		t.Fatal("expected nil override after ClearOverride")
	}
}
