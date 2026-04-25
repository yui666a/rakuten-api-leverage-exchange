// Package quality builds the execution-quality report consumed by the live
// dashboard. It joins the venue's my-trades feed with the locally persisted
// ticker history to compute slippage relative to the contemporaneous mid
// price, and surfaces the SOR / book-gate / circuit-breaker counters that
// have accumulated since the last window boundary.
package quality

import (
	"context"
	"fmt"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// VenueClient is the narrow surface the reporter needs.
type VenueClient interface {
	GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error)
}

// HaltSource exposes whether trading is currently halted and why. RiskManager
// satisfies it through its existing GetStatus / HaltReason pair, but we
// declare a port here so tests don't need a real RiskManager.
type HaltSource interface {
	GetStatus() haltStatus
}

// haltStatus is the small portion of RiskStatus the reporter needs.
type haltStatus struct {
	Halted     bool
	HaltReason string
}

// HaltStatusFunc is a convenience adapter so the composition root can pass
// a closure (e.g. wrapping RiskManager.GetStatus) without defining a new
// type.
type HaltStatusFunc func() (halted bool, reason string)

func (f HaltStatusFunc) GetStatus() haltStatus {
	if f == nil {
		return haltStatus{}
	}
	h, r := f()
	return haltStatus{Halted: h, HaltReason: r}
}

// Reporter composes everything together. Fields are set once at construction
// and the Build call is goroutine-safe (every Build does its own queries).
type Reporter struct {
	venue   VenueClient
	repo    repository.MarketDataRepository
	halts   HaltSource
	now     func() time.Time
}

// New wires the reporter. Any of venue / repo / halts may be nil; the Build
// path degrades gracefully (e.g. nil repo skips slippage computation).
func New(venue VenueClient, repo repository.MarketDataRepository, halts HaltSource) *Reporter {
	return &Reporter{venue: venue, repo: repo, halts: halts, now: time.Now}
}

// SetClock overrides the now() source. Tests inject a fixed clock.
func (r *Reporter) SetClock(now func() time.Time) {
	if now != nil {
		r.now = now
	}
}

// Build assembles the report for the last windowSec seconds against symbolID.
// windowSec <= 0 falls back to 86_400 (24 h).
func (r *Reporter) Build(ctx context.Context, symbolID int64, windowSec int64) (entity.ExecutionQualityReport, error) {
	if windowSec <= 0 {
		windowSec = 86_400
	}
	now := r.now()
	to := now.UnixMilli()
	from := now.Add(-time.Duration(windowSec) * time.Second).UnixMilli()

	rep := entity.ExecutionQualityReport{
		WindowSec: windowSec,
		From:      from,
		To:        to,
	}

	// Halt status is independent of the venue / repo paths so it always lands
	// even when the trade history fetch fails.
	if r.halts != nil {
		s := r.halts.GetStatus()
		rep.CircuitBreaker.Halted = s.Halted
		rep.CircuitBreaker.HaltReason = s.HaltReason
	}

	if r.venue == nil {
		return rep, nil
	}
	trades, err := r.venue.GetMyTrades(ctx, symbolID)
	if err != nil {
		return rep, fmt.Errorf("GetMyTrades: %w", err)
	}

	// Filter by window — the venue does not always honour from/to, so we do
	// it client-side. Trades arrive newest-first per docs but we don't rely
	// on that; just iterate and check.
	filtered := trades[:0]
	for _, t := range trades {
		if t.CreatedAt < from || t.CreatedAt > to {
			continue
		}
		filtered = append(filtered, t)
	}

	tickerLookup := r.buildTickerLookup(ctx, symbolID, filtered)
	rep.Trades = aggregateTrades(filtered, tickerLookup)
	return rep, nil
}

// buildTickerLookup queries the ticker history once per Build (range = the
// report window) and exposes a closure that returns the mid for a given ts
// using binary-search-ish nearest-before semantics.
func (r *Reporter) buildTickerLookup(ctx context.Context, symbolID int64, trades []entity.MyTrade) func(ts int64) (mid float64, ok bool) {
	if r.repo == nil || len(trades) == 0 {
		return func(int64) (float64, bool) { return 0, false }
	}
	// Pad the range by 1 minute on each side so the nearest-before lookup
	// always has a candidate even for the very first / last trade in the
	// window.
	minTs, maxTs := trades[0].CreatedAt, trades[0].CreatedAt
	for _, t := range trades[1:] {
		if t.CreatedAt < minTs {
			minTs = t.CreatedAt
		}
		if t.CreatedAt > maxTs {
			maxTs = t.CreatedAt
		}
	}
	tickers, err := r.repo.GetTickersBetween(ctx, symbolID, minTs-60_000, maxTs+60_000, 0)
	if err != nil || len(tickers) == 0 {
		return func(int64) (float64, bool) { return 0, false }
	}
	// Tickers are returned ascending by timestamp by repo contract.
	return func(ts int64) (float64, bool) {
		// Linear nearest-before — N is small (one row per ticker throttle
		// interval over the window, e.g. 86_400 at 1 Hz worst case) but we
		// only call this once per trade so total work is O(trades * tickers).
		// For a 24 h window with ~50 trades that's negligible.
		var pick *entity.Ticker
		for i := range tickers {
			if tickers[i].Timestamp > ts {
				break
			}
			pick = &tickers[i]
		}
		if pick == nil {
			return 0, false
		}
		mid := (pick.BestBid + pick.BestAsk) / 2
		if mid <= 0 {
			return 0, false
		}
		return mid, true
	}
}

func aggregateTrades(trades []entity.MyTrade, midOf func(ts int64) (float64, bool)) entity.ExecutionQualityTrades {
	out := entity.ExecutionQualityTrades{
		ByOrderBehavior: make(map[string]entity.ExecutionQualityBehaviorBucket),
	}
	if len(trades) == 0 {
		return out
	}

	slipSum, slipN := 0.0, 0
	for _, t := range trades {
		out.Count++
		fee := float64(t.Fee)
		out.TotalFeeJPY += fee

		switch t.TradeAction {
		case "MAKER":
			out.MakerCount++
		case "TAKER":
			out.TakerCount++
		default:
			out.UnknownCount++
		}

		// Per-OrderBehavior bucket.
		key := string(t.OrderBehavior)
		if key == "" {
			key = "UNKNOWN"
		}
		b := out.ByOrderBehavior[key]
		b.Count++
		b.FeeJPY += fee
		if t.TradeAction == "MAKER" {
			b.MakerCount++
		}
		out.ByOrderBehavior[key] = b

		// Slippage (in bps, signed).
		if mid, ok := midOf(t.CreatedAt); ok && mid > 0 {
			price := float64(t.Price)
			if price > 0 {
				bps := (price - mid) / mid * 10000
				if t.OrderSide == entity.OrderSideSell {
					bps = -bps // SELL above mid is a *win*; flip sign so + = bad fill.
				}
				slipSum += bps
				slipN++
			}
		}
	}

	if out.Count > 0 {
		out.MakerRatio = float64(out.MakerCount) / float64(out.Count)
	}
	for k, b := range out.ByOrderBehavior {
		if b.Count > 0 {
			b.MakerRatio = float64(b.MakerCount) / float64(b.Count)
		}
		out.ByOrderBehavior[k] = b
	}
	if slipN > 0 {
		avg := slipSum / float64(slipN)
		out.AvgSlippageBps = &avg
	}
	return out
}
