// Package strategy hosts the pluggable trading-strategy abstraction: the
// DefaultStrategy (wrapper around the classic StrategyEngine) and the
// StrategyRegistry that lets composition roots look strategies up by name.
package strategy

import (
	"context"
	"errors"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// DefaultStrategyName is the registration name of the built-in strategy.
const DefaultStrategyName = "default"

// ErrIndicatorsRequired is returned by Evaluate when the caller passes a nil
// indicators argument. The port's contract is that callers must supply a
// snapshot (possibly with missing fields), so nil is treated as a programmer
// error rather than silently downgrading to HOLD.
var ErrIndicatorsRequired = errors.New("strategy: indicators required")

// DefaultStrategy adapts the existing *usecase.StrategyEngine to the
// port.Strategy interface. It is the production strategy and satisfies
// port.Strategy.
//
// The wrapper keeps runtime behaviour identical to calling
// StrategyEngine.EvaluateWithHigherTFAt directly.
type DefaultStrategy struct {
	engine *usecase.StrategyEngine
}

// NewDefaultStrategy wraps a StrategyEngine so it can be consumed as a
// port.Strategy. engine must not be nil.
func NewDefaultStrategy(engine *usecase.StrategyEngine) *DefaultStrategy {
	return &DefaultStrategy{engine: engine}
}

// Evaluate implements port.Strategy by delegating to the underlying
// StrategyEngine. It converts the pointer-based port signature to the
// by-value signature used by StrategyEngine. Nil indicators result in
// ErrIndicatorsRequired.
func (s *DefaultStrategy) Evaluate(
	ctx context.Context,
	indicators *entity.IndicatorSet,
	higherTF *entity.IndicatorSet,
	lastPrice float64,
	now time.Time,
) (*entity.Signal, error) {
	if s == nil || s.engine == nil {
		return nil, errors.New("strategy: default strategy is not initialised")
	}
	if indicators == nil {
		return nil, ErrIndicatorsRequired
	}
	return s.engine.EvaluateWithHigherTFAt(ctx, *indicators, higherTF, lastPrice, now)
}

// Name returns the stable identifier used to register this strategy.
func (s *DefaultStrategy) Name() string {
	return DefaultStrategyName
}

// Compile-time guarantee that DefaultStrategy satisfies port.Strategy.
var _ port.Strategy = (*DefaultStrategy)(nil)
