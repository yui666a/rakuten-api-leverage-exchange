package circuitbreaker

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type fakeHalter struct {
	calls    []string
	firstHit bool
}

func (f *fakeHalter) HaltAutomatic(reason string) bool {
	f.calls = append(f.calls, reason)
	if !f.firstHit {
		f.firstHit = true
		return true
	}
	return false
}

type fakePublisher struct {
	events []struct {
		reason string
		detail string
		ts     int64
	}
}

func (p *fakePublisher) PublishCircuitBreaker(reason, detail string, ts int64) {
	p.events = append(p.events, struct {
		reason string
		detail string
		ts     int64
	}{reason, detail, ts})
}

func TestWatcher_AbnormalSpread_TripsAfterHoldWindow(t *testing.T) {
	halter := &fakeHalter{}
	pub := &fakePublisher{}
	w := New(Config{AbnormalSpreadPct: 1.0, AbnormalSpreadHoldMs: 5_000}, halter, pub)

	// First abnormal frame at t=1000 — starts the timer, does not yet trip.
	w.OnOrderbook(entity.Orderbook{
		Timestamp: 1000, BestBid: 99, BestAsk: 102, MidPrice: 100.5,
	})
	if w.IsTripped() {
		t.Fatal("must not trip on first abnormal frame")
	}
	// Still abnormal at t=4_000 (4 s held — not yet at 5 s threshold).
	w.OnOrderbook(entity.Orderbook{
		Timestamp: 4_000, BestBid: 99, BestAsk: 102, MidPrice: 100.5,
	})
	if w.IsTripped() {
		t.Fatal("must not trip before 5 s hold")
	}
	// At t=6_000, hold window exceeded → trip.
	w.OnOrderbook(entity.Orderbook{
		Timestamp: 6_000, BestBid: 99, BestAsk: 102, MidPrice: 100.5,
	})
	if !w.IsTripped() {
		t.Fatal("expected trip after 5 s of abnormal spread")
	}
	if len(pub.events) != 1 || pub.events[0].reason != "abnormal_spread" {
		t.Fatalf("publisher events: %+v", pub.events)
	}
	if len(halter.calls) != 1 || halter.calls[0] != "circuit_breaker:abnormal_spread" {
		t.Fatalf("halter calls: %+v", halter.calls)
	}
}

func TestWatcher_AbnormalSpread_RecoveryClearsTimer(t *testing.T) {
	halter := &fakeHalter{}
	w := New(Config{AbnormalSpreadPct: 1.0, AbnormalSpreadHoldMs: 5_000}, halter, nil)

	w.OnOrderbook(entity.Orderbook{Timestamp: 1000, BestBid: 99, BestAsk: 102, MidPrice: 100.5})
	// Spread tightens — timer must reset.
	w.OnOrderbook(entity.Orderbook{Timestamp: 2000, BestBid: 100, BestAsk: 100.4, MidPrice: 100.2})
	// Spread widens again later — needs another full hold window.
	w.OnOrderbook(entity.Orderbook{Timestamp: 6000, BestBid: 99, BestAsk: 102, MidPrice: 100.5})
	if w.IsTripped() {
		t.Fatal("must not trip when timer reset by recovery")
	}
}

func TestWatcher_PriceJump_DetectsRangeOverWindow(t *testing.T) {
	halter := &fakeHalter{}
	pub := &fakePublisher{}
	w := New(Config{PriceJumpPct: 3.0, PriceJumpWindowMs: 60_000}, halter, pub)

	// 100 → 100.5: 0.5% range, no trip.
	w.OnTicker(entity.Ticker{Timestamp: 1000, Last: 100})
	w.OnTicker(entity.Ticker{Timestamp: 2000, Last: 100.5})
	if w.IsTripped() {
		t.Fatal("0.5% range must not trip")
	}
	// 100 → 104: 4% range over 60 s window → trip.
	w.OnTicker(entity.Ticker{Timestamp: 3000, Last: 104})
	if !w.IsTripped() {
		t.Fatal("expected trip on 4% range")
	}
	if pub.events[0].reason != "price_jump" {
		t.Fatalf("expected price_jump, got %s", pub.events[0].reason)
	}
}

func TestWatcher_PriceJump_EvictsObservationsOlderThanWindow(t *testing.T) {
	halter := &fakeHalter{}
	w := New(Config{PriceJumpPct: 3.0, PriceJumpWindowMs: 1_000}, halter, nil)

	w.OnTicker(entity.Ticker{Timestamp: 1000, Last: 90})  // would be a 10% drop reference
	w.OnTicker(entity.Ticker{Timestamp: 5000, Last: 100}) // 4 s later — first ob evicted
	w.OnTicker(entity.Ticker{Timestamp: 5500, Last: 100.5})
	if w.IsTripped() {
		t.Fatal("must not trip after old observation is evicted")
	}
}

func TestWatcher_BookFeedStale_TripsAfterStaleAfter(t *testing.T) {
	halter := &fakeHalter{}
	pub := &fakePublisher{}
	w := New(Config{BookFeedStaleAfterMs: 90_000}, halter, pub)

	now := int64(10_000)
	w.SetClock(func() int64 { return now })

	w.OnOrderbook(entity.Orderbook{Timestamp: now, BestBid: 99, BestAsk: 101, MidPrice: 100})
	now = 50_000 // 40 s later — within window
	w.CheckStale()
	if w.IsTripped() {
		t.Fatal("40s gap must not trip")
	}
	now = 110_000 // 100 s after last book
	w.CheckStale()
	if !w.IsTripped() {
		t.Fatal("expected trip on 100 s book gap")
	}
	if pub.events[0].reason != "book_feed_stale" {
		t.Fatalf("expected book_feed_stale, got %s", pub.events[0].reason)
	}
}

func TestWatcher_EmptyBook_TripsAfterHold(t *testing.T) {
	halter := &fakeHalter{}
	pub := &fakePublisher{}
	w := New(Config{EmptyBookHoldMs: 5_000}, halter, pub)

	w.OnOrderbook(entity.Orderbook{Timestamp: 1000, BestBid: 0, BestAsk: 100, MidPrice: 50})
	w.OnOrderbook(entity.Orderbook{Timestamp: 4000, BestBid: 0, BestAsk: 100, MidPrice: 50})
	if w.IsTripped() {
		t.Fatal("must not trip before hold window")
	}
	w.OnOrderbook(entity.Orderbook{Timestamp: 7000, BestBid: 0, BestAsk: 100, MidPrice: 50})
	if !w.IsTripped() {
		t.Fatal("expected trip after 5s of empty bid")
	}
	if pub.events[0].reason != "empty_book" {
		t.Fatalf("expected empty_book, got %s", pub.events[0].reason)
	}
}

func TestWatcher_TripsExactlyOnce(t *testing.T) {
	halter := &fakeHalter{}
	pub := &fakePublisher{}
	w := New(Config{AbnormalSpreadPct: 1.0, AbnormalSpreadHoldMs: 5_000}, halter, pub)

	// Trigger once.
	w.OnOrderbook(entity.Orderbook{Timestamp: 1000, BestBid: 99, BestAsk: 102, MidPrice: 100.5})
	w.OnOrderbook(entity.Orderbook{Timestamp: 6000, BestBid: 99, BestAsk: 102, MidPrice: 100.5})
	if !w.IsTripped() {
		t.Fatal("first trip required")
	}
	// Subsequent abnormal frames must not produce more publish events nor halt calls.
	w.OnOrderbook(entity.Orderbook{Timestamp: 8000, BestBid: 99, BestAsk: 102, MidPrice: 100.5})
	if len(pub.events) != 1 {
		t.Fatalf("expected 1 publish event, got %d", len(pub.events))
	}
	if len(halter.calls) != 1 {
		t.Fatalf("expected 1 halter call, got %d", len(halter.calls))
	}
}

func TestWatcher_ResetReArms(t *testing.T) {
	halter := &fakeHalter{}
	w := New(Config{AbnormalSpreadPct: 1.0, AbnormalSpreadHoldMs: 1_000}, halter, nil)

	w.OnOrderbook(entity.Orderbook{Timestamp: 1000, BestBid: 99, BestAsk: 102, MidPrice: 100.5})
	w.OnOrderbook(entity.Orderbook{Timestamp: 3000, BestBid: 99, BestAsk: 102, MidPrice: 100.5})
	if !w.IsTripped() {
		t.Fatal("expected initial trip")
	}

	w.Reset()
	if w.IsTripped() {
		t.Fatal("Reset must clear tripped state")
	}
	// After reset, watcher must be re-armable.
	w.OnOrderbook(entity.Orderbook{Timestamp: 5000, BestBid: 99, BestAsk: 102, MidPrice: 100.5})
	w.OnOrderbook(entity.Orderbook{Timestamp: 7000, BestBid: 99, BestAsk: 102, MidPrice: 100.5})
	if !w.IsTripped() {
		t.Fatal("expected second trip after Reset")
	}
}

func TestWatcher_PanicsOnNilHalter(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil halter")
		}
	}()
	_ = New(Config{}, nil, nil)
}
