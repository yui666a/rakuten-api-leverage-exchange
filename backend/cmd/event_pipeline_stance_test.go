package main

import (
	"context"
	"sync"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// fakeStanceResolver records the last Resolve call and returns a canned
// stance, mirroring the contract the live pipeline expects from the
// rule-based resolver without pulling its full constructor + repo
// dependency into the test.
type fakeStanceResolver struct {
	mu          sync.Mutex
	calls       int
	gotIndCount int
	gotPrice    float64
	canned      entity.MarketStance
}

func (f *fakeStanceResolver) Resolve(_ context.Context, indicators entity.IndicatorSet, lastPrice float64) usecase.StanceResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if indicators.SMAShort != nil {
		f.gotIndCount++
	}
	f.gotPrice = lastPrice
	return usecase.StanceResult{Stance: f.canned, Source: "rule-based"}
}

// TestCurrentStance_NoResolverReturnsUnknown pins the legacy contract:
// without a wired resolver the recorder must continue to log "UNKNOWN", so
// existing integration tests that pre-date PR2 stay bit-identical.
func TestCurrentStance_NoResolverReturnsUnknown(t *testing.T) {
	p := &EventDrivenPipeline{}
	if got := p.currentStance(context.Background()); got != "UNKNOWN" {
		t.Errorf("currentStance with nil resolver = %q, want UNKNOWN", got)
	}
}

// TestCurrentStance_NoIndicatorsYetReturnsUnknown covers the post-restart
// window where the resolver is wired but no IndicatorEvent has fired. The
// resolver must NOT be called against an empty IndicatorSet — it would
// classify HOLD via "insufficient indicator data", which would mask the
// real "we have no data" state. UNKNOWN tells operators "stance is not
// observable yet" instead of "stance is HOLD".
func TestCurrentStance_NoIndicatorsYetReturnsUnknown(t *testing.T) {
	resolver := &fakeStanceResolver{canned: entity.MarketStanceTrendFollow}
	p := &EventDrivenPipeline{stanceResolver: resolver}

	if got := p.currentStance(context.Background()); got != "UNKNOWN" {
		t.Errorf("currentStance before first IndicatorEvent = %q, want UNKNOWN", got)
	}
	if resolver.calls != 0 {
		t.Errorf("resolver.Resolve called %d times before any indicator event, want 0", resolver.calls)
	}
}

// TestCurrentStance_AfterTapResolvesViaResolver verifies the end-to-end
// path: indicatorEventTap caches an IndicatorEvent → currentStance feeds
// the cached snapshot into the resolver → the result is returned to the
// recorder.
func TestCurrentStance_AfterTapResolvesViaResolver(t *testing.T) {
	resolver := &fakeStanceResolver{canned: entity.MarketStanceContrarian}
	p := &EventDrivenPipeline{stanceResolver: resolver}
	tap := &indicatorEventTap{pipeline: p}

	smaShort := 100.5
	smaLong := 99.0
	rsi := 28.0
	ev := entity.IndicatorEvent{
		SymbolID:  10,
		Interval:  "PT15M",
		LastPrice: 8500,
		Timestamp: 1_700_000_000_000,
		Primary: entity.IndicatorSet{
			SymbolID:  10,
			SMAShort:  &smaShort,
			SMALong:   &smaLong,
			RSI:       &rsi,
			Timestamp: 1_700_000_000_000,
		},
	}
	if _, err := tap.Handle(context.Background(), ev); err != nil {
		t.Fatalf("tap handle: %v", err)
	}

	got := p.currentStance(context.Background())
	if got != string(entity.MarketStanceContrarian) {
		t.Errorf("currentStance = %q, want %q", got, entity.MarketStanceContrarian)
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver.Resolve calls = %d, want 1", resolver.calls)
	}
	if resolver.gotIndCount != 1 {
		t.Errorf("resolver did not receive the cached IndicatorSet (SMAShort missing)")
	}
	if resolver.gotPrice != 8500 {
		t.Errorf("resolver got lastPrice %v, want 8500", resolver.gotPrice)
	}
}

// TestIndicatorEventTap_IgnoresNonIndicatorEvents guards the tap from
// accidentally caching unrelated event types, which would silently feed
// stale data into stance resolution if someone widened the registration.
func TestIndicatorEventTap_IgnoresNonIndicatorEvents(t *testing.T) {
	p := &EventDrivenPipeline{}
	tap := &indicatorEventTap{pipeline: p}

	if _, err := tap.Handle(context.Background(), entity.TickEvent{SymbolID: 10, Price: 8500}); err != nil {
		t.Fatalf("tap handle tick: %v", err)
	}
	if p.hasLatestIndicators {
		t.Errorf("hasLatestIndicators = true after a TickEvent, want false")
	}
}
