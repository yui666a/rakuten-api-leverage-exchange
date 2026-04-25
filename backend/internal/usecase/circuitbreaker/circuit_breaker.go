// Package circuitbreaker stops live trading automatically when the venue
// market data drifts outside known-safe envelopes.
//
// Four detectors are bundled into one Watcher and fired off the same WS
// event stream:
//
//   - AbnormalSpread   spread/mid > threshold for N seconds
//   - PriceJump        max-min last in last 60s exceeds threshold
//   - BookFeedStale    no orderbook for > StaleAfterMs
//   - EmptyBook        BestBid == 0 || BestAsk == 0 for N seconds
//
// Any trip halts trading via Halter.HaltAutomatic and publishes a
// "risk_event" with kind "circuit_breaker". Trading does NOT auto-resume —
// the user must hit POST /api/v1/start to re-enable it. This is by design:
// the conditions a circuit breaker fires on are exactly the ones where a
// human should glance at the venue before letting the bot trade again.
package circuitbreaker

import (
	"fmt"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// Halter is the narrow port the watcher uses to actually stop trading.
// RiskManager satisfies it; tests pass an in-memory fake.
type Halter interface {
	HaltAutomatic(reason string) bool
}

// Publisher is an optional channel for surfacing trip events to operators.
// Nil disables publishing.
type Publisher interface {
	PublishCircuitBreaker(reason string, detail string, ts int64)
}

// Config bundles the four detector thresholds. Zero values disable the
// corresponding check, so callers can opt in piecemeal.
type Config struct {
	// AbnormalSpreadPct rejects when (BestAsk - BestBid) / mid > pct/100
	// for AbnormalSpreadHoldMs continuous milliseconds. 0 disables. 1.5 = 1.5%.
	AbnormalSpreadPct    float64
	AbnormalSpreadHoldMs int64

	// PriceJumpPct fires when (max - min) / mid in the rolling
	// PriceJumpWindowMs (default 60_000) exceeds pct/100. 0 disables. 3.0 = 3%.
	PriceJumpPct      float64
	PriceJumpWindowMs int64

	// BookFeedStaleAfterMs trips when no orderbook has been observed for
	// this long. 0 disables. 90_000 = 90 s.
	BookFeedStaleAfterMs int64

	// EmptyBookHoldMs trips when one side of the book has 0 best price for
	// this long continuously. 0 disables.
	EmptyBookHoldMs int64
}

// DefaultConfig returns conservative thresholds proven on Rakuten Wallet
// LTC/JPY: ~0.4% normal spread, ~1% normal 1m range, occasional WS gaps
// up to ~30 s. The defaults give one full envelope of slack on each axis.
func DefaultConfig() Config {
	return Config{
		AbnormalSpreadPct:    1.5,
		AbnormalSpreadHoldMs: 5_000,
		PriceJumpPct:         3.0,
		PriceJumpWindowMs:    60_000,
		BookFeedStaleAfterMs: 90_000,
		EmptyBookHoldMs:      5_000,
	}
}

type priceObservation struct {
	timestamp int64
	last      float64
}

// Watcher is the per-symbol state machine. One instance is shared across
// all WS event handlers for a single live pipeline.
type Watcher struct {
	cfg       Config
	halter    Halter
	publisher Publisher
	now       func() int64 // override for tests

	mu                sync.Mutex
	tripped           bool
	lastBookTs        int64
	abnormalSpreadAt  int64 // 0 = not currently abnormal
	emptyBookAt       int64
	priceObservations []priceObservation
}

// New constructs a Watcher. halter is required (a nil halter is a runtime
// programming error and panics so it surfaces at startup, not silently).
func New(cfg Config, halter Halter, publisher Publisher) *Watcher {
	if halter == nil {
		panic("circuitbreaker.New: halter must not be nil")
	}
	if cfg.PriceJumpWindowMs <= 0 {
		cfg.PriceJumpWindowMs = 60_000
	}
	return &Watcher{
		cfg:       cfg,
		halter:    halter,
		publisher: publisher,
		now:       func() int64 { return time.Now().UnixMilli() },
	}
}

// SetClock overrides the real-time source. Tests inject a mock clock so the
// watcher does not need to sleep through hold windows.
func (w *Watcher) SetClock(now func() int64) {
	if now != nil {
		w.now = now
	}
}

// Reset re-arms the watcher so it can fire again. Called from
// EventDrivenPipeline when StartTrading flips the manual-stop back off.
func (w *Watcher) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tripped = false
	w.abnormalSpreadAt = 0
	w.emptyBookAt = 0
	w.priceObservations = w.priceObservations[:0]
}

// OnTicker processes one ticker frame. Cheap so it can run on the hot WS
// goroutine.
func (w *Watcher) OnTicker(t entity.Ticker) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tripped {
		return
	}

	if t.Last > 0 {
		w.priceObservations = append(w.priceObservations, priceObservation{timestamp: t.Timestamp, last: t.Last})
		w.evictPriceWindow(t.Timestamp - w.cfg.PriceJumpWindowMs)
		if w.cfg.PriceJumpPct > 0 && len(w.priceObservations) >= 2 {
			minV, maxV := w.priceObservations[0].last, w.priceObservations[0].last
			for _, o := range w.priceObservations {
				if o.last < minV {
					minV = o.last
				}
				if o.last > maxV {
					maxV = o.last
				}
			}
			mid := (minV + maxV) / 2
			if mid > 0 && (maxV-minV)/mid*100 > w.cfg.PriceJumpPct {
				w.tripLocked("price_jump", t.Timestamp)
				return
			}
		}
	}
}

// OnOrderbook processes one orderbook frame.
func (w *Watcher) OnOrderbook(ob entity.Orderbook) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tripped {
		return
	}
	w.lastBookTs = ob.Timestamp

	mid := ob.MidPrice
	if mid <= 0 && ob.BestAsk > 0 && ob.BestBid > 0 {
		mid = (ob.BestAsk + ob.BestBid) / 2
	}

	// Empty book detection
	if w.cfg.EmptyBookHoldMs > 0 && (ob.BestBid <= 0 || ob.BestAsk <= 0) {
		if w.emptyBookAt == 0 {
			w.emptyBookAt = ob.Timestamp
		} else if ob.Timestamp-w.emptyBookAt >= w.cfg.EmptyBookHoldMs {
			w.tripLocked("empty_book", ob.Timestamp)
			return
		}
	} else {
		w.emptyBookAt = 0
	}

	// Abnormal-spread detection
	if w.cfg.AbnormalSpreadPct > 0 && mid > 0 && ob.BestAsk > ob.BestBid {
		spreadPct := (ob.BestAsk - ob.BestBid) / mid * 100
		if spreadPct > w.cfg.AbnormalSpreadPct {
			if w.abnormalSpreadAt == 0 {
				w.abnormalSpreadAt = ob.Timestamp
			} else if ob.Timestamp-w.abnormalSpreadAt >= w.cfg.AbnormalSpreadHoldMs {
				w.tripLocked("abnormal_spread", ob.Timestamp)
				return
			}
		} else {
			w.abnormalSpreadAt = 0
		}
	} else {
		w.abnormalSpreadAt = 0
	}
}

// CheckStale must be called by the caller's heartbeat (e.g. once per second)
// because BookFeedStale by definition cannot be detected from incoming
// frames — the absence of frames is the signal.
func (w *Watcher) CheckStale() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tripped || w.cfg.BookFeedStaleAfterMs <= 0 || w.lastBookTs == 0 {
		return
	}
	if w.now()-w.lastBookTs > w.cfg.BookFeedStaleAfterMs {
		w.tripLocked("book_feed_stale", w.now())
	}
}

// IsTripped reports whether the watcher has currently halted trading.
// Useful for status endpoints.
func (w *Watcher) IsTripped() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tripped
}

func (w *Watcher) tripLocked(reason string, ts int64) {
	w.tripped = true
	if !w.halter.HaltAutomatic("circuit_breaker:" + reason) {
		// Already halted by something else (manual /stop, daily-loss limit).
		// Still publish for observability so operators see the trigger
		// even if the bot was already parked.
	}
	if w.publisher != nil {
		w.publisher.PublishCircuitBreaker(reason, w.detailLocked(reason), ts)
	}
}

// detailLocked returns a short human-readable string for the given reason
// using the values still in scope. Caller must hold w.mu.
func (w *Watcher) detailLocked(reason string) string {
	switch reason {
	case "price_jump":
		if len(w.priceObservations) >= 2 {
			minV, maxV := w.priceObservations[0].last, w.priceObservations[0].last
			for _, o := range w.priceObservations {
				if o.last < minV {
					minV = o.last
				}
				if o.last > maxV {
					maxV = o.last
				}
			}
			return fmt.Sprintf("range=%.2f", maxV-minV)
		}
	}
	return ""
}

func (w *Watcher) evictPriceWindow(cutoff int64) {
	idx := 0
	for ; idx < len(w.priceObservations); idx++ {
		if w.priceObservations[idx].timestamp >= cutoff {
			break
		}
	}
	if idx > 0 {
		w.priceObservations = append(w.priceObservations[:0], w.priceObservations[idx:]...)
	}
}
