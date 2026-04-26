package backtest

import (
	"context"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

// atrTestExecutor is a tiny TickRiskExecutor stub that records Close calls
// and serves one pre-set position. Used only for the PR-12 wiring tests.
type atrTestExecutor struct {
	positions []eventengine.Position
	closes    []closeCall
}

type closeCall struct {
	ID     int64
	Price  float64
	Reason string
	TS     int64
}

func (e *atrTestExecutor) Positions() []eventengine.Position { return e.positions }
func (e *atrTestExecutor) SelectSLTPExit(side entity.OrderSide, slPrice, tpPrice, low, high float64) (float64, string, bool) {
	// Report hits so tests can cover SL triggers via the tick-range check.
	// Buy side: SL fires when low <= slPrice; TP when high >= tpPrice.
	if side == entity.OrderSideBuy {
		if low <= slPrice {
			return slPrice, "stop_loss", true
		}
		if high >= tpPrice {
			return tpPrice, "take_profit", true
		}
	} else {
		if high >= slPrice {
			return slPrice, "stop_loss", true
		}
		if low <= tpPrice {
			return tpPrice, "take_profit", true
		}
	}
	return 0, "", false
}
func (e *atrTestExecutor) Close(id int64, price float64, reason string, ts int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error) {
	e.closes = append(e.closes, closeCall{ID: id, Price: price, Reason: reason, TS: ts})
	return entity.OrderEvent{Timestamp: ts}, &entity.BacktestTradeRecord{}, nil
}

// TestTickRiskHandler_TrailingDistance_ATRBiggerThanPercent covers the
// "max of the two" policy: when ATR-derived distance exceeds the
// percent-derived one, the handler trails at the ATR distance instead
// of the tighter percent distance.
func TestTickRiskHandler_TrailingDistance_ATRBiggerThanPercent(t *testing.T) {
	h := NewTickRiskHandler("PT15M", &atrTestExecutor{}, 5, 10)
	h.SetATRMultipliers(0, 2.0) // ATR SL disabled, trailing ATR = 2x
	h.UpdateATR(500)            // => ATR distance = 1000

	// Entry 10_000, percent 5% => 500; ATR 2×500 = 1000 (bigger wins)
	got := h.trailingDistance(10_000)
	if got != 1000 {
		t.Fatalf("trailingDistance = %v, want 1000 (ATR-based)", got)
	}
}

func TestTickRiskHandler_TrailingDistance_PercentBiggerThanATR(t *testing.T) {
	h := NewTickRiskHandler("PT15M", &atrTestExecutor{}, 10, 10)
	h.SetATRMultipliers(0, 1.0)
	h.UpdateATR(200) // ATR distance = 200; percent 10% × 10_000 = 1000 bigger

	got := h.trailingDistance(10_000)
	if got != 1000 {
		t.Fatalf("trailingDistance = %v, want 1000 (percent-based)", got)
	}
}

func TestTickRiskHandler_TrailingDistance_ATRDisabled(t *testing.T) {
	h := NewTickRiskHandler("PT15M", &atrTestExecutor{}, 5, 10)
	// Default: both ATR multipliers = 0 (legacy percent-only path).
	h.UpdateATR(10_000) // should be ignored when multipliers are zero.
	got := h.trailingDistance(10_000)
	if got != 500 {
		t.Fatalf("trailingDistance = %v, want 500 (percent-only)", got)
	}
}

// TestTickRiskHandler_StopLossDistance_ATRBiggerThanPercent verifies the
// same "max wins" policy applies to the hard SL distance, not only to
// trailing. Before PR-12 stop_loss_atr_multiplier had no effect here.
func TestTickRiskHandler_StopLossDistance_ATRBiggerThanPercent(t *testing.T) {
	h := NewTickRiskHandler("PT15M", &atrTestExecutor{}, 3, 10)
	h.SetATRMultipliers(2.0, 0)
	h.UpdateATR(500)

	// percent 3% × 10_000 = 300; ATR 2×500 = 1000 (wins)
	got := h.stopLossDistance(10_000)
	if got != 1000 {
		t.Fatalf("stopLossDistance = %v, want 1000", got)
	}
}

// TestTickRiskHandler_UpdateATRFromIndicatorEvent verifies the event wiring:
// when an IndicatorEvent with a Primary.ATR arrives, Handle feeds the value
// into the handler so the next TickEvent sees the updated distance.
// TestTickRiskHandler_UpdateATRAcceptsZero is the Codex PR-12 follow-up:
// previously UpdateATR rejected zero (treated it like NaN), which meant a
// flat-market bar could not clear a stale positive ATR, and the handler
// would keep "max(percent, old ATR)" in effect when it should have fallen
// back to percent-only.
func TestTickRiskHandler_UpdateATRAcceptsZero(t *testing.T) {
	h := NewTickRiskHandler("PT15M", &atrTestExecutor{}, 5, 10)
	h.SetATRMultipliers(2.0, 2.0)

	// Seed with a real ATR.
	h.UpdateATR(500)
	if h.currentATR != 500 {
		t.Fatalf("seed: currentATR = %v, want 500", h.currentATR)
	}
	// Market goes flat: ATR should zero out. Before the fix this was a
	// no-op.
	h.UpdateATR(0)
	if h.currentATR != 0 {
		t.Fatalf("zero should be accepted, currentATR = %v", h.currentATR)
	}
	// trailingDistance now falls back to percent because ATR*mult == 0.
	got := h.trailingDistance(10_000)
	if got != 500 {
		t.Fatalf("trailing distance with ATR=0 should revert to percent=500, got %v", got)
	}
}

// TestTickRiskHandler_UpdateATRRejectsNaN keeps the NaN-rejection
// behaviour (NaN is emitted by the indicator calculator when data is
// insufficient; treating that as "flat market" would be wrong).
func TestTickRiskHandler_UpdateATRRejectsNaN(t *testing.T) {
	h := NewTickRiskHandler("PT15M", &atrTestExecutor{}, 5, 10)
	h.SetATRMultipliers(2.0, 2.0)
	h.UpdateATR(500)
	h.UpdateATR(math.NaN())
	if h.currentATR != 500 {
		t.Fatalf("NaN should be ignored, currentATR = %v, want 500", h.currentATR)
	}
}

func TestTickRiskHandler_UpdateATRFromIndicatorEvent(t *testing.T) {
	h := NewTickRiskHandler("PT15M", &atrTestExecutor{}, 5, 10)
	h.SetATRMultipliers(0, 2.0)

	atr := 300.0
	ev := entity.IndicatorEvent{
		SymbolID:  1,
		Interval:  "PT15M",
		Timestamp: 1,
		Primary:   entity.IndicatorSet{ATR: &atr},
	}
	if _, err := h.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if h.currentATR != 300 {
		t.Fatalf("currentATR = %v, want 300", h.currentATR)
	}
}

// TestTickRiskHandler_TrailingHitsAtATRDistance is the integration guard:
// given a position, a post-entry price excursion, and an ATR-distance
// reversal, the handler must close the position with reason="trailing_stop"
// at the ATR distance (not the tighter percent distance).
//
// Layout:
//   - Entry 10_000, TP=10%=11_000, percent SL=500 → 9_500, TP=11_000
//   - ATR=400, trailing multiplier=2.0 → trailing distance=800
//   - Use a BarLow/BarHigh tuple that *never* triggers SL or TP so the
//     SL/TP branch is a no-op for every tick; only the trailing branch
//     can close the position.
func TestTickRiskHandler_TrailingHitsAtATRDistance(t *testing.T) {
	exec := &atrTestExecutor{
		positions: []eventengine.Position{{
			PositionID: 1, SymbolID: 1,
			Side:       entity.OrderSideBuy,
			EntryPrice: 10_000,
			Amount:     0.1,
		}},
	}
	h := NewTickRiskHandler("PT15M", exec, 5, 10)
	h.SetATRMultipliers(0, 2.0)
	h.UpdateATR(400)

	// Bar range used for every tick: [9_600, 10_999]. Well above percent
	// SL (9_500) and strictly below TP (11_000), so SL/TP never fires.
	barLow := 9_600.0
	barHigh := 10_999.0

	ctx := context.Background()
	_, err := h.Handle(ctx, entity.TickEvent{
		SymbolID:  1,
		Interval:  "PT15M",
		Timestamp: 100,
		Price:     10_900, // moves "best" up
		BarLow:    barLow, BarHigh: barHigh,
	})
	if err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if len(exec.closes) != 0 {
		t.Fatalf("tick1 should not close anything, got %+v", exec.closes)
	}

	// Tick 2: price pulls back to 10_300 => drop from best 10_900 is 600.
	// That's < ATR distance 800, so no trailing hit.
	_, _ = h.Handle(ctx, entity.TickEvent{
		SymbolID: 1, Interval: "PT15M", Timestamp: 200,
		Price: 10_300, BarLow: barLow, BarHigh: barHigh,
	})
	if len(exec.closes) != 0 {
		t.Fatalf("trailing should not fire at drop=600 (<800), got %+v", exec.closes)
	}

	// Tick 3: price drops to 10_050 => drop = 850 >= 800, trail hits.
	_, _ = h.Handle(ctx, entity.TickEvent{
		SymbolID: 1, Interval: "PT15M", Timestamp: 300,
		Price: 10_050, BarLow: barLow, BarHigh: barHigh,
	})
	if len(exec.closes) != 1 {
		t.Fatalf("expected 1 trailing close, got %d: %+v", len(exec.closes), exec.closes)
	}
	if exec.closes[0].Reason != "trailing_stop" {
		t.Fatalf("close reason = %q, want trailing_stop", exec.closes[0].Reason)
	}
	// With the percent-only policy (distance 500), tick 2 would have hit
	// the trail (drop 600 > 500). The fact that we only fire at tick 3
	// (>= 800 drop) is the regression lock that proves ATR multiplier
	// is overriding the percent path.
}
