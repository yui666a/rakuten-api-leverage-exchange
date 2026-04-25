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

func TestPostOnlyLimitFill_OverrideMaker_FillsAtBestBidForBuy(t *testing.T) {
	snap := entity.Orderbook{
		Timestamp: 1000, BestBid: 99, BestAsk: 101,
		Asks: []entity.OrderbookEntry{{Price: 101, Amount: 10}},
		Bids: []entity.OrderbookEntry{{Price: 99, Amount: 10}},
	}
	replay := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)
	p := &PostOnlyLimitFill{
		MakerFillProbability: 1.0,
		TakerSource:          replay,
		BookSource:           replay,
		SymbolID:             7,
		SamplerOverride:      "maker",
	}
	got, err := p.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 1500)
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	if got != 99 {
		t.Fatalf("expected fill at BestBid 99, got %f", got)
	}
	if !p.LastFillWasMaker() {
		t.Fatal("expected LastFillWasMaker=true")
	}
}

func TestPostOnlyLimitFill_OverrideTaker_FallsBackToTakerSource(t *testing.T) {
	snap := entity.Orderbook{
		Timestamp: 1000, BestBid: 99, BestAsk: 101,
		Asks: []entity.OrderbookEntry{{Price: 101, Amount: 10}},
		Bids: []entity.OrderbookEntry{{Price: 99, Amount: 10}},
	}
	replay := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)
	p := &PostOnlyLimitFill{
		MakerFillProbability: 0.0,
		TakerSource:          replay,
		BookSource:           replay,
		SymbolID:             7,
		SamplerOverride:      "taker",
	}
	got, err := p.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 1500)
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	if got != 101 {
		t.Fatalf("expected taker fill at BestAsk 101, got %f", got)
	}
	if p.LastFillWasMaker() {
		t.Fatal("expected LastFillWasMaker=false on taker fallback")
	}
}

func TestPostOnlyLimitFill_DeterministicSampler(t *testing.T) {
	// Same timestamp must produce the same maker decision across runs.
	snap := entity.Orderbook{
		Timestamp: 1000, BestBid: 99, BestAsk: 101,
		Bids: []entity.OrderbookEntry{{Price: 99, Amount: 10}},
		Asks: []entity.OrderbookEntry{{Price: 101, Amount: 10}},
	}
	replay := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)
	p := &PostOnlyLimitFill{
		MakerFillProbability: 0.5,
		TakerSource:          replay,
		BookSource:           replay,
		SymbolID:             7,
	}
	first, _ := p.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 1500)
	firstMaker := p.LastFillWasMaker()
	second, _ := p.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 1500)
	secondMaker := p.LastFillWasMaker()
	if first != second || firstMaker != secondMaker {
		t.Fatalf("non-deterministic: first=(%f,%v) second=(%f,%v)", first, firstMaker, second, secondMaker)
	}
}

func TestLatencyAdjustedSource_ShiftsTimestamp(t *testing.T) {
	// Two snapshots: snap0 at ts=1000 with BestAsk=100, snap1 at ts=5000
	// with BestAsk=200. Without latency, a fill at ts=2000 picks snap0
	// (cost 100). With LatencyMs=4000 shifting to ts=6000, it picks snap1.
	snap0 := entity.Orderbook{
		Timestamp: 1000, BestAsk: 100, BestBid: 99,
		Asks: []entity.OrderbookEntry{{Price: 100, Amount: 10}},
	}
	snap1 := entity.Orderbook{
		Timestamp: 5000, BestAsk: 200, BestBid: 199,
		Asks: []entity.OrderbookEntry{{Price: 200, Amount: 10}},
	}
	replay := NewOrderbookReplay([]entity.Orderbook{snap0, snap1}, 60_000)

	// Baseline (no latency).
	baseline, err := replay.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 2000)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if baseline != 100 {
		t.Fatalf("expected baseline 100, got %f", baseline)
	}

	wrapped := &LatencyAdjustedSource{Inner: replay, LatencyMs: 4000}
	shifted, err := wrapped.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 2000)
	if err != nil {
		t.Fatalf("shifted: %v", err)
	}
	if shifted != 200 {
		t.Fatalf("expected latency-shifted 200, got %f", shifted)
	}
}

func TestLatencyAdjustedSource_PassesThroughMakerFlag(t *testing.T) {
	snap := entity.Orderbook{
		Timestamp: 1000, BestBid: 99, BestAsk: 101,
		Bids: []entity.OrderbookEntry{{Price: 99, Amount: 10}},
		Asks: []entity.OrderbookEntry{{Price: 101, Amount: 10}},
	}
	replay := NewOrderbookReplay([]entity.Orderbook{snap}, 60_000)
	post := &PostOnlyLimitFill{
		MakerFillProbability: 1.0, TakerSource: replay, BookSource: replay,
		SymbolID: 7, SamplerOverride: "maker",
	}
	wrapped := &LatencyAdjustedSource{Inner: post, LatencyMs: 0}
	if _, err := wrapped.FillPrice(FillKindEntry, entity.OrderSideBuy, 0, 1.0, 1500); err != nil {
		t.Fatalf("fill: %v", err)
	}
	if !wrapped.LastFillWasMaker() {
		t.Fatal("expected wrapper to expose maker=true from inner PostOnlyLimitFill")
	}
}
