package decision

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// Config bundles the dependencies needed by Handler.
type Config struct {
	// Positions reports current net position side per symbol. Use
	// FlatPositionView during PR2 (shadow-only) and replace with a real
	// view in PR3 when DecisionHandler starts driving execution.
	Positions PositionView
}

// Handler converts MarketSignalEvent into ActionDecisionEvent by combining
// market direction with current position state. It is the central piece of
// the Decision layer (PR2 of the Signal/Decision/ExecutionPolicy three-layer
// separation).
//
// PR2 wiring: priority 27 on EventTypeMarketSignal. Output flows only into
// the recorder — RiskHandler still consumes the legacy SignalEvent path.
// PR3 swaps the Risk path to consume ActionDecisionEvent and wires cooldown.
type Handler struct {
	positions PositionView
}

// NewHandler builds a Handler. Panics if Positions is nil — composition-root
// invariant matching StrategyHandler's behaviour.
func NewHandler(cfg Config) *Handler {
	if cfg.Positions == nil {
		panic("decision: NewHandler Positions must not be nil")
	}
	return &Handler{positions: cfg.Positions}
}

// Handle implements eventengine.EventHandler. It only acts on
// MarketSignalEvent; other events pass through silently.
func (h *Handler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
	ev, ok := event.(entity.MarketSignalEvent)
	if !ok {
		return nil, nil
	}
	side := h.positions.CurrentSide(ctx, ev.Signal.SymbolID)
	decision := h.decide(ev.Signal, side)
	return []entity.Event{
		entity.ActionDecisionEvent{
			Decision:   decision,
			Price:      ev.Price,
			CurrentATR: ev.CurrentATR,
			Timestamp:  ev.Timestamp,
		},
	}, nil
}

// decide is the pure logic that maps (signal, position) → ActionDecision.
// Cooldown is intentionally absent in PR2; PR3 wires RiskManager.IsEntryCooldown
// in here as a guard before the position branches.
func (h *Handler) decide(ms entity.MarketSignal, hold entity.OrderSide) entity.ActionDecision {
	base := entity.ActionDecision{
		SymbolID:  ms.SymbolID,
		Source:    ms.Source,
		Strength:  ms.Strength,
		Timestamp: ms.Timestamp,
	}

	switch hold {
	case "":
		switch ms.Direction {
		case entity.DirectionBullish:
			base.Intent = entity.IntentNewEntry
			base.Side = entity.OrderSideBuy
			base.Reason = "no position; bullish signal -> new long"
		case entity.DirectionBearish:
			base.Intent = entity.IntentNewEntry
			base.Side = entity.OrderSideSell
			base.Reason = "no position; bearish signal -> new short"
		default:
			base.Intent = entity.IntentHold
			base.Reason = "no position; neutral signal"
		}
	case entity.OrderSideBuy:
		switch ms.Direction {
		case entity.DirectionBearish:
			base.Intent = entity.IntentExitCandidate
			base.Side = entity.OrderSideSell
			base.Reason = "long held; bearish signal -> exit candidate"
		default:
			base.Intent = entity.IntentHold
			base.Reason = "long held; not bearish"
		}
	case entity.OrderSideSell:
		switch ms.Direction {
		case entity.DirectionBullish:
			base.Intent = entity.IntentExitCandidate
			base.Side = entity.OrderSideBuy
			base.Reason = "short held; bullish signal -> exit candidate"
		default:
			base.Intent = entity.IntentHold
			base.Reason = "short held; not bullish"
		}
	default:
		base.Intent = entity.IntentHold
		base.Reason = "unknown position side; defensive hold"
	}
	return base
}
