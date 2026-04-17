// Package port defines inbound/outbound ports (interfaces) used to decouple the
// domain/usecase layers from concrete implementations. Ports live in the domain
// layer so that dependencies point inward (infrastructure depends on port, not
// the other way around).
package port

import (
	"context"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// Strategy is the abstract port for a trading strategy. Implementations turn a
// snapshot of technical indicators into a trading Signal.
//
// Semantics of Evaluate's indicators argument:
//   - indicators is a pointer by convention so implementations can distinguish
//     "no indicators available" (nil) from "indicators present but incomplete".
//   - Callers MUST pass a non-nil *entity.IndicatorSet when they have
//     calculated a snapshot. A nil argument signals "no data" and the
//     implementation is expected to return an error rather than a HOLD signal,
//     because "no signal" and "no input" are different conditions.
//   - higherTF may be nil: it denotes an optional multi-timeframe indicator
//     snapshot that the strategy may use to filter or confirm the primary
//     signal.
//   - now is the evaluation timestamp used by the strategy to stamp the
//     resulting Signal. Using an explicit time (rather than time.Now()) keeps
//     backtests deterministic.
type Strategy interface {
	Evaluate(
		ctx context.Context,
		indicators *entity.IndicatorSet,
		higherTF *entity.IndicatorSet,
		lastPrice float64,
		now time.Time,
	) (*entity.Signal, error)

	// Name returns a stable identifier for the strategy (e.g. "default").
	// Used by registries and logging.
	Name() string
}
