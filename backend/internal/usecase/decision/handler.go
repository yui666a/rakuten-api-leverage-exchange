package decision

import (
	"context"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// CooldownChecker reports whether the entry cooldown is active. Implemented
// by RiskManager (its IsEntryCooldown method satisfies this interface). nil
// is allowed and disables the cooldown branch entirely (used by tests and
// during PR2 shadow wiring before the real cooldown is plumbed).
type CooldownChecker interface {
	IsEntryCooldown(now time.Time) bool
}

// Config bundles the dependencies needed by Handler.
type Config struct {
	// Positions reports current net position side per symbol. Use
	// FlatPositionView during PR2 (shadow-only) and replace with a real
	// view in PR3 when DecisionHandler starts driving execution.
	Positions PositionView
	// Cooldown is optional. When non-nil, an active entry cooldown forces
	// every signal to COOLDOWN_BLOCKED regardless of position state. nil
	// keeps the legacy behaviour (no cooldown branch).
	Cooldown CooldownChecker
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
	cooldown  CooldownChecker
}

// NewHandler builds a Handler. Panics if Positions is nil — composition-root
// invariant matching StrategyHandler's behaviour. Cooldown may be nil.
func NewHandler(cfg Config) *Handler {
	if cfg.Positions == nil {
		panic("decision: NewHandler Positions must not be nil")
	}
	return &Handler{positions: cfg.Positions, cooldown: cfg.Cooldown}
}

// Handle implements eventengine.EventHandler. It only acts on
// MarketSignalEvent; other events pass through silently.
func (h *Handler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
	ev, ok := event.(entity.MarketSignalEvent)
	if !ok {
		return nil, nil
	}
	side := h.positions.CurrentSide(ctx, ev.Signal.SymbolID)
	decision := h.decide(ev.Signal, side, time.UnixMilli(ev.Timestamp))
	return []entity.Event{
		entity.ActionDecisionEvent{
			Decision:   decision,
			Price:      ev.Price,
			CurrentATR: ev.CurrentATR,
			Timestamp:  ev.Timestamp,
		},
	}, nil
}

// decide is the pure logic that maps (signal, position, now) → ActionDecision.
// Cooldown is checked first: an active entry cooldown short-circuits every
// other branch into COOLDOWN_BLOCKED so the bot does not chase entries
// immediately after a close fill.
func (h *Handler) decide(ms entity.MarketSignal, hold entity.OrderSide, now time.Time) entity.ActionDecision {
	base := entity.ActionDecision{
		SymbolID:  ms.SymbolID,
		Source:    ms.Source,
		Strength:  ms.Strength,
		Timestamp: ms.Timestamp,
	}

	if h.cooldown != nil && h.cooldown.IsEntryCooldown(now) {
		base.Intent = entity.IntentCooldownBlocked
		base.Reason = "entry cooldown active after recent close"
		return base
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
