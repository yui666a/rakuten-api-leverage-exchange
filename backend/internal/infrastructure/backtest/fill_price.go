package backtest

import (
	"context"
	"fmt"
	"sort"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// FillKind distinguishes how a fill price is being computed.
//   - FillKindEntry  : opening a fresh position; signal-side is the trader's side.
//   - FillKindExit   : closing an existing position; the actual market side is opposite.
//
// The simulator decides which book side to consume based on FillKind + the
// trader-facing OrderSide:
//   - entry BUY  / exit SELL  → consume asks (taker buying)
//   - entry SELL / exit BUY   → consume bids (taker selling)
type FillKind int

const (
	FillKindEntry FillKind = iota
	FillKindExit
)

// ThinBookError signals that the orderbook side did not have enough depth to
// fill the requested size. The runner converts this into a "thin_book" trade
// skip so the strategy is not credited with an impossible fill.
type ThinBookError struct {
	Reason string
}

func (e *ThinBookError) Error() string {
	if e.Reason == "" {
		return "thin book"
	}
	return "thin book: " + e.Reason
}

// FillPriceSource computes the realised fill price for a given trade intent.
// Implementations may inspect side, signalPrice, amount, and timestamp.
//
// Returning a *ThinBookError tells the simulator to skip the trade entirely.
// Any other error aborts the backtest run.
type FillPriceSource interface {
	FillPrice(kind FillKind, side entity.OrderSide, signalPrice, amount float64, timestamp int64) (float64, error)
}

// LegacyPercentSlippage reproduces the historical "spread% / 2 + slippage%"
// adjustment so existing backtest invocations stay bit-identical.
type LegacyPercentSlippage struct {
	SpreadPercent   float64
	SlippagePercent float64
}

func (l LegacyPercentSlippage) FillPrice(kind FillKind, side entity.OrderSide, signalPrice, amount float64, _ int64) (float64, error) {
	_ = kind
	_ = amount
	adjust := (l.SpreadPercent/100.0)/2.0 + (l.SlippagePercent / 100.0)
	if isSellLike(kind, side) {
		return signalPrice * (1 - adjust), nil
	}
	return signalPrice * (1 + adjust), nil
}

// isSellLike collapses (kind, side) into "are we hitting the bid?" — true when
// the trader is reducing a long (entry SELL or exit BUY-position).
func isSellLike(kind FillKind, side entity.OrderSide) bool {
	switch kind {
	case FillKindEntry:
		return side == entity.OrderSideSell
	case FillKindExit:
		// Exit closes a long-side position via SELL. The simulator stores the
		// position's original side in `side`, not the close-order side, so a
		// long position (Side=BUY) needs a sell to close → hit the bid.
		return side == entity.OrderSideBuy
	}
	return false
}

// OrderbookReplay computes a VWAP fill price by walking the persisted
// orderbook snapshot whose timestamp is the most recent entry at or before
// the trade timestamp.
//
// Snapshots older than StaleAfter (millis) are treated as missing — that
// trade gets skipped via ThinBookError so the strategy is not credited with
// a stale-book fill.
//
// The implementation eagerly loads the full snapshot range at construction
// time and binary-searches by timestamp. For 1.5 M rows this is cheap (a few
// hundred MB if the caller asks for it; typical 14-day backtests load far
// less). Callers needing streaming access can implement FillPriceSource
// directly.
type OrderbookReplay struct {
	snapshots []entity.Orderbook // ascending by Timestamp
	stale     int64              // millis
}

// NewOrderbookReplay builds the replayer from a chronologically sorted slice.
// staleAfterMillis bounds how old a snapshot may be relative to a trade ts.
// The default in the runner is 60_000 (60 seconds).
func NewOrderbookReplay(snapshots []entity.Orderbook, staleAfterMillis int64) *OrderbookReplay {
	// Defensive copy + sort so callers cannot mutate our state and we tolerate
	// repos that don't promise ordering.
	cp := make([]entity.Orderbook, len(snapshots))
	copy(cp, snapshots)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Timestamp < cp[j].Timestamp })
	return &OrderbookReplay{snapshots: cp, stale: staleAfterMillis}
}

// SnapshotCount exposes the number of snapshots loaded; the runner uses this
// for the pre-flight coverage check.
func (o *OrderbookReplay) SnapshotCount() int { return len(o.snapshots) }

// LatestBefore implements booklimit.BookSource: it returns the snapshot whose
// timestamp is the most recent at or before ts and within the stale window.
// Same lookup primitive used by FillPrice — exposed so the pre-trade gate can
// share one OrderbookReplay instance with the simulator.
func (o *OrderbookReplay) LatestBefore(_ context.Context, _ int64, ts int64) (entity.Orderbook, bool, error) {
	snap, ok := o.lookup(ts)
	return snap, ok, nil
}

// FillPrice picks the most recent snapshot at or before ts and walks the
// appropriate side until the requested amount is filled. ThinBookError is
// returned when (a) no snapshot is in range, or (b) the side is too thin.
func (o *OrderbookReplay) FillPrice(kind FillKind, side entity.OrderSide, signalPrice, amount float64, ts int64) (float64, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("amount must be positive")
	}
	snap, ok := o.lookup(ts)
	if !ok {
		return 0, &ThinBookError{Reason: "no snapshot within stale window"}
	}

	hitAsk := !isSellLike(kind, side) // BUY hits asks
	var levels []entity.OrderbookEntry
	if hitAsk {
		levels = snap.Asks
	} else {
		levels = snap.Bids
	}

	if len(levels) == 0 {
		return 0, &ThinBookError{Reason: "empty book side"}
	}

	// Walk levels in price-priority order. Persisted snapshots store top-of-book
	// first by venue convention but we sort defensively to be deterministic.
	sorted := make([]entity.OrderbookEntry, len(levels))
	copy(sorted, levels)
	if hitAsk {
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Price < sorted[j].Price })
	} else {
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Price > sorted[j].Price })
	}

	remaining := amount
	cost := 0.0
	for _, lvl := range sorted {
		if lvl.Amount <= 0 {
			continue
		}
		take := lvl.Amount
		if take > remaining {
			take = remaining
		}
		cost += lvl.Price * take
		remaining -= take
		if remaining <= 0 {
			break
		}
	}
	if remaining > 0 {
		return 0, &ThinBookError{Reason: "insufficient depth"}
	}

	_ = signalPrice // signalPrice is unused — VWAP from the book is the real fill.
	return cost / amount, nil
}

// lookup returns the snapshot with timestamp <= ts and within stale window,
// or (zero, false) when no snapshot qualifies.
func (o *OrderbookReplay) lookup(ts int64) (entity.Orderbook, bool) {
	if len(o.snapshots) == 0 {
		return entity.Orderbook{}, false
	}
	// sort.Search finds the first index whose timestamp > ts; we want the one
	// just before that.
	idx := sort.Search(len(o.snapshots), func(i int) bool {
		return o.snapshots[i].Timestamp > ts
	})
	if idx == 0 {
		return entity.Orderbook{}, false
	}
	candidate := o.snapshots[idx-1]
	if o.stale > 0 && ts-candidate.Timestamp > o.stale {
		return entity.Orderbook{}, false
	}
	return candidate, true
}

// OrderbookHistoryLoader is the minimal repo surface OrderbookReplay needs.
// Defined here (rather than imported from domain/repository) so this file
// stays a leaf with no usecase-side imports.
type OrderbookHistoryLoader interface {
	GetOrderbookHistory(ctx context.Context, symbolID int64, from, to int64, limit int) ([]entity.Orderbook, error)
}

// LoadOrderbookReplay is a convenience helper for the runner: load the entire
// snapshot range for a symbol/window and wrap it in an OrderbookReplay.
//
// The hard cap of 1_000_000 rows is a safety net for very long windows; the
// runner pre-checks coverage and bails out with a clear error before reaching
// it.
func LoadOrderbookReplay(ctx context.Context, repo OrderbookHistoryLoader, symbolID, from, to int64, staleAfterMillis int64) (*OrderbookReplay, error) {
	snaps, err := repo.GetOrderbookHistory(ctx, symbolID, from, to, 1_000_000)
	if err != nil {
		return nil, fmt.Errorf("load orderbook history: %w", err)
	}
	return NewOrderbookReplay(snaps, staleAfterMillis), nil
}
