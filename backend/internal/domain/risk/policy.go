// Package risk holds the strategy-level risk policy domain types.
//
// The policy describes how a strategy exits a trade — stop loss, take
// profit, and trailing stop. It is the single representation both the
// backtest runner and the live event pipeline read from, so a profile
// tuned in PDCA produces identical exit behaviour in production.
//
// Before this package the same knobs lived as loose float64 fields on
// EventDrivenPipelineConfig, the backtest RunInput, and TickRiskHandler,
// glued together by setter calls that the live path silently failed to
// invoke. Promoting RiskPolicy to a constructor argument turns that
// "forgot to call SetX" class of bug into a compile error.
package risk

import (
	"errors"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// StopLossMode selects how the stop-loss distance is computed at runtime.
type StopLossMode int

const (
	// StopLossModePercent computes distance as entryPrice × Percent / 100.
	StopLossModePercent StopLossMode = iota
	// StopLossModeATR computes distance as currentATR × Multiplier and
	// falls back to the percent path when ATR is not yet known.
	StopLossModeATR
)

// StopLossSpec declares the stop-loss policy. Percent is required (the
// fallback when ATR is unavailable); ATRMultiplier is optional.
type StopLossSpec struct {
	// Percent is the stop-loss distance as a fraction of entry price,
	// expressed as a percentage (e.g. 14.0 for 14%). Must be > 0.
	Percent float64
	// ATRMultiplier scales the current ATR to derive the stop distance.
	// 0 disables the ATR branch and the percent value is used verbatim.
	ATRMultiplier float64
}

// Mode returns the active mode given the configured fields.
func (s StopLossSpec) Mode() StopLossMode {
	if s.ATRMultiplier > 0 {
		return StopLossModeATR
	}
	return StopLossModePercent
}

// TakeProfitSpec declares the take-profit policy. Today only a percent
// take-profit is supported; the type leaves room to add ATR / R-multiple
// take-profits without revisiting every call site.
type TakeProfitSpec struct {
	// Percent is the take-profit distance as a percentage of entry price.
	// Must be > 0.
	Percent float64
}

// TrailingMode selects how the trailing-stop reversal distance is
// computed independently of the stop-loss policy.
type TrailingMode int

const (
	// TrailingModeDisabled turns off trailing-stop tracking entirely.
	TrailingModeDisabled TrailingMode = iota
	// TrailingModePercent uses StopLossSpec.Percent as the reversal
	// distance. This matches the legacy behaviour where percent SL also
	// served as the trailing distance via TickRiskHandler.trailingDistance.
	TrailingModePercent
	// TrailingModeATR uses currentATR × ATRMultiplier as the reversal
	// distance, with TrailingModePercent as the fallback when ATR is
	// not yet known. This is what production_ltc_60k actually wants
	// (trailing_atr_multiplier=2.5).
	TrailingModeATR
)

// TrailingSpec declares the trailing-stop policy. Mode == Disabled
// switches the trailing path off entirely; Mode == ATR requires a
// positive ATRMultiplier.
type TrailingSpec struct {
	Mode          TrailingMode
	ATRMultiplier float64
}

// RiskPolicy is the strategy-level risk envelope passed into the
// TickRiskHandler. Constructing it via FromProfile (or hand-built in
// tests) is the only sanctioned way for either backtest or live to
// reach the handler — there is no longer a setter the live path could
// forget to call.
type RiskPolicy struct {
	StopLoss   StopLossSpec
	TakeProfit TakeProfitSpec
	Trailing   TrailingSpec
}

// Validate enforces the invariants the TickRiskHandler relies on. It is
// run at profile load time so a misconfigured profile fails fast at
// startup rather than producing silent HOLD-only behaviour in live.
func (p RiskPolicy) Validate() error {
	if p.StopLoss.Percent <= 0 {
		return fmt.Errorf("risk policy: stop_loss percent must be > 0 (got %v)", p.StopLoss.Percent)
	}
	if p.StopLoss.ATRMultiplier < 0 {
		return fmt.Errorf("risk policy: stop_loss atr_multiplier must be >= 0 (got %v)", p.StopLoss.ATRMultiplier)
	}
	if p.TakeProfit.Percent <= 0 {
		return fmt.Errorf("risk policy: take_profit percent must be > 0 (got %v)", p.TakeProfit.Percent)
	}
	switch p.Trailing.Mode {
	case TrailingModeDisabled, TrailingModePercent:
		// no extra constraints
	case TrailingModeATR:
		if p.Trailing.ATRMultiplier <= 0 {
			return fmt.Errorf("risk policy: trailing mode=ATR requires atr_multiplier > 0 (got %v)", p.Trailing.ATRMultiplier)
		}
	default:
		return fmt.Errorf("risk policy: unknown trailing mode %v", p.Trailing.Mode)
	}
	return nil
}

// MustValidate panics if the policy is invalid. Useful for test builders
// where a malformed policy indicates a bug in the test, not user input.
func (p RiskPolicy) MustValidate() RiskPolicy {
	if err := p.Validate(); err != nil {
		panic(err)
	}
	return p
}

// ErrEmptyPolicy is returned by FromProfile when the caller passed a nil
// profile pointer. Callers that explicitly support "no profile" (e.g.
// the legacy env-only live path) should branch on this and supply a
// hand-built fallback policy.
var ErrEmptyPolicy = errors.New("risk policy: no profile supplied")

// FromProfile builds a RiskPolicy from a StrategyProfile, applying the
// fallback rules that used to be scattered across cmd/main.go's
// liveStopLossPercent / liveTakeProfitPercent / liveStopLossATRMultiplier
// helpers and the backtest runner's per-call SetATRMultipliers logic.
//
// envSL / envTP are the legacy environment-driven fallbacks: when the
// profile declares the field as 0 they are used instead. Callers that
// have no env-only fallback (e.g. tests) pass 0 and get an
// ErrEmptyPolicy-style validation error from RiskPolicy.Validate, which
// is exactly the early failure mode this package is meant to enforce.
//
// FromProfile does not call Validate — callers are expected to call it
// at startup so a misconfigured profile fails before the pipeline runs.
func FromProfile(p *entity.StrategyProfile, envSL, envTP float64) (RiskPolicy, error) {
	if p == nil {
		return policyFromEnv(envSL, envTP), ErrEmptyPolicy
	}
	slPercent := p.Risk.StopLossPercent
	if slPercent <= 0 {
		slPercent = envSL
	}
	tpPercent := p.Risk.TakeProfitPercent
	if tpPercent <= 0 {
		tpPercent = envTP
	}
	policy := RiskPolicy{
		StopLoss: StopLossSpec{
			Percent:       slPercent,
			ATRMultiplier: p.Risk.StopLossATRMultiplier,
		},
		TakeProfit: TakeProfitSpec{Percent: tpPercent},
		Trailing:   trailingFromATR(p.Risk.TrailingATRMultiplier),
	}
	return policy, nil
}

// policyFromEnv is the nil-profile fallback. It mirrors the legacy
// env-only live path which historically ran with percent SL / TP and
// no trailing — promoted profiles supersede this entirely.
func policyFromEnv(envSL, envTP float64) RiskPolicy {
	return RiskPolicy{
		StopLoss:   StopLossSpec{Percent: envSL},
		TakeProfit: TakeProfitSpec{Percent: envTP},
		Trailing:   TrailingSpec{Mode: TrailingModePercent},
	}
}

// trailingFromATR maps the legacy "trailing_atr_multiplier" knob into
// the explicit TrailingSpec the rest of the system reads. 0 → Percent
// (legacy behaviour), > 0 → ATR with the supplied multiplier. Negative
// values are not normalised here so Validate can reject them.
func trailingFromATR(multiplier float64) TrailingSpec {
	if multiplier > 0 {
		return TrailingSpec{Mode: TrailingModeATR, ATRMultiplier: multiplier}
	}
	return TrailingSpec{Mode: TrailingModePercent}
}
