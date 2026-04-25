package indicator

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestMicroprice_BalancedBookReturnsMid(t *testing.T) {
	ob := entity.Orderbook{
		BestBid: 100, BestAsk: 102,
		Bids: []entity.OrderbookEntry{{Price: 100, Amount: 5}},
		Asks: []entity.OrderbookEntry{{Price: 102, Amount: 5}},
	}
	got, ok := Microprice(ob)
	if !ok {
		t.Fatal("expected ok")
	}
	// (100*5 + 102*5) / 10 = 101 — same as plain mid.
	if math.Abs(got-101) > 1e-9 {
		t.Fatalf("expected 101, got %f", got)
	}
}

func TestMicroprice_BidHeavyTiltsUp(t *testing.T) {
	ob := entity.Orderbook{
		BestBid: 100, BestAsk: 102,
		Bids: []entity.OrderbookEntry{{Price: 100, Amount: 9}},
		Asks: []entity.OrderbookEntry{{Price: 102, Amount: 1}},
	}
	got, ok := Microprice(ob)
	if !ok {
		t.Fatal("ok")
	}
	// (100*1 + 102*9) / 10 = 101.8 — closer to the ask side because buyers
	// are stacked and sellers will likely move first.
	if math.Abs(got-101.8) > 1e-9 {
		t.Fatalf("expected 101.8, got %f", got)
	}
}

func TestMicroprice_AskHeavyTiltsDown(t *testing.T) {
	ob := entity.Orderbook{
		BestBid: 100, BestAsk: 102,
		Bids: []entity.OrderbookEntry{{Price: 100, Amount: 1}},
		Asks: []entity.OrderbookEntry{{Price: 102, Amount: 9}},
	}
	got, ok := Microprice(ob)
	if !ok {
		t.Fatal("ok")
	}
	if math.Abs(got-100.2) > 1e-9 {
		t.Fatalf("expected 100.2, got %f", got)
	}
}

func TestMicroprice_RejectsEmptyOrZeroSides(t *testing.T) {
	cases := []entity.Orderbook{
		{}, // zero touch
		{BestBid: 100, BestAsk: 0, Bids: []entity.OrderbookEntry{{Price: 100, Amount: 1}}},
		{BestBid: 100, BestAsk: 102, Bids: nil, Asks: nil},
	}
	for i, ob := range cases {
		if _, ok := Microprice(ob); ok {
			t.Fatalf("case %d: expected ok=false", i)
		}
	}
}

func TestTopNDepth(t *testing.T) {
	levels := []entity.OrderbookEntry{
		{Price: 100, Amount: 1}, {Price: 101, Amount: 2}, {Price: 102, Amount: 3},
	}
	if got := TopNDepth(levels, 2); math.Abs(got-3) > 1e-9 {
		t.Fatalf("top2 = %f, want 3", got)
	}
	// n > len returns full sum
	if got := TopNDepth(levels, 100); math.Abs(got-6) > 1e-9 {
		t.Fatalf("top100 = %f, want 6", got)
	}
	if got := TopNDepth(levels, 0); got != 0 {
		t.Fatalf("top0 = %f, want 0", got)
	}
}
