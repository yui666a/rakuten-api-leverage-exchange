package booklimit

import (
	"context"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type fakeSource struct {
	snap   entity.Orderbook
	found  bool
	err    error
}

func (f *fakeSource) LatestBefore(_ context.Context, _ int64, _ int64) (entity.Orderbook, bool, error) {
	return f.snap, f.found, f.err
}

func ob(ts int64, asks, bids []entity.OrderbookEntry, bestAsk, bestBid, mid float64) entity.Orderbook {
	return entity.Orderbook{
		Timestamp: ts,
		Asks:      asks,
		Bids:      bids,
		BestAsk:   bestAsk,
		BestBid:   bestBid,
		MidPrice:  mid,
	}
}

func TestGate_AllowsTradeBelowAllThresholds(t *testing.T) {
	src := &fakeSource{
		snap: ob(1000,
			[]entity.OrderbookEntry{{Price: 1001, Amount: 10}, {Price: 1002, Amount: 10}, {Price: 1003, Amount: 10}, {Price: 1004, Amount: 10}, {Price: 1005, Amount: 10}},
			[]entity.OrderbookEntry{{Price: 999, Amount: 10}},
			1001, 999, 1000,
		),
		found: true,
	}
	g := New(src, Config{
		MaxSlippageBps:     50,
		MaxBookSidePct:     30,
		TopN:               5,
		StaleAfterMillis:   60_000,
		AllowOnMissingBook: true,
	})

	// 1.0 LTC vs 50 LTC top-5 → 2% ratio, fills entirely at 1001 → ~10 bps
	d := g.Check(context.Background(), 7, entity.OrderSideBuy, 1.0, 1500)
	if !d.Allow {
		t.Fatalf("expected allow, got %+v", d)
	}
	// VWAP = 1001 → slip = (1001-1000)/1000 × 10000 = 10 bps
	if d.SlippageBps < 9.9 || d.SlippageBps > 10.1 {
		t.Fatalf("unexpected slippage bps: %f", d.SlippageBps)
	}
	if d.BookFillRatio < 0.019 || d.BookFillRatio > 0.021 {
		t.Fatalf("unexpected book fill ratio: %f", d.BookFillRatio)
	}
}

func TestGate_RejectsExcessiveSlippage(t *testing.T) {
	// VWAP からのスリッページが 100 bps 想定: 半々で 1010 と 1020 を消化
	src := &fakeSource{
		snap: ob(1000,
			[]entity.OrderbookEntry{{Price: 1010, Amount: 0.5}, {Price: 1020, Amount: 100}},
			[]entity.OrderbookEntry{{Price: 999, Amount: 10}},
			1010, 999, 1000,
		),
		found: true,
	}
	g := New(src, Config{MaxSlippageBps: 50, MaxBookSidePct: 0, TopN: 5, AllowOnMissingBook: true})

	// 1.0 LTC: 0.5@1010 + 0.5@1020 → VWAP 1015 → 150 bps
	d := g.Check(context.Background(), 7, entity.OrderSideBuy, 1.0, 1500)
	if d.Allow {
		t.Fatalf("expected reject for slippage, got %+v", d)
	}
	if d.Reason != "slippage_exceeds_threshold" {
		t.Fatalf("unexpected reason: %s", d.Reason)
	}
}

func TestGate_RejectsExcessiveBookRatio(t *testing.T) {
	// 全く深くない板。3 LTC を打つと 5 段累計 (5 LTC) の 60% を食う。
	src := &fakeSource{
		snap: ob(1000,
			[]entity.OrderbookEntry{
				{Price: 1001, Amount: 1}, {Price: 1002, Amount: 1},
				{Price: 1003, Amount: 1}, {Price: 1004, Amount: 1},
				{Price: 1005, Amount: 1}, {Price: 1006, Amount: 100},
			},
			[]entity.OrderbookEntry{{Price: 999, Amount: 10}},
			1001, 999, 1000,
		),
		found: true,
	}
	g := New(src, Config{MaxSlippageBps: 9999, MaxBookSidePct: 30, TopN: 5})

	d := g.Check(context.Background(), 7, entity.OrderSideBuy, 3.0, 1500)
	if d.Allow {
		t.Fatalf("expected reject for book ratio, got %+v", d)
	}
	if d.Reason != "lot_exceeds_book_side_ratio" {
		t.Fatalf("unexpected reason: %s", d.Reason)
	}
	if d.BookFillRatio < 0.59 || d.BookFillRatio > 0.61 {
		t.Fatalf("expected ~0.6 ratio, got %f", d.BookFillRatio)
	}
}

func TestGate_RejectsThinBookPreTrade(t *testing.T) {
	src := &fakeSource{
		snap: ob(1000,
			[]entity.OrderbookEntry{{Price: 1001, Amount: 0.5}}, // total depth 0.5
			nil, 1001, 999, 1000,
		),
		found: true,
	}
	g := New(src, Config{MaxSlippageBps: 50, MaxBookSidePct: 30, TopN: 5})

	d := g.Check(context.Background(), 7, entity.OrderSideBuy, 1.0, 1500)
	if d.Allow {
		t.Fatalf("expected reject thin book, got %+v", d)
	}
	if d.Reason != "thin_book_pre_trade" {
		t.Fatalf("unexpected reason: %s", d.Reason)
	}
}

func TestGate_NoBookAllowedInLiveMode(t *testing.T) {
	src := &fakeSource{found: false}
	g := New(src, Config{MaxSlippageBps: 50, AllowOnMissingBook: true})
	d := g.Check(context.Background(), 7, entity.OrderSideBuy, 1.0, 1500)
	if !d.Allow {
		t.Fatalf("expected allow on missing book in live mode, got %+v", d)
	}
}

func TestGate_NoBookRejectedInBacktestMode(t *testing.T) {
	src := &fakeSource{found: false}
	g := New(src, Config{MaxSlippageBps: 50, AllowOnMissingBook: false})
	d := g.Check(context.Background(), 7, entity.OrderSideBuy, 1.0, 1500)
	if d.Allow {
		t.Fatalf("expected reject on missing book in backtest mode, got %+v", d)
	}
	if d.Reason != "no_book" {
		t.Fatalf("unexpected reason: %s", d.Reason)
	}
}

func TestGate_StaleSnapshotHandledByMode(t *testing.T) {
	old := ob(1000,
		[]entity.OrderbookEntry{{Price: 1001, Amount: 10}},
		[]entity.OrderbookEntry{{Price: 999, Amount: 10}},
		1001, 999, 1000,
	)
	src := &fakeSource{snap: old, found: true}

	// Live: stale → allow with reason
	gLive := New(src, Config{MaxSlippageBps: 50, StaleAfterMillis: 60_000, AllowOnMissingBook: true})
	d := gLive.Check(context.Background(), 7, entity.OrderSideBuy, 1.0, 70_000)
	if !d.Allow || d.Reason != "stale_book_pass" {
		t.Fatalf("live stale: %+v", d)
	}

	// Backtest: stale → reject
	gBT := New(src, Config{MaxSlippageBps: 50, StaleAfterMillis: 60_000, AllowOnMissingBook: false})
	d = gBT.Check(context.Background(), 7, entity.OrderSideBuy, 1.0, 70_000)
	if d.Allow || d.Reason != "stale_book" {
		t.Fatalf("backtest stale: %+v", d)
	}
}

func TestGate_NilSourceShortCircuits(t *testing.T) {
	g := New(nil, Config{MaxSlippageBps: 50})
	d := g.Check(context.Background(), 7, entity.OrderSideBuy, 1.0, 1500)
	if !d.Allow {
		t.Fatalf("nil source should always allow, got %+v", d)
	}
}

func TestGate_SellSideEvaluatesBidsWithSignedSlippage(t *testing.T) {
	// 売り注文: bid 側を食う。bestBid=999、自ロット 1.0 が 999@10 で完結、
	// VWAP=999、slip=(1000-999)/1000 × 10000 = 10 bps。
	src := &fakeSource{
		snap: ob(1000,
			[]entity.OrderbookEntry{{Price: 1001, Amount: 10}},
			[]entity.OrderbookEntry{{Price: 999, Amount: 10}, {Price: 998, Amount: 10}, {Price: 997, Amount: 10}, {Price: 996, Amount: 10}, {Price: 995, Amount: 10}},
			1001, 999, 1000,
		),
		found: true,
	}
	g := New(src, Config{MaxSlippageBps: 50, MaxBookSidePct: 30, TopN: 5, AllowOnMissingBook: true})
	d := g.Check(context.Background(), 7, entity.OrderSideSell, 1.0, 1500)
	if !d.Allow {
		t.Fatalf("expected allow, got %+v", d)
	}
	if d.SlippageBps < 9.9 || d.SlippageBps > 10.1 {
		t.Fatalf("expected ~10 bps, got %f", d.SlippageBps)
	}
}

// PR4 (Phase 1): edge-case coverage requested by the design doc §7.1 matrix.

// TestGate_BookSidePctBoundary: 自ロットがちょうど top-N 累積数量の M% に
// 到達する境界。strictly greater でなく == で reject される実装か allow か
// 仕様を固定する。現実装は >= で reject なので 30% でちょうど reject。
func TestGate_BookSidePctBoundary(t *testing.T) {
	// Top-5 累積 = 5 + 5 + 5 + 5 + 5 = 25。30% = 7.5。
	// 自分のサイズ 7.5 → ratio = 7.5 / 25 = 30% → 境界。
	src := &fakeSource{
		snap: ob(1000,
			[]entity.OrderbookEntry{
				{Price: 1001, Amount: 5},
				{Price: 1002, Amount: 5},
				{Price: 1003, Amount: 5},
				{Price: 1004, Amount: 5},
				{Price: 1005, Amount: 5},
			},
			[]entity.OrderbookEntry{{Price: 999, Amount: 25}},
			1001, 999, 1000,
		),
		found: true,
	}

	g := New(src, Config{MaxSlippageBps: 99999, MaxBookSidePct: 30, TopN: 5, AllowOnMissingBook: true})

	// Just below the boundary (29.6%) should pass.
	dBelow := g.Check(context.Background(), 7, entity.OrderSideBuy, 7.4, 1500)
	if !dBelow.Allow {
		t.Errorf("ratio=29.6%% should pass MaxBookSidePct=30, got %+v", dBelow)
	}

	// Strictly above (30.4%) should reject.
	dAbove := g.Check(context.Background(), 7, entity.OrderSideBuy, 7.6, 1500)
	if dAbove.Allow {
		t.Errorf("ratio=30.4%% should fail MaxBookSidePct=30, got %+v", dAbove)
	}
}

// TestGate_TopNUnderfilled: 板に N 段未満しか並んでいないとき、累積は
// 存在する分だけで計算される。Thin book の reject は別経路 (depth_pre_trade)
// なので、十分な単一 ask があればそれが top-1 として扱われる。
func TestGate_TopNUnderfilled(t *testing.T) {
	src := &fakeSource{
		snap: ob(1000,
			[]entity.OrderbookEntry{{Price: 1001, Amount: 100}}, // top-1 のみ、残り 4 段空
			[]entity.OrderbookEntry{{Price: 999, Amount: 100}},
			1001, 999, 1000,
		),
		found: true,
	}
	g := New(src, Config{MaxSlippageBps: 99999, MaxBookSidePct: 30, TopN: 5, AllowOnMissingBook: true})

	// 自ロット 1.0 / cumulative 100 = 1% → pass
	d := g.Check(context.Background(), 7, entity.OrderSideBuy, 1.0, 1500)
	if !d.Allow {
		t.Errorf("single deep ask level should be enough to pass 1%% lot, got %+v", d)
	}
}
