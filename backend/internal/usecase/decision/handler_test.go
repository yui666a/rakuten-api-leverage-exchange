package decision

import (
	"context"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// fixedPositionView returns a constant side, ignoring symbol id. Use it to
// exercise Handler.decide via the public Handle path.
type fixedPositionView struct{ side entity.OrderSide }

func (f fixedPositionView) CurrentSide(ctx context.Context, symbolID int64) entity.OrderSide {
	return f.side
}

func TestHandler_Decide_Matrix(t *testing.T) {
	cases := []struct {
		name      string
		hold      entity.OrderSide
		direction entity.SignalDirection
		wantInt   entity.DecisionIntent
		wantSide  entity.OrderSide
	}{
		{"flat+bullish", "", entity.DirectionBullish, entity.IntentNewEntry, entity.OrderSideBuy},
		{"flat+bearish", "", entity.DirectionBearish, entity.IntentNewEntry, entity.OrderSideSell},
		{"flat+neutral", "", entity.DirectionNeutral, entity.IntentHold, ""},
		{"long+bullish", entity.OrderSideBuy, entity.DirectionBullish, entity.IntentHold, ""},
		{"long+bearish", entity.OrderSideBuy, entity.DirectionBearish, entity.IntentExitCandidate, entity.OrderSideSell},
		{"long+neutral", entity.OrderSideBuy, entity.DirectionNeutral, entity.IntentHold, ""},
		{"short+bullish", entity.OrderSideSell, entity.DirectionBullish, entity.IntentExitCandidate, entity.OrderSideBuy},
		{"short+bearish", entity.OrderSideSell, entity.DirectionBearish, entity.IntentHold, ""},
		{"short+neutral", entity.OrderSideSell, entity.DirectionNeutral, entity.IntentHold, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := NewHandler(Config{Positions: fixedPositionView{side: c.hold}})
			ev := entity.MarketSignalEvent{
				Signal: entity.MarketSignal{
					SymbolID:  10,
					Direction: c.direction,
					Strength:  0.5,
					Source:    "test",
					Timestamp: 1700000000000,
				},
				Price:     8900,
				Timestamp: 1700000000000,
			}
			out, err := h.Handle(context.Background(), ev)
			if err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if len(out) != 1 {
				t.Fatalf("expected 1 event, got %d", len(out))
			}
			dec, ok := out[0].(entity.ActionDecisionEvent)
			if !ok {
				t.Fatalf("expected ActionDecisionEvent, got %T", out[0])
			}
			if dec.Decision.Intent != c.wantInt {
				t.Errorf("Intent = %q, want %q", dec.Decision.Intent, c.wantInt)
			}
			if dec.Decision.Side != c.wantSide {
				t.Errorf("Side = %q, want %q", dec.Decision.Side, c.wantSide)
			}
			if dec.Decision.Reason == "" {
				t.Error("Reason should not be empty")
			}
		})
	}
}

func TestHandler_PreservesSignalMetadata(t *testing.T) {
	h := NewHandler(Config{Positions: FlatPositionView{}})
	ev := entity.MarketSignalEvent{
		Signal: entity.MarketSignal{
			SymbolID:  10,
			Direction: entity.DirectionBullish,
			Strength:  0.73,
			Source:    "contrarian:rsi",
			Timestamp: 1700000000000,
		},
		Price:      8900.5,
		CurrentATR: 12.4,
		Timestamp:  1700000000000,
	}
	out, err := h.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	dec := out[0].(entity.ActionDecisionEvent)
	if dec.Decision.Strength != 0.73 {
		t.Errorf("Strength = %v, want 0.73", dec.Decision.Strength)
	}
	if dec.Decision.Source != "contrarian:rsi" {
		t.Errorf("Source = %q, want contrarian:rsi", dec.Decision.Source)
	}
	if dec.Price != 8900.5 {
		t.Errorf("Price not propagated, got %v", dec.Price)
	}
	if dec.CurrentATR != 12.4 {
		t.Errorf("CurrentATR not propagated, got %v", dec.CurrentATR)
	}
	if dec.Timestamp != 1700000000000 {
		t.Errorf("Timestamp not propagated, got %v", dec.Timestamp)
	}
}

func TestHandler_IgnoresOtherEvents(t *testing.T) {
	h := NewHandler(Config{Positions: FlatPositionView{}})
	out, err := h.Handle(context.Background(), entity.IndicatorEvent{Timestamp: 1})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil events for non-MarketSignalEvent, got %v", out)
	}
}

func TestNewHandler_PanicsOnNilPositions(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil Positions")
		}
	}()
	NewHandler(Config{Positions: nil})
}

// Cooldown 経路は PR3 で配線するためここでは未テスト。
// PR3 の plan で COOLDOWN_BLOCKED / cooldown timing のテストを追加する。
func TestHandler_CooldownIsDeferredToPR3(t *testing.T) {
	t.Skip("cooldown wired in PR3 (RiskManager.IsEntryCooldown)")
}
