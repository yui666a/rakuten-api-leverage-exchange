package decision

import (
	"context"
	"testing"
	"time"

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

// stubCooldown reports a fixed value regardless of `now`. PR3 plumbs the real
// RiskManager.IsEntryCooldown into Handler; this stub keeps the unit tests
// independent of clock plumbing.
type stubCooldown struct{ active bool }

func (s stubCooldown) IsEntryCooldown(_ time.Time) bool { return s.active }

func TestHandler_CooldownActive_ForcesCooldownBlocked(t *testing.T) {
	cases := []struct {
		name      string
		hold      entity.OrderSide
		direction entity.SignalDirection
	}{
		{"flat+bullish", "", entity.DirectionBullish},
		{"flat+bearish", "", entity.DirectionBearish},
		{"flat+neutral", "", entity.DirectionNeutral},
		{"long+bearish (would be EXIT_CANDIDATE)", entity.OrderSideBuy, entity.DirectionBearish},
		{"short+bullish (would be EXIT_CANDIDATE)", entity.OrderSideSell, entity.DirectionBullish},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := NewHandler(Config{
				Positions: fixedPositionView{side: c.hold},
				Cooldown:  stubCooldown{active: true},
			})
			out, err := h.Handle(context.Background(), entity.MarketSignalEvent{
				Signal:    entity.MarketSignal{SymbolID: 7, Direction: c.direction},
				Timestamp: 1700000000000,
			})
			if err != nil {
				t.Fatalf("Handle: %v", err)
			}
			dec := out[0].(entity.ActionDecisionEvent).Decision
			if dec.Intent != entity.IntentCooldownBlocked {
				t.Errorf("Intent = %q, want COOLDOWN_BLOCKED (cooldown should win over %q+%q)", dec.Intent, c.hold, c.direction)
			}
			if dec.Side != "" {
				t.Errorf("Side = %q, want empty", dec.Side)
			}
		})
	}
}

func TestHandler_CooldownInactive_RunsMatrix(t *testing.T) {
	// When cooldown is plumbed but inactive, behaviour must be identical to
	// the no-cooldown path. Spot-check the BUY branch that would otherwise
	// be the first thing affected by a logic mistake.
	h := NewHandler(Config{
		Positions: FlatPositionView{},
		Cooldown:  stubCooldown{active: false},
	})
	out, _ := h.Handle(context.Background(), entity.MarketSignalEvent{
		Signal:    entity.MarketSignal{SymbolID: 7, Direction: entity.DirectionBullish},
		Timestamp: 1700000000000,
	})
	dec := out[0].(entity.ActionDecisionEvent).Decision
	if dec.Intent != entity.IntentNewEntry || dec.Side != entity.OrderSideBuy {
		t.Errorf("inactive cooldown changed matrix: got Intent=%q Side=%q", dec.Intent, dec.Side)
	}
}

func TestHandler_NilCooldown_PreservesLegacyBehaviour(t *testing.T) {
	// Config without Cooldown field should match PR2 behaviour exactly.
	h := NewHandler(Config{Positions: FlatPositionView{}})
	out, _ := h.Handle(context.Background(), entity.MarketSignalEvent{
		Signal:    entity.MarketSignal{SymbolID: 7, Direction: entity.DirectionBearish},
		Timestamp: 1,
	})
	dec := out[0].(entity.ActionDecisionEvent).Decision
	if dec.Intent != entity.IntentNewEntry {
		t.Errorf("nil cooldown should not block: Intent=%q", dec.Intent)
	}
}
