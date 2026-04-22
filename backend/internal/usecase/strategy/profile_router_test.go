package strategy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
)

// recordingStrategy is a minimal port.Strategy that records every Evaluate
// call so tests can assert which child the router delegated to.
type recordingStrategy struct {
	name string

	mu    sync.Mutex
	calls int
}

func (s *recordingStrategy) Evaluate(_ context.Context, _ *entity.IndicatorSet, _ *entity.IndicatorSet, _ float64, now time.Time) (*entity.Signal, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	// Return a HOLD signal stamped with the strategy name so the test
	// can also assert via the Signal payload, not just the call counter.
	return &entity.Signal{
		Action:    entity.SignalActionHold,
		Reason:    s.name,
		Timestamp: now.UnixMilli(),
	}, nil
}

func (s *recordingStrategy) Name() string { return s.name }

func (s *recordingStrategy) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// fp helper for IndicatorSet pointer fields.
func fp(v float64) *float64 { return &v }

// trendingIndicators builds an IndicatorSet that the regime detector
// will classify as bull-trend (ADX strong + SMA20>SMA50).
func trendingIndicators() entity.IndicatorSet {
	return entity.IndicatorSet{
		ADX14: fp(35),
		ATR14: fp(1.0),
		SMA20: fp(110),
		SMA50: fp(100),
	}
}

// bearIndicators builds an IndicatorSet that classifies as bear-trend.
func bearIndicators() entity.IndicatorSet {
	return entity.IndicatorSet{
		ADX14: fp(35),
		ATR14: fp(1.0),
		SMA20: fp(95),
		SMA50: fp(100),
	}
}

// rangeIndicators builds an IndicatorSet that classifies as range
// (low ADX + low ATR%).
func rangeIndicators() entity.IndicatorSet {
	return entity.IndicatorSet{
		ADX14: fp(12),
		ATR14: fp(0.8),
		SMA20: fp(100),
		SMA50: fp(100),
	}
}

// volatileIndicators builds an IndicatorSet that classifies as
// volatile (low ADX + high ATR%).
func volatileIndicators() entity.IndicatorSet {
	return entity.IndicatorSet{
		ADX14: fp(15),
		ATR14: fp(4.0),
		SMA20: fp(100),
		SMA50: fp(100),
	}
}

// -------------- ProfileRouter validation --------------

func TestNewProfileRouter_RequiresName(t *testing.T) {
	_, err := NewProfileRouter(ProfileRouterInput{
		DefaultStrategy: &recordingStrategy{name: "d"},
	})
	if err == nil {
		t.Fatal("expected error on empty name")
	}
}

func TestNewProfileRouter_RequiresDefault(t *testing.T) {
	_, err := NewProfileRouter(ProfileRouterInput{Name: "r"})
	if err == nil {
		t.Fatal("expected error when DefaultStrategy is nil")
	}
}

func TestNewProfileRouter_RejectsUnknownRegimeKey(t *testing.T) {
	_, err := NewProfileRouter(ProfileRouterInput{
		Name:            "r",
		DefaultStrategy: &recordingStrategy{name: "d"},
		Overrides: map[entity.Regime]port.Strategy{
			entity.RegimeUnknown: &recordingStrategy{name: "x"}, // shadow Default
		},
	})
	if err == nil {
		t.Fatal("expected error on RegimeUnknown override (shadows Default)")
	}
}

func TestNewProfileRouter_RejectsNilOverride(t *testing.T) {
	_, err := NewProfileRouter(ProfileRouterInput{
		Name:            "r",
		DefaultStrategy: &recordingStrategy{name: "d"},
		Overrides: map[entity.Regime]port.Strategy{
			entity.RegimeBearTrend: nil,
		},
	})
	if err == nil {
		t.Fatal("expected error on nil override entry")
	}
}

// -------------- ProfileRouter dispatch --------------

// SelectStrategy is the deterministic surface; Evaluate ties together
// detector + dispatch and is covered separately below.
func TestSelectStrategy_DispatchTable(t *testing.T) {
	def := &recordingStrategy{name: "default"}
	bear := &recordingStrategy{name: "bear"}
	vol := &recordingStrategy{name: "vol"}
	r, err := NewProfileRouter(ProfileRouterInput{
		Name:            "r",
		DefaultStrategy: def,
		Overrides: map[entity.Regime]port.Strategy{
			entity.RegimeBearTrend: bear,
			entity.RegimeVolatile:  vol,
		},
	})
	if err != nil {
		t.Fatalf("NewProfileRouter: %v", err)
	}

	cases := []struct {
		regime entity.Regime
		want   port.Strategy
	}{
		{entity.RegimeUnknown, def},   // warmup → default
		{entity.RegimeBullTrend, def}, // not in overrides → default
		{entity.RegimeRange, def},     // not in overrides → default
		{entity.RegimeBearTrend, bear},
		{entity.RegimeVolatile, vol},
	}
	for _, c := range cases {
		got := r.SelectStrategy(c.regime)
		if got != c.want {
			t.Errorf("regime %q: got strategy %q, want %q", c.regime, got.Name(), c.want.Name())
		}
	}
}

// Evaluate must consult the detector, route to the child, and return
// the child's signal verbatim. Use bear indicators so the router picks
// the bear child, then assert the bear stub got the call (not default).
func TestEvaluate_RoutesByRegime(t *testing.T) {
	def := &recordingStrategy{name: "default"}
	bear := &recordingStrategy{name: "bear"}
	r, _ := NewProfileRouter(ProfileRouterInput{
		Name:            "r",
		DefaultStrategy: def,
		Overrides: map[entity.Regime]port.Strategy{
			entity.RegimeBearTrend: bear,
		},
	})

	in := bearIndicators()
	sig, err := r.Evaluate(context.Background(), &in, nil, 100, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig == nil {
		t.Fatal("Evaluate returned nil signal")
	}
	if sig.Reason != "bear" {
		t.Errorf("signal.Reason = %q, want %q (router did not delegate to bear child)", sig.Reason, "bear")
	}
	if def.callCount() != 0 || bear.callCount() != 1 {
		t.Errorf("call counts: default=%d bear=%d, want 0/1", def.callCount(), bear.callCount())
	}
	if r.CommittedRegime() != entity.RegimeBearTrend {
		t.Errorf("CommittedRegime = %q, want bear-trend", r.CommittedRegime())
	}
}

// During warmup (ADX missing) the detector emits Unknown — the router
// must route to the default strategy, not panic and not skip the bar.
func TestEvaluate_WarmupRoutesToDefault(t *testing.T) {
	def := &recordingStrategy{name: "default"}
	bear := &recordingStrategy{name: "bear"}
	r, _ := NewProfileRouter(ProfileRouterInput{
		Name:            "r",
		DefaultStrategy: def,
		Overrides:       map[entity.Regime]port.Strategy{entity.RegimeBearTrend: bear},
	})

	in := entity.IndicatorSet{} // no ADX, no ATR — warmup
	sig, err := r.Evaluate(context.Background(), &in, nil, 100, time.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Reason != "default" {
		t.Errorf("warmup signal.Reason = %q, want default", sig.Reason)
	}
}

func TestEvaluate_NilIndicatorsErrors(t *testing.T) {
	r, _ := NewProfileRouter(ProfileRouterInput{
		Name:            "r",
		DefaultStrategy: &recordingStrategy{name: "d"},
	})
	_, err := r.Evaluate(context.Background(), nil, nil, 100, time.Now())
	if !errors.Is(err, ErrIndicatorsRequired) {
		t.Fatalf("err = %v, want ErrIndicatorsRequired", err)
	}
}

func TestReset_ClearsDetectorState(t *testing.T) {
	def := &recordingStrategy{name: "default"}
	r, _ := NewProfileRouter(ProfileRouterInput{
		Name:            "r",
		DefaultStrategy: def,
	})
	in := trendingIndicators()
	_, _ = r.Evaluate(context.Background(), &in, nil, 100, time.Now())
	if r.CommittedRegime() != entity.RegimeBullTrend {
		t.Fatalf("setup: regime not committed, got %q", r.CommittedRegime())
	}
	r.Reset()
	if r.CommittedRegime() != entity.RegimeUnknown {
		t.Errorf("after Reset: regime = %q, want unknown", r.CommittedRegime())
	}
}

// -------------- Builder: BuildStrategyFromProfile --------------

// stubLoader resolves child profile names from an in-memory map and
// records every Load call so tests can assert the builder did not
// over-fetch.
type stubLoader struct {
	profiles map[string]*entity.StrategyProfile
	loaded   []string
	err      error
}

func (l *stubLoader) Load(name string) (*entity.StrategyProfile, error) {
	l.loaded = append(l.loaded, name)
	if l.err != nil {
		return nil, l.err
	}
	p, ok := l.profiles[name]
	if !ok {
		return nil, errors.New("not found: " + name)
	}
	return p, nil
}

// flatProfile returns a minimal valid (non-router) StrategyProfile.
func flatProfile(name string) *entity.StrategyProfile {
	return &entity.StrategyProfile{
		Name: name,
		Indicators: entity.IndicatorConfig{
			SMAShort: 20, SMALong: 50, RSIPeriod: 14,
			MACDFast: 12, MACDSlow: 26, MACDSignal: 9,
			BBPeriod: 20, BBMultiplier: 2.0, ATRPeriod: 14,
		},
		StanceRules: entity.StanceRulesConfig{RSIOversold: 30, RSIOverbought: 70},
	}
}

// routerProfile returns a router profile with the given default and
// optional overrides.
func routerProfile(name, def string, overrides map[string]string) *entity.StrategyProfile {
	return &entity.StrategyProfile{
		Name: name,
		RegimeRouting: &entity.RegimeRoutingConfig{
			Default:   def,
			Overrides: overrides,
		},
	}
}

// Non-router profiles must pass through to NewConfigurableStrategy
// with no loader call (the loader is irrelevant for flat profiles).
func TestBuildStrategyFromProfile_FlatProfileSkipsLoader(t *testing.T) {
	loader := &stubLoader{}
	got, err := BuildStrategyFromProfile(loader, flatProfile("flat"))
	if err != nil {
		t.Fatalf("BuildStrategyFromProfile: %v", err)
	}
	if _, ok := got.(*ConfigurableStrategy); !ok {
		t.Errorf("got type %T, want *ConfigurableStrategy", got)
	}
	if len(loader.loaded) != 0 {
		t.Errorf("loader was consulted for a flat profile: %v", loader.loaded)
	}
}

// Router profiles resolve every child via the loader and produce a
// *ProfileRouter. Each child name appears in loader.loaded exactly
// once (no over-fetch).
func TestBuildStrategyFromProfile_RouterResolvesChildren(t *testing.T) {
	loader := &stubLoader{
		profiles: map[string]*entity.StrategyProfile{
			"def":  flatProfile("def"),
			"bear": flatProfile("bear"),
		},
	}
	root := routerProfile("router", "def", map[string]string{
		"bear-trend": "bear",
	})
	got, err := BuildStrategyFromProfile(loader, root)
	if err != nil {
		t.Fatalf("BuildStrategyFromProfile: %v", err)
	}
	router, ok := got.(*ProfileRouter)
	if !ok {
		t.Fatalf("got type %T, want *ProfileRouter", got)
	}
	if router.Name() != "router" {
		t.Errorf("router.Name() = %q", router.Name())
	}
	// bear regime → bear child; bull (not in overrides) → default.
	bearChoice := router.SelectStrategy(entity.RegimeBearTrend)
	defChoice := router.SelectStrategy(entity.RegimeBullTrend)
	if bearChoice.Name() != "bear" {
		t.Errorf("bear choice name = %q", bearChoice.Name())
	}
	if defChoice.Name() != "def" {
		t.Errorf("default choice name = %q", defChoice.Name())
	}
	// Loader called exactly twice (default + one override), not for
	// regimes the router would never consult.
	if len(loader.loaded) != 2 {
		t.Errorf("loader.loaded = %v, want 2 calls", loader.loaded)
	}
}

// Depth-1 enforcement: a child profile that is itself a router must
// be rejected at build time, not silently treated as a deep router.
func TestBuildStrategyFromProfile_RejectsNestedRouter(t *testing.T) {
	loader := &stubLoader{
		profiles: map[string]*entity.StrategyProfile{
			"def":         flatProfile("def"),
			"nestedchild": flatProfile("nestedchild"),
			"nested":      routerProfile("nested", "nestedchild", nil),
		},
	}
	root := routerProfile("router", "def", map[string]string{
		"bear-trend": "nested",
	})
	_, err := BuildStrategyFromProfile(loader, root)
	if err == nil {
		t.Fatal("expected error on nested router child")
	}
	if !strings.Contains(err.Error(), "max depth") {
		t.Errorf("error message %q does not mention depth limit", err.Error())
	}
}

// Self-reference (default = router itself) would loop forever via
// loader.Load if not caught. Build must reject it at the top of the
// recursion so the loader.Load call never even fires for the cycle.
func TestBuildStrategyFromProfile_RejectsSelfReference(t *testing.T) {
	loader := &stubLoader{profiles: map[string]*entity.StrategyProfile{}}
	root := routerProfile("router", "router", nil)
	_, err := BuildStrategyFromProfile(loader, root)
	if err == nil {
		t.Fatal("expected error on router referencing itself")
	}
}

// Missing child file is a fatal config error, not a fall-through to
// "default" — the router would silently drop the regime otherwise.
func TestBuildStrategyFromProfile_MissingChildErrors(t *testing.T) {
	loader := &stubLoader{
		profiles: map[string]*entity.StrategyProfile{
			"def": flatProfile("def"),
			// no "bear" entry
		},
	}
	root := routerProfile("router", "def", map[string]string{"bear-trend": "bear"})
	_, err := BuildStrategyFromProfile(loader, root)
	if err == nil {
		t.Fatal("expected error on missing child profile")
	}
}

// Validate-side coverage: a router profile with empty
// Indicators/StanceRules/etc. must pass Validate (router profiles
// delegate, so those fields are unused).
func TestStrategyProfile_Validate_RouterProfileSkipsFieldChecks(t *testing.T) {
	p := *routerProfile("router", "child", nil)
	if err := p.Validate(); err != nil {
		t.Fatalf("router-only profile rejected by Validate: %v", err)
	}
}

// Conversely, a non-router profile that left Indicators empty must
// still be rejected — the router shortcut should not weaken existing
// profiles' validation.
func TestStrategyProfile_Validate_FlatProfileStillRequiresIndicators(t *testing.T) {
	p := entity.StrategyProfile{Name: "flat"} // no Indicators, no RegimeRouting
	if err := p.Validate(); err == nil {
		t.Fatal("flat profile with empty Indicators must fail Validate")
	}
}

// Validate rejects unknown regime keys in overrides so a typo like
// "bear-trnd" is caught at load time, not silently ignored at runtime.
func TestStrategyProfile_Validate_UnknownRegimeKeyRejected(t *testing.T) {
	p := *routerProfile("router", "def", map[string]string{
		"bear-trnd": "child", // typo
	})
	if err := p.Validate(); err == nil {
		t.Fatal("expected error on unknown regime key")
	}
}

// overrides set without default is almost certainly a typo — the
// writer meant to set both. Catch at Validate time.
func TestStrategyProfile_Validate_OverridesWithoutDefaultRejected(t *testing.T) {
	// Build a baseline flat profile (so the Indicators/StanceRules
	// field-level checks pass) and then attach a routing block that
	// has overrides but no default — the trailing guard inside the
	// flat-profile branch must catch this.
	p := *flatProfile("router")
	p.RegimeRouting = &entity.RegimeRoutingConfig{
		// Default deliberately empty so HasRouting() is false and the
		// router-shortcut path in Validate does not fire — but the
		// trailing "overrides without default" guard does.
		Overrides: map[string]string{"bear-trend": "child"},
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error on overrides without default")
	}
	if !strings.Contains(err.Error(), "default must be set") {
		t.Errorf("error message %q does not mention the missing default", err.Error())
	}
}
