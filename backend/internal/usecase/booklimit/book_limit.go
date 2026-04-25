// Package booklimit provides a pre-trade orderbook depth gate shared by the
// backtest runner and the live pipeline.
//
// The gate runs after a signal has been risk-approved and before the executor
// fills it: it inspects the most recent orderbook snapshot for the symbol,
// computes the implied VWAP for the proposed lot, and rejects the trade when
// either of two thresholds is breached:
//
//   - Expected slippage > MaxSlippageBps (relative to mid price)
//   - Lot exceeds MaxBookSidePct of the cumulative top-N levels on the
//     side being hit
//
// Backtest and live pipelines share the same Gate instance via the BookSource
// abstraction so the same rejection logic governs both worlds.
package booklimit

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// Decision captures the outcome of one pre-trade check.
type Decision struct {
	Allow  bool
	Reason string // empty when Allow is true
	// SlippageBps and BookFillRatio are populated for observability even when
	// the trade is allowed. SlippageBps is signed: positive when the fill is
	// worse than mid (the typical case for taker fills).
	SlippageBps   float64
	BookFillRatio float64 // requestedAmount / topNCumulativeAmount on the side being hit
}

// Config controls the thresholds. Zero values disable the corresponding
// check, which is what the runner uses when the caller has not opted in.
type Config struct {
	// MaxSlippageBps rejects trades whose VWAP would deviate from mid by
	// more than this many basis points. 0 disables the check. 50 = 0.5%.
	MaxSlippageBps float64
	// MaxBookSidePct rejects trades whose lot is more than this percentage
	// of the cumulative top-N levels on the side being hit. 0 disables.
	// 30 = lot must be <= 30% of top-N depth.
	MaxBookSidePct float64
	// TopN is how many levels feed the cumulative-depth ratio. 0 falls back
	// to DefaultTopN (5) so callers don't have to remember a magic number.
	TopN int
	// StaleAfterMillis bounds how old a snapshot may be relative to the
	// trade timestamp. 0 disables the staleness check (live mode treats a
	// missing-but-recent book as "no opinion" — see GateBehaviour below).
	StaleAfterMillis int64
	// AllowOnMissingBook controls what happens when the BookSource has no
	// snapshot in range:
	//   - true  (live default): treat as "no opinion" → trade is allowed.
	//   - false (backtest):     reject with reason "no_book".
	AllowOnMissingBook bool
}

// DefaultTopN is the fallback level count for the cumulative-depth ratio.
const DefaultTopN = 5

// DefaultConfig returns the values the live pipeline uses when no override
// is supplied.
func DefaultConfig() Config {
	return Config{
		MaxSlippageBps:     50, // 0.5%
		MaxBookSidePct:     30, // lot <= 30% of top-5 cumulative depth
		TopN:               DefaultTopN,
		StaleAfterMillis:   60_000,
		AllowOnMissingBook: true,
	}
}

// BookSource is the minimal port the gate needs to look up the latest snapshot
// at or before a given timestamp. Live wires a per-symbol cache here;
// backtest wires the same eager-loaded slice used by the orderbook-replay
// FillPriceSource.
type BookSource interface {
	LatestBefore(ctx context.Context, symbolID, ts int64) (entity.Orderbook, bool, error)
}

// Gate is the runtime object: a Config + BookSource pair.
type Gate struct {
	Source BookSource
	Cfg    Config
}

// New constructs a Gate. Either argument may be nil — a nil Source short-
// circuits Check to "allow" so callers can wire the gate unconditionally
// and disable it by leaving Source nil during early dev.
func New(source BookSource, cfg Config) *Gate {
	if cfg.TopN <= 0 {
		cfg.TopN = DefaultTopN
	}
	return &Gate{Source: source, Cfg: cfg}
}

// Check evaluates a proposed trade. The gate is intentionally side-aware:
//   - BUY hits asks (the trader is the taker, lifting offers)
//   - SELL hits bids
func (g *Gate) Check(ctx context.Context, symbolID int64, side entity.OrderSide, amount float64, tsMillis int64) Decision {
	// No source wired → skip the gate entirely (used by tests and the
	// initial CLI-only path before live cache is available).
	if g == nil || g.Source == nil {
		return Decision{Allow: true}
	}
	if amount <= 0 {
		return Decision{Allow: true}
	}

	snap, found, err := g.Source.LatestBefore(ctx, symbolID, tsMillis)
	if err != nil || !found {
		if g.Cfg.AllowOnMissingBook {
			return Decision{Allow: true, Reason: "no_book_pass"}
		}
		return Decision{Allow: false, Reason: "no_book"}
	}
	if g.Cfg.StaleAfterMillis > 0 && tsMillis-snap.Timestamp > g.Cfg.StaleAfterMillis {
		if g.Cfg.AllowOnMissingBook {
			return Decision{Allow: true, Reason: "stale_book_pass"}
		}
		return Decision{Allow: false, Reason: "stale_book"}
	}

	// Pick the side being hit.
	var levels []entity.OrderbookEntry
	if side == entity.OrderSideBuy {
		levels = snap.Asks
	} else {
		levels = snap.Bids
	}
	if len(levels) == 0 {
		return Decision{Allow: false, Reason: "empty_book_side"}
	}

	mid := snap.MidPrice
	if mid <= 0 {
		// Fall back to (bestAsk+bestBid)/2 when the venue did not supply mid.
		if snap.BestAsk > 0 && snap.BestBid > 0 {
			mid = (snap.BestAsk + snap.BestBid) / 2
		}
	}

	vwap, depth := walkLevels(levels, amount, side)

	// Book-fill ratio uses cumulative top-N depth, NOT the depth actually
	// consumed by the lot — we want to reject "lot would soak too much of
	// the visible book" even if the lot itself fits in the very top level.
	topN := topNCumAmount(levels, g.Cfg.TopN)
	var fillRatio float64
	if topN > 0 {
		fillRatio = amount / topN
	}

	if depth < amount {
		// Lot did not fully fit in the visible book at all; this is more
		// severe than the soft "ratio" check above and we always reject it
		// regardless of MaxBookSidePct. The orderbook-replay simulator
		// surfaces the same condition as ThinBookError; here we catch it
		// before the order is even placed.
		return Decision{
			Allow:         false,
			Reason:        "thin_book_pre_trade",
			BookFillRatio: fillRatio,
		}
	}

	slipBps := 0.0
	if mid > 0 {
		// Sign the slippage so taker fills come out positive.
		if side == entity.OrderSideBuy {
			slipBps = (vwap - mid) / mid * 10000
		} else {
			slipBps = (mid - vwap) / mid * 10000
		}
	}

	if g.Cfg.MaxBookSidePct > 0 && fillRatio*100 > g.Cfg.MaxBookSidePct {
		return Decision{
			Allow:         false,
			Reason:        "lot_exceeds_book_side_ratio",
			SlippageBps:   slipBps,
			BookFillRatio: fillRatio,
		}
	}
	if g.Cfg.MaxSlippageBps > 0 && slipBps > g.Cfg.MaxSlippageBps {
		return Decision{
			Allow:         false,
			Reason:        "slippage_exceeds_threshold",
			SlippageBps:   slipBps,
			BookFillRatio: fillRatio,
		}
	}
	return Decision{
		Allow:         true,
		SlippageBps:   slipBps,
		BookFillRatio: fillRatio,
	}
}

// walkLevels computes the VWAP for filling `amount` against the given side.
// Returns (vwap, totalDepth). When totalDepth < amount, vwap is the average
// over what was filled (caller should reject via the depth comparison rather
// than relying on vwap).
func walkLevels(levels []entity.OrderbookEntry, amount float64, side entity.OrderSide) (vwap float64, totalDepth float64) {
	sortLevels(levels, side)
	remaining := amount
	cost := 0.0
	filled := 0.0
	depth := 0.0
	for _, lvl := range levels {
		if lvl.Amount <= 0 {
			continue
		}
		depth += lvl.Amount
		take := lvl.Amount
		if take > remaining {
			take = remaining
		}
		cost += lvl.Price * take
		filled += take
		remaining -= take
	}
	if filled <= 0 {
		return 0, depth
	}
	return cost / filled, depth
}

// sortLevels orders by best-price-first. The snapshot persisted by the venue
// is already in this order, but we sort defensively so callers can pass
// reshuffled slices (e.g. tests).
func sortLevels(levels []entity.OrderbookEntry, side entity.OrderSide) {
	if len(levels) <= 1 {
		return
	}
	if side == entity.OrderSideBuy {
		// Asks: ascending price.
		for i := 1; i < len(levels); i++ {
			for j := i; j > 0 && levels[j-1].Price > levels[j].Price; j-- {
				levels[j-1], levels[j] = levels[j], levels[j-1]
			}
		}
	} else {
		// Bids: descending price.
		for i := 1; i < len(levels); i++ {
			for j := i; j > 0 && levels[j-1].Price < levels[j].Price; j-- {
				levels[j-1], levels[j] = levels[j], levels[j-1]
			}
		}
	}
}

func topNCumAmount(levels []entity.OrderbookEntry, n int) float64 {
	if n <= 0 || len(levels) == 0 {
		return 0
	}
	if n > len(levels) {
		n = len(levels)
	}
	sum := 0.0
	for i := 0; i < n; i++ {
		if levels[i].Amount > 0 {
			sum += levels[i].Amount
		}
	}
	return sum
}

