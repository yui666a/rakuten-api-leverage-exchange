package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// stubSnapshot lets tests inject a canned IndicatorSet + price into the
// StrategyHandler without wiring up a real pipeline/market service. Keeps
// the test independent of the live snapshot implementation.
type stubSnapshot struct {
	indicators entity.IndicatorSet
	price      float64
}

func (s *stubSnapshot) Snapshot(_ context.Context) (entity.IndicatorSet, float64) {
	return s.indicators, s.price
}

func ptrFloat(v float64) *float64 { return &v }
func ptrBool(v bool) *bool        { return &v }

// TestStrategyHandler_GetStrategy_WithLiveSnapshot_TrendFollow feeds a
// well-formed trending indicator snapshot through the handler and checks
// that GET /strategy no longer returns the legacy "insufficient indicator
// data" HOLD. Without WithLiveSnapshot the handler uses an empty
// IndicatorSet and therefore always emits HOLD — this test is the
// regression guard for the dashboard bug that kicked off the fix.
func TestStrategyHandler_GetStrategy_WithLiveSnapshot_TrendFollow(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)

	// An uptrending snapshot: SMA20 > SMA50 by more than the convergence
	// threshold (0.002) so the rule-based resolver commits to TREND_FOLLOW
	// instead of HOLD.
	indicators := entity.IndicatorSet{
		SMA20:         ptrFloat(120),
		SMA50:         ptrFloat(100),
		RSI14:         ptrFloat(55),
		BBBandwidth:   ptrFloat(0.05),
		VolumeRatio:   ptrFloat(1.0),
		RecentSqueeze: ptrBool(false),
	}
	snap := &stubSnapshot{indicators: indicators, price: 12000}

	h := NewStrategyHandler(resolver).WithLiveSnapshot(snap)
	w := doRequest(h.GetStrategy, http.MethodGet, "/strategy", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["stance"] == "HOLD" {
		t.Fatalf("live snapshot should unblock HOLD; got stance=%v reasoning=%v", body["stance"], body["reasoning"])
	}
	if body["source"] != "rule-based" {
		t.Fatalf("expected source 'rule-based', got %v", body["source"])
	}
}

// TestStrategyHandler_GetStrategy_WithLiveSnapshot_ZeroPriceFallsBackToHold
// documents that an empty snapshot (warmup / pipeline idle) preserves the
// legacy behaviour so dashboards in that state aren't noisy.
func TestStrategyHandler_GetStrategy_WithLiveSnapshot_ZeroPriceFallsBackToHold(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	snap := &stubSnapshot{} // empty indicators, price 0

	h := NewStrategyHandler(resolver).WithLiveSnapshot(snap)
	w := doRequest(h.GetStrategy, http.MethodGet, "/strategy", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["stance"] != "HOLD" {
		t.Fatalf("empty snapshot should fall back to HOLD, got %v", body["stance"])
	}
}
