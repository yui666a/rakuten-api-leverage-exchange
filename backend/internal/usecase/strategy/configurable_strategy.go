package strategy

import (
	"context"
	"fmt"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// ConfigurableStrategy is a port.Strategy driven by a StrategyProfile. It
// wires a profile into a RuleBasedStanceResolver + StrategyEngine pair so the
// existing signal pipeline is reused, while the thresholds and feature
// toggles are sourced from the profile rather than the hard-coded defaults.
//
// Scope note: indicator *periods* (from IndicatorConfig) are informational at
// this layer — the live pipeline computes IndicatorSet with fixed periods
// upstream, so periods are not applied by this struct. Stance and signal
// thresholds, however, are fully profile-driven.
type ConfigurableStrategy struct {
	profile *entity.StrategyProfile
	engine  *usecase.StrategyEngine
}

// NewConfigurableStrategy builds a ConfigurableStrategy from the supplied
// profile. The profile is validated up-front; invalid profiles fail
// construction rather than surfacing later as confusing signal behaviour.
//
// breakout_volume_ratio appears in both StanceRulesConfig and
// SignalRulesConfig; they govern different code paths (stance classification
// vs. signal generation) and are intentionally wired separately.
func NewConfigurableStrategy(profile *entity.StrategyProfile) (*ConfigurableStrategy, error) {
	if profile == nil {
		return nil, fmt.Errorf("strategy: profile must not be nil")
	}
	if err := profile.Validate(); err != nil {
		return nil, fmt.Errorf("strategy: invalid profile: %w", err)
	}

	// Stateless stance resolver: DisableOverride + DisablePersistence both
	// true so the strategy is deterministic for backtests and does not touch
	// the override repository.
	resolverOpts := usecase.RuleBasedStanceResolverOptions{
		DisableOverride:         true,
		DisablePersistence:      true,
		RSIOversold:             profile.StanceRules.RSIOversold,
		RSIOverbought:           profile.StanceRules.RSIOverbought,
		SMAConvergenceThreshold: profile.StanceRules.SMAConvergenceThreshold,
		// StanceRules.BreakoutVolumeRatio drives the BREAKOUT *stance*
		// decision; SignalRules.Breakout.VolumeRatioMin drives the breakout
		// *signal* threshold below. They are separate values by design.
		BreakoutVolumeRatio: profile.StanceRules.BreakoutVolumeRatio,
	}
	resolver := usecase.NewRuleBasedStanceResolverWithOptions(nil, resolverOpts)

	engineOpts := usecase.StrategyEngineOptions{
		// Trend-follow
		EnableTrendFollow:  profile.SignalRules.TrendFollow.Enabled,
		RSIBuyMax:          profile.SignalRules.TrendFollow.RSIBuyMax,
		RSISellMin:         profile.SignalRules.TrendFollow.RSISellMin,
		RequireMACDConfirm: profile.SignalRules.TrendFollow.RequireMACDConfirm,
		RequireEMACross:    profile.SignalRules.TrendFollow.RequireEMACross,
		TrendFollowADXMin:  profile.SignalRules.TrendFollow.ADXMin, // PR-6

		// Contrarian
		EnableContrarian:        profile.SignalRules.Contrarian.Enabled,
		ContrarianRSIEntry:      profile.SignalRules.Contrarian.RSIEntry,
		ContrarianRSIExit:       profile.SignalRules.Contrarian.RSIExit,
		MACDHistogramLimit:      profile.SignalRules.Contrarian.MACDHistogramLimit,
		ContrarianADXMax:        profile.SignalRules.Contrarian.ADXMax, // PR-6
		ContrarianStochEntryMax: profile.SignalRules.Contrarian.StochEntryMax, // PR-7
		ContrarianStochExitMin:  profile.SignalRules.Contrarian.StochExitMin,  // PR-7

		// Breakout
		EnableBreakout:             profile.SignalRules.Breakout.Enabled,
		BreakoutVolumeRatio:        profile.SignalRules.Breakout.VolumeRatioMin,
		BreakoutRequireMACDConfirm: profile.SignalRules.Breakout.RequireMACDConfirm,
		BreakoutADXMin:             profile.SignalRules.Breakout.ADXMin, // PR-6
		BreakoutDonchianPeriod:     profile.SignalRules.Breakout.DonchianPeriod, // PR-11

		// HTF filter
		HTFEnabled:           profile.HTFFilter.Enabled,
		HTFBlockCounterTrend: profile.HTFFilter.BlockCounterTrend,
		HTFAlignmentBoost:    profile.HTFFilter.AlignmentBoost,
		HTFMode:              profile.HTFFilter.Mode, // PR-8
	}
	engine := usecase.NewStrategyEngineWithOptions(resolver, engineOpts)

	return &ConfigurableStrategy{
		profile: profile,
		engine:  engine,
	}, nil
}

// Evaluate implements port.Strategy by delegating to the underlying
// StrategyEngine. Nil indicators yield ErrIndicatorsRequired to mirror
// DefaultStrategy's contract.
func (s *ConfigurableStrategy) Evaluate(
	ctx context.Context,
	indicators *entity.IndicatorSet,
	higherTF *entity.IndicatorSet,
	lastPrice float64,
	now time.Time,
) (*entity.Signal, error) {
	if indicators == nil {
		return nil, ErrIndicatorsRequired
	}
	return s.engine.EvaluateWithHigherTFAt(ctx, *indicators, higherTF, lastPrice, now)
}

// Name returns the profile's name. Registries key strategies by Name(), so
// loading two profiles with the same name is a caller error.
func (s *ConfigurableStrategy) Name() string {
	return s.profile.Name
}

// Compile-time guarantee that ConfigurableStrategy satisfies port.Strategy.
var _ port.Strategy = (*ConfigurableStrategy)(nil)
