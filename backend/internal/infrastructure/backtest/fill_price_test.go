package backtest

import (
	"errors"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestLegacyPercentSlippage_AdjustsBothDirections(t *testing.T) {
	src := LegacyPercentSlippage{SpreadPercent: 0.2, SlippagePercent: 0}

	// Entry BUY hits asks → upward adjust by spread/2.
	got, err := src.FillPrice(FillKindEntry, entity.OrderSideBuy, 1000, 0.1, 0)
	if err != nil {
		t.Fatalf("entry buy: %v", err)
	}
	if math.Abs(got-1001) > 1e-9 {
		t.Fatalf("entry buy fill = %f, want 1001", got)
	}

	// Entry SELL hits bids → downward adjust.
	got, err = src.FillPrice(FillKindEntry, entity.OrderSideSell, 1000, 0.1, 0)
	if err != nil {
		t.Fatalf("entry sell: %v", err)
	}
	if math.Abs(got-999) > 1e-9 {
		t.Fatalf("entry sell fill = %f, want 999", got)
	}

	// Exit on a long (Side=BUY stored on the position) → close with sell, hit bids.
	got, err = src.FillPrice(FillKindExit, entity.OrderSideBuy, 1000, 0.1, 0)
	if err != nil {
		t.Fatalf("exit long: %v", err)
	}
	if math.Abs(got-999) > 1e-9 {
		t.Fatalf("exit long fill = %f, want 999", got)
	}
}

func TestOrderbookReplay_VWAPSingleLevelExactFit(t *testing.T) {
	snap := entity.Orderbook{
		SymbolID:  7,
		Timestamp: 1000,
		Asks: []entity.OrderbookEntry{
			{Price: 9001, Amount: 1.0},
			{Price: 9002, Amount: 1.0},
		},
		Bids: []entity.OrderbookEntry{
			{Price: 8999, Amount: 1.0},
		},
	}
	r := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)

	// Entry buy 0.5 LTC → fully filled at 9001.
	got, err := r.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 0.5, 1500)
	if err != nil {
		t.Fatalf("entry buy: %v", err)
	}
	if math.Abs(got-9001) > 1e-9 {
		t.Fatalf("vwap = %f, want 9001", got)
	}
}

func TestOrderbookReplay_VWAPMultiLevelWalk(t *testing.T) {
	snap := entity.Orderbook{
		SymbolID:  7,
		Timestamp: 1000,
		Asks: []entity.OrderbookEntry{
			{Price: 100, Amount: 0.4},
			{Price: 110, Amount: 0.6},
			{Price: 120, Amount: 1.0},
		},
	}
	r := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)

	// Buy 0.8 → consumes 0.4@100 + 0.4@110 → cost 40 + 44 = 84 → vwap 105.
	got, err := r.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 0.8, 1500)
	if err != nil {
		t.Fatalf("vwap walk: %v", err)
	}
	if math.Abs(got-105) > 1e-9 {
		t.Fatalf("vwap = %f, want 105", got)
	}
}

func TestOrderbookReplay_ThinBookOnInsufficientDepth(t *testing.T) {
	snap := entity.Orderbook{
		SymbolID:  7,
		Timestamp: 1000,
		Asks:      []entity.OrderbookEntry{{Price: 100, Amount: 0.5}}, // 0.5 only
	}
	r := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)

	_, err := r.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 1500)
	var thin *ThinBookError
	if !errors.As(err, &thin) {
		t.Fatalf("expected ThinBookError, got %v", err)
	}
}

func TestOrderbookReplay_StaleSnapshotRejected(t *testing.T) {
	// Snapshot at ts=1000, lookup at ts=70_000 with stale=60_000 → rejected.
	snap := entity.Orderbook{
		SymbolID:  7,
		Timestamp: 1000,
		Asks:      []entity.OrderbookEntry{{Price: 100, Amount: 10}},
	}
	r := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)
	_, err := r.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 70_000)
	var thin *ThinBookError
	if !errors.As(err, &thin) {
		t.Fatalf("expected ThinBookError for stale snapshot, got %v", err)
	}
}

func TestOrderbookReplay_PicksMostRecentBeforeTimestamp(t *testing.T) {
	snap1 := entity.Orderbook{
		SymbolID:  7,
		Timestamp: 1000,
		Asks:      []entity.OrderbookEntry{{Price: 100, Amount: 10}},
	}
	snap2 := entity.Orderbook{
		SymbolID:  7,
		Timestamp: 5000,
		Asks:      []entity.OrderbookEntry{{Price: 200, Amount: 10}},
	}
	r := NewOrderbookReplay([]entity.Orderbook{snap1, snap2}, 60_000)

	// Lookup at ts=4000 → must pick snap1 (200 hasn't happened yet).
	got, err := r.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 4000)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if math.Abs(got-100) > 1e-9 {
		t.Fatalf("expected snap1 (100), got %f", got)
	}

	// At ts=5000 the new snap is in scope.
	got, err = r.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 5000)
	if err != nil {
		t.Fatalf("lookup2: %v", err)
	}
	if math.Abs(got-200) > 1e-9 {
		t.Fatalf("expected snap2 (200), got %f", got)
	}
}

func TestOrderbookReplay_NoSnapshotsBefore(t *testing.T) {
	snap := entity.Orderbook{Timestamp: 5000, Asks: []entity.OrderbookEntry{{Price: 100, Amount: 10}}}
	r := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)
	_, err := r.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 1000)
	var thin *ThinBookError
	if !errors.As(err, &thin) {
		t.Fatalf("expected ThinBookError when no snapshot precedes ts, got %v", err)
	}
}

func TestOrderbookReplay_EmptySideTreatedAsThinBook(t *testing.T) {
	snap := entity.Orderbook{
		Timestamp: 1000,
		Asks:      nil,
		Bids:      []entity.OrderbookEntry{{Price: 99, Amount: 5}},
	}
	r := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)
	_, err := r.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 1500)
	var thin *ThinBookError
	if !errors.As(err, &thin) {
		t.Fatalf("expected ThinBookError on empty asks, got %v", err)
	}
}

func TestSimExecutor_DefaultsToLegacyPercentSlippage(t *testing.T) {
	// Sanity check: SimExecutor without an explicit FillPriceSource matches the
	// pre-refactor arithmetic exactly.
	sim := NewSimExecutor(SimConfig{
		InitialBalance:  100_000,
		SpreadPercent:   0.2,
		SlippagePercent: 0,
	})
	ev, err := sim.Open(7, entity.OrderSideBuy, 1000, 0.1, "test", 1000)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if math.Abs(ev.Price-1001) > 1e-9 {
		t.Fatalf("default slippage fill = %f, want 1001", ev.Price)
	}
}

func TestSimExecutor_OrderbookReplayThinBookErrorPropagates(t *testing.T) {
	// SimExecutor wired with an OrderbookReplay must surface ThinBookError to
	// the runner so the trade gets skipped (not silently filled at signal price).
	snap := entity.Orderbook{
		Timestamp: 1000,
		Asks:      []entity.OrderbookEntry{{Price: 100, Amount: 0.01}}, // thin
	}
	r := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)
	sim := NewSimExecutor(SimConfig{
		InitialBalance:  100_000,
		FillPriceSource: r,
	})
	_, err := sim.Open(7, entity.OrderSideBuy, 1000, 0.5, "thin-book", 1500)
	var thin *ThinBookError
	if !errors.As(err, &thin) {
		t.Fatalf("expected ThinBookError to propagate from SimExecutor, got %v", err)
	}
}
