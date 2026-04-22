package strategy

import (
	"context"
	"fmt"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/regime"
)

// ProfileRouter is a port.Strategy that delegates each Evaluate to a
// child Strategy chosen by the current Regime.
//
// The router owns one regime.Detector instance for its full lifetime
// (one router per backtest run / live pipeline). Hysteresis state on
// the detector therefore persists across bars within a single run,
// which is the entire point — without persisted state every bar would
// re-classify from scratch and the dwell threshold would never apply.
//
// Wiring is fully constructed at NewProfileRouter time: the caller
// resolves child profiles to their port.Strategy implementations and
// hands them in by Regime. This keeps ProfileRouter free of any
// loader/profile concerns and makes the dispatch behaviour trivially
// testable with stub Strategies.
//
// Fallback rules at delegation time:
//   - RegimeUnknown (warmup, missing inputs) → defaultStrategy
//   - Any regime not present in overrides    → defaultStrategy
//   - When higherTF is nil but the detector needs it for direction, it
//     falls back to primary SMA cross (see regime.classifyDirection)
//     so the router never panics on missing optional inputs.
type ProfileRouter struct {
	name            string
	detector        *regime.Detector
	defaultStrategy port.Strategy
	overrides       map[entity.Regime]port.Strategy
}

// ProfileRouterInput collects the pre-resolved children plus a detector
// override hook. DetectorConfig is taken by value so a zero-valued
// config falls through to regime.DefaultConfig in NewDetector.
type ProfileRouterInput struct {
	Name            string
	DetectorConfig  regime.Config
	DefaultStrategy port.Strategy
	Overrides       map[entity.Regime]port.Strategy
}

// NewProfileRouter builds a ProfileRouter from pre-resolved Strategy
// children. The caller is responsible for constructing each child
// (typically via NewConfigurableStrategy on a child profile).
//
// Validation:
//   - DefaultStrategy is required: a router with no default would
//     return ErrNoStrategyForRegime on every warmup bar.
//   - Overrides keys must be known Regime values; RegimeUnknown is
//     rejected because it would shadow Default.
//   - Empty Name is rejected so registries / log lines stay legible.
func NewProfileRouter(in ProfileRouterInput) (*ProfileRouter, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("profile router: name must not be empty")
	}
	if in.DefaultStrategy == nil {
		return nil, fmt.Errorf("profile router %q: default strategy is required", in.Name)
	}
	for r, s := range in.Overrides {
		if !r.IsValidLabel() {
			return nil, fmt.Errorf("profile router %q: overrides has unknown regime key %q", in.Name, string(r))
		}
		if s == nil {
			return nil, fmt.Errorf("profile router %q: overrides[%q] is nil", in.Name, string(r))
		}
	}

	overrides := make(map[entity.Regime]port.Strategy, len(in.Overrides))
	for r, s := range in.Overrides {
		overrides[r] = s
	}

	return &ProfileRouter{
		name:            in.Name,
		detector:        regime.NewDetector(in.DetectorConfig),
		defaultStrategy: in.DefaultStrategy,
		overrides:       overrides,
	}, nil
}

// Evaluate consults the regime detector, picks the matching child
// Strategy, and delegates. The returned Signal carries whichever
// confidence/decision the child produced — the router does not
// post-process.
//
// The selected child is recorded on the Signal via the existing
// signal.Confidence field's neighbours indirectly (no schema change
// in this PR); a future PR can add a "regime"+"selectedProfile" field
// to Signal once the router is in production.
func (r *ProfileRouter) Evaluate(
	ctx context.Context,
	indicators *entity.IndicatorSet,
	higherTF *entity.IndicatorSet,
	lastPrice float64,
	now time.Time,
) (*entity.Signal, error) {
	if indicators == nil {
		return nil, ErrIndicatorsRequired
	}

	classification := r.detector.Classify(*indicators, higherTF, lastPrice)
	child := r.SelectStrategy(classification.Regime)
	return child.Evaluate(ctx, indicators, higherTF, lastPrice, now)
}

// SelectStrategy returns the child Strategy that the router would pick
// for the given Regime, or the default when no override applies.
// Exposed for tests and for diagnostic tooling that needs to label a
// trade with the strategy that produced it without re-running the
// detector.
func (r *ProfileRouter) SelectStrategy(rg entity.Regime) port.Strategy {
	if !rg.IsKnown() {
		return r.defaultStrategy
	}
	if s, ok := r.overrides[rg]; ok {
		return s
	}
	return r.defaultStrategy
}

// Name returns the router's profile name. Registries key strategies by
// Name(), so loading two routers with the same name is a caller error.
func (r *ProfileRouter) Name() string { return r.name }

// CommittedRegime exposes the detector's current committed regime
// without forcing a re-classify. Used by tests and by future telemetry
// hooks that want to log the active regime per tick.
func (r *ProfileRouter) CommittedRegime() entity.Regime {
	return r.detector.Committed()
}

// Reset rewinds the detector's hysteresis state. Backtest runners
// should call this between independent windows so the previous
// window's regime memory does not bleed into the next one.
func (r *ProfileRouter) Reset() {
	r.detector.Reset()
}

// Compile-time guarantee that ProfileRouter satisfies port.Strategy.
var _ port.Strategy = (*ProfileRouter)(nil)
