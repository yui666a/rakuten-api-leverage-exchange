package indicator

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func ob(ts int64, bidAmt, askAmt float64) entity.Orderbook {
	return entity.Orderbook{
		Timestamp: ts,
		Bids:      []entity.OrderbookEntry{{Price: 100, Amount: bidAmt}},
		Asks:      []entity.OrderbookEntry{{Price: 102, Amount: askAmt}},
	}
}

func TestOFI_NotReadyWithSingleSnapshot(t *testing.T) {
	c := NewOFICalculator(5, 10_000)
	c.Add(ob(0, 1, 1))
	if _, ok := c.Compute(); ok {
		t.Fatal("OFI must be unavailable until 2 snapshots accumulate")
	}
}

func TestOFI_PositiveOnBidGrowth(t *testing.T) {
	c := NewOFICalculator(5, 10_000)
	c.Add(ob(0, 1, 1))      // bid=1, ask=1, total=2
	c.Add(ob(1000, 3, 1))   // bidΔ=+2, askΔ=0 → (2-0)/2 = +1.0
	got, ok := c.Compute()
	if !ok {
		t.Fatal("expected ok")
	}
	if math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("expected +1.0, got %f", got)
	}
}

func TestOFI_NegativeOnAskGrowth(t *testing.T) {
	c := NewOFICalculator(5, 10_000)
	c.Add(ob(0, 1, 1))
	c.Add(ob(1000, 1, 3)) // (0 - 2) / 2 = -1.0
	got, _ := c.Compute()
	if math.Abs(got-(-1.0)) > 1e-9 {
		t.Fatalf("expected -1.0, got %f", got)
	}
}

func TestOFI_EvictsObservationsOlderThanWindow(t *testing.T) {
	c := NewOFICalculator(5, 10_000)
	c.Add(ob(0, 10, 10))      // depth=20 baseline
	c.Add(ob(5_000, 12, 8))   // still inside window
	c.Add(ob(15_000, 14, 6))  // 15s after first → first should be evicted
	if c.Len() != 2 {
		t.Fatalf("expected 2 obs after eviction, got %d", c.Len())
	}
	// New baseline is the (12,8) snapshot at t=5000.
	// Δbid = 14-12 = 2, Δask = 6-8 = -2 → (2 - (-2)) / (12+8) = 0.2
	got, _ := c.Compute()
	if math.Abs(got-0.2) > 1e-9 {
		t.Fatalf("expected +0.2 after eviction, got %f", got)
	}
}

func TestOFI_IgnoresOutOfOrderSnapshots(t *testing.T) {
	c := NewOFICalculator(5, 10_000)
	c.Add(ob(1000, 1, 1))
	c.Add(ob(500, 100, 0)) // older — must be dropped
	if c.Len() != 1 {
		t.Fatalf("expected 1 obs, got %d", c.Len())
	}
}

func TestOFI_ZeroDenominatorReturnsNotOK(t *testing.T) {
	c := NewOFICalculator(5, 10_000)
	c.Add(ob(0, 0, 0))      // both sides empty at window start
	c.Add(ob(1000, 1, 1))
	if _, ok := c.Compute(); ok {
		t.Fatal("expected unavailable when window-start depth is 0")
	}
}
