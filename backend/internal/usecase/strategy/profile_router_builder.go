package strategy

import (
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/regime"
)

// ProfileLoader is the minimum loader contract the router builder
// needs. The infrastructure-side strategyprofile.Loader satisfies this
// without the strategy package importing the infrastructure package
// (clean architecture: usecase depends on a port, not on infra).
type ProfileLoader interface {
	Load(name string) (*entity.StrategyProfile, error)
}

// BuildStrategyFromProfile turns a loaded StrategyProfile into a ready
// port.Strategy.
//
// When the profile is *not* a router (RegimeRouting.Default == ""),
// this is just NewConfigurableStrategy(root). When the profile *is* a
// router, this resolves every child via loader.Load, wraps each in a
// ConfigurableStrategy, and assembles a ProfileRouter.
//
// Depth limit: child profiles must NOT themselves carry regime_routing.
// This is enforced here (not in entity.StrategyProfile.Validate)
// because Validate is profile-local and cannot see other profiles.
// Capping at depth 1 keeps the routing graph trivially explainable
// ("router → flat strategy") and removes a class of cycle bugs that
// would otherwise need a visited-set walk to detect.
//
// The detector config is taken from the router profile's optional
// detector_config block when it is set in a future PR; for now every
// router uses regime.DefaultConfig. This is the "no behaviour change
// on existing profiles" promise — adding detector_config to the
// router schema can land in a follow-up without touching this builder.
func BuildStrategyFromProfile(loader ProfileLoader, root *entity.StrategyProfile) (port.Strategy, error) {
	if root == nil {
		return nil, fmt.Errorf("strategy: profile must not be nil")
	}
	if !root.HasRouting() {
		return NewConfigurableStrategy(root)
	}
	if loader == nil {
		return nil, fmt.Errorf("strategy: router profile %q requires a non-nil loader to resolve children", root.Name)
	}

	rr := root.RegimeRouting
	// Resolve default first so a typo there fails loud before we go
	// hunting for override children.
	defaultProfile, err := loadChild(loader, root.Name, "default", rr.Default)
	if err != nil {
		return nil, err
	}
	defaultStrategy, err := NewConfigurableStrategy(defaultProfile)
	if err != nil {
		return nil, fmt.Errorf("strategy: router %q default child %q: %w", root.Name, rr.Default, err)
	}

	overrides := make(map[entity.Regime]port.Strategy, len(rr.Overrides))
	for key, childName := range rr.Overrides {
		r := entity.Regime(key)
		// Validate already screened unknown regime keys, but defend
		// here too in case a future caller skips Validate.
		if !r.IsValidLabel() {
			return nil, fmt.Errorf("strategy: router %q overrides has unknown regime key %q", root.Name, key)
		}
		childProfile, err := loadChild(loader, root.Name, key, childName)
		if err != nil {
			return nil, err
		}
		childStrategy, err := NewConfigurableStrategy(childProfile)
		if err != nil {
			return nil, fmt.Errorf("strategy: router %q override[%q] child %q: %w", root.Name, key, childName, err)
		}
		overrides[r] = childStrategy
	}

	return NewProfileRouter(ProfileRouterInput{
		Name:            root.Name,
		DefaultStrategy: defaultStrategy,
		Overrides:       overrides,
	})
}

// loadChild loads one child profile and enforces depth-1 (children
// must not themselves carry regime_routing). The slot string is just
// for the error message — "default" or the regime label that owns
// this child reference.
func loadChild(loader ProfileLoader, parentName, slot, childName string) (*entity.StrategyProfile, error) {
	if childName == "" {
		return nil, fmt.Errorf("strategy: router %q slot %q has empty child profile name", parentName, slot)
	}
	if childName == parentName {
		return nil, fmt.Errorf("strategy: router %q slot %q references itself (would loop)", parentName, slot)
	}
	child, err := loader.Load(childName)
	if err != nil {
		return nil, fmt.Errorf("strategy: router %q slot %q: load child %q: %w", parentName, slot, childName, err)
	}
	if child == nil {
		return nil, fmt.Errorf("strategy: router %q slot %q: loader returned nil for %q", parentName, slot, childName)
	}
	if child.HasRouting() {
		return nil, fmt.Errorf("strategy: router %q slot %q: child %q is itself a routing profile (max depth is 1)", parentName, slot, childName)
	}
	return child, nil
}

// Keep regime.Config visible at the strategy package surface so users
// of BuildStrategyFromProfile do not need a second import for the
// router config (anti-import-pyramid pattern).
type DetectorConfig = regime.Config
