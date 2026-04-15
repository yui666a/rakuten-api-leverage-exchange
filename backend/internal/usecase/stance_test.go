package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

func ptr(f float64) *float64 { return &f }

func boolPtr(b bool) *bool { return &b }

func TestRuleBasedStanceResolver_RSIBelow25_Contrarian(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(20.0),
	}, 0)
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
	}, 0)
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
	}, 0)
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
	}, 0)
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
	}, 0)
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW for downtrend, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_NoIndicators_Hold(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{}, 0)
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
	}, 0)
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
	// Use a mock repo with an already-expired override to simulate expiration
	repo := &mockStanceOverrideRepo{
		record: &repository.StanceOverrideRecord{
			Stance:    string(entity.MarketStanceContrarian),
			Reasoning: "expired override",
			SetAt:     time.Now().Add(-2 * time.Hour).Unix(),
			TTLSec:    60, // 1 minute TTL, set 2 hours ago → expired
		},
	}
	resolver := NewRuleBasedStanceResolver(repo)

	// Should fall back to rules: TREND_FOLLOW
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	}, 0)
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
	}, 0)
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW after clearing override, got %s", result.Stance)
	}

	override := resolver.GetOverride()
	if override != nil {
		t.Fatal("expected nil override after ClearOverride")
	}
}

// --- RSI boundary value tests ---

func TestRuleBasedStanceResolver_RSIExactly25_NotContrarian(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(25.0),
	}, 0)
	if result.Stance == entity.MarketStanceContrarian {
		t.Fatalf("RSI=25.0 should NOT be CONTRARIAN, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_RSIExactly75_NotContrarian(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(75.0),
	}, 0)
	if result.Stance == entity.MarketStanceContrarian {
		t.Fatalf("RSI=75.0 should NOT be CONTRARIAN, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_RSI24_9_Contrarian(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(24.9),
	}, 0)
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("RSI=24.9 should be CONTRARIAN, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_RSI75_1_Contrarian(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(75.1),
	}, 0)
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("RSI=75.1 should be CONTRARIAN, got %s", result.Stance)
	}
}

// --- Mock-based persistence tests ---

type mockStanceOverrideRepo struct {
	record       *repository.StanceOverrideRecord
	saveErr      error
	loadErr      error
	deleteErr    error
	saveCalled   int
	deleteCalled int
}

func (m *mockStanceOverrideRepo) Save(_ context.Context, rec repository.StanceOverrideRecord) error {
	m.saveCalled++
	if m.saveErr != nil {
		return m.saveErr
	}
	copied := rec
	m.record = &copied
	return nil
}

func (m *mockStanceOverrideRepo) Load(_ context.Context) (*repository.StanceOverrideRecord, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return m.record, nil
}

func (m *mockStanceOverrideRepo) Delete(_ context.Context) error {
	m.deleteCalled++
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.record = nil
	return nil
}

func TestRuleBasedStanceResolver_SetOverride_CallsRepoSave(t *testing.T) {
	repo := &mockStanceOverrideRepo{}
	resolver := NewRuleBasedStanceResolver(repo)
	resolver.SetOverride(entity.MarketStanceContrarian, "test", 5*time.Minute)

	if repo.saveCalled != 1 {
		t.Fatalf("expected repo.Save to be called once, got %d", repo.saveCalled)
	}
	if repo.record == nil {
		t.Fatal("expected record to be saved")
	}
	if repo.record.Stance != string(entity.MarketStanceContrarian) {
		t.Fatalf("expected stance %s, got %s", entity.MarketStanceContrarian, repo.record.Stance)
	}
}

func TestRuleBasedStanceResolver_RestoresFromRepo(t *testing.T) {
	repo := &mockStanceOverrideRepo{
		record: &repository.StanceOverrideRecord{
			Stance:    string(entity.MarketStanceContrarian),
			Reasoning: "restored override",
			SetAt:     time.Now().Unix(),
			TTLSec:    3600,
		},
	}
	resolver := NewRuleBasedStanceResolver(repo)

	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	}, 0)
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN from restored override, got %s", result.Stance)
	}
	if result.Source != "override" {
		t.Fatalf("expected source override, got %s", result.Source)
	}
}

func TestRuleBasedStanceResolver_ClearOverride_CallsRepoDelete(t *testing.T) {
	repo := &mockStanceOverrideRepo{}
	resolver := NewRuleBasedStanceResolver(repo)
	resolver.SetOverride(entity.MarketStanceContrarian, "test", 5*time.Minute)
	resolver.ClearOverride()

	if repo.deleteCalled != 1 {
		t.Fatalf("expected repo.Delete to be called once, got %d", repo.deleteCalled)
	}
	if repo.record != nil {
		t.Fatal("expected record to be deleted")
	}
}

func TestRuleBasedStanceResolver_ExpiredOverrideOnRestore_AutoDeleted(t *testing.T) {
	repo := &mockStanceOverrideRepo{
		record: &repository.StanceOverrideRecord{
			Stance:    string(entity.MarketStanceContrarian),
			Reasoning: "expired",
			SetAt:     time.Now().Add(-2 * time.Hour).Unix(),
			TTLSec:    60, // 1 minute TTL, set 2 hours ago → expired
		},
	}
	resolver := NewRuleBasedStanceResolver(repo)

	if repo.deleteCalled != 1 {
		t.Fatalf("expected repo.Delete to be called for expired override, got %d", repo.deleteCalled)
	}

	// The override should not be active
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	}, 0)
	if result.Source != "rule-based" {
		t.Fatalf("expected rule-based after expired override cleanup, got %s", result.Source)
	}
}

func TestRuleBasedStanceResolver_TTLClampedToMin(t *testing.T) {
	repo := &mockStanceOverrideRepo{}
	resolver := NewRuleBasedStanceResolver(repo)
	resolver.SetOverride(entity.MarketStanceContrarian, "short ttl", 0)

	if repo.record == nil {
		t.Fatal("expected record to be saved")
	}
	if repo.record.TTLSec != 60 {
		t.Fatalf("expected TTL clamped to 60s, got %d", repo.record.TTLSec)
	}
}

func TestRuleBasedStanceResolver_TTLClampedToMax(t *testing.T) {
	repo := &mockStanceOverrideRepo{}
	resolver := NewRuleBasedStanceResolver(repo)
	resolver.SetOverride(entity.MarketStanceContrarian, "long ttl", 48*time.Hour)

	if repo.record == nil {
		t.Fatal("expected record to be saved")
	}
	expectedTTL := int64((1440 * time.Minute).Seconds())
	if repo.record.TTLSec != expectedTTL {
		t.Fatalf("expected TTL clamped to %d, got %d", expectedTTL, repo.record.TTLSec)
	}
}

func TestRuleBasedStanceResolver_RepoSaveError_LoggedNotPanicked(t *testing.T) {
	repo := &mockStanceOverrideRepo{
		saveErr: errors.New("db connection failed"),
	}
	resolver := NewRuleBasedStanceResolver(repo)
	// Should not panic even if Save fails
	resolver.SetOverride(entity.MarketStanceContrarian, "test", 5*time.Minute)

	if repo.saveCalled != 1 {
		t.Fatalf("expected Save to be called, got %d", repo.saveCalled)
	}
}

func TestRuleBasedStanceResolver_RepoDeleteError_LoggedNotPanicked(t *testing.T) {
	repo := &mockStanceOverrideRepo{
		deleteErr: errors.New("db connection failed"),
	}
	resolver := NewRuleBasedStanceResolver(repo)
	resolver.SetOverride(entity.MarketStanceContrarian, "test", 5*time.Minute)
	// Should not panic even if Delete fails
	resolver.ClearOverride()

	if repo.deleteCalled != 1 {
		t.Fatalf("expected Delete to be called, got %d", repo.deleteCalled)
	}
}

func TestRuleBasedStanceResolver_ResolveAt_UsesInjectedTimeForExpiry(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	resolver.SetOverride(entity.MarketStanceContrarian, "manual", 2*time.Minute)

	early := resolver.ResolveAt(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	}, 0, time.Now().Add(1*time.Minute))
	if early.Source != "override" {
		t.Fatalf("expected override before expiry, got %s", early.Source)
	}

	late := resolver.ResolveAt(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	}, 0, time.Now().Add(3*time.Minute))
	if late.Source != "rule-based" {
		t.Fatalf("expected rule-based after expiry, got %s", late.Source)
	}
}

func TestRuleBasedStanceResolverWithOptions_DisableOverride(t *testing.T) {
	resolver := NewRuleBasedStanceResolverWithOptions(nil, RuleBasedStanceResolverOptions{
		DisableOverride: true,
	})
	resolver.SetOverride(entity.MarketStanceContrarian, "manual", 5*time.Minute)

	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	}, 0)
	if result.Source != "rule-based" {
		t.Fatalf("expected rule-based when override disabled, got %s", result.Source)
	}
	if resolver.GetOverride() != nil {
		t.Fatal("expected no active override when override disabled")
	}
}

// --- BREAKOUT tests ---

func TestRuleBasedStanceResolver_Breakout_UpwardWithVolume(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// RecentSqueeze=true, price > BBUpper, VolumeRatio >= 1.5
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(4900000),
		RSI14:         ptr(55.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.015),
		VolumeRatio:   ptr(2.0),
		RecentSqueeze: boolPtr(true),
	}, 5200000)
	if result.Stance != entity.MarketStanceBreakout {
		t.Fatalf("expected BREAKOUT for upward breakout with volume, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_Breakout_DownwardWithVolume(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(5100000),
		RSI14:         ptr(45.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.015),
		VolumeRatio:   ptr(1.8),
		RecentSqueeze: boolPtr(true),
	}, 4800000)
	if result.Stance != entity.MarketStanceBreakout {
		t.Fatalf("expected BREAKOUT for downward breakout with volume, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_Squeeze_NoBreakout_Hold(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// RecentSqueeze=true, but price is between bands → HOLD
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(5000400),
		RSI14:         ptr(50.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.01),
		VolumeRatio:   ptr(2.0),
		RecentSqueeze: boolPtr(true),
	}, 5050000)
	if result.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD for squeeze without breakout, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_Breakout_LowVolume_Hold(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// Price > BBUpper + RecentSqueeze, but VolumeRatio < 1.5 → HOLD
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(5000400),
		RSI14:         ptr(50.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.01),
		VolumeRatio:   ptr(1.0),
		RecentSqueeze: boolPtr(true),
	}, 5200000)
	if result.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD for breakout without volume confirmation, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_Breakout_NoRecentSqueeze_TrendFollow(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// Price > BBUpper + high volume, but RecentSqueeze=false → not a breakout
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5100000),
		SMA50:         ptr(5000000),
		RSI14:         ptr(55.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.05),
		VolumeRatio:   ptr(2.0),
		RecentSqueeze: boolPtr(false),
	}, 5200000)
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW without recent squeeze, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_RSI_Contrarian_OverridesBreakout(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// RSI < 25 takes priority over breakout conditions
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(4900000),
		RSI14:         ptr(20.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.01),
		VolumeRatio:   ptr(2.0),
		RecentSqueeze: boolPtr(true),
	}, 5200000)
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN to override BREAKOUT when RSI extreme, got %s", result.Stance)
	}
}
