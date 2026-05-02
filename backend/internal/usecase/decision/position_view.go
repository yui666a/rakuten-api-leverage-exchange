// Package decision implements the Decision layer of the
// Signal/Decision/ExecutionPolicy three-layer separation. It receives
// MarketSignalEvent (situational interpretation from Strategy) and emits
// ActionDecisionEvent (concrete intent + side) which RiskHandler eventually
// consumes in PR3.
package decision

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// PositionView exposes the current net position side for a symbol.
// Implementations must be safe to call from the EventBus dispatch goroutine.
type PositionView interface {
	// CurrentSide reports OrderSideBuy when net long, OrderSideSell when net
	// short, or empty string when flat. The return type uses entity.OrderSide
	// so callers can compare directly against ActionDecision.Side.
	CurrentSide(ctx context.Context, symbolID int64) entity.OrderSide
}

// FlatPositionView always reports "no position". PR2 wires this in both
// backtest and live paths because DecisionHandler is shadow-only — its
// output drives the recorder for column population, not real orders. PR3
// replaces it with PositionManager- / SimExecutor-backed implementations
// when the new route starts to drive real execution.
type FlatPositionView struct{}

func (FlatPositionView) CurrentSide(ctx context.Context, symbolID int64) entity.OrderSide {
	return ""
}
