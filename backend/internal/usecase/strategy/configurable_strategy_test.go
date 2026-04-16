package strategy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// productionProfilePath walks up from this test file to the module root (go.mod)
// and appends profiles/production.json. We resolve it dynamically so the test
// works regardless of where `go test` is invoked from.
func productionProfilePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "profiles", "production.json")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test file")
		}
		dir = parent
	}
}

// productionProfile loads the on-disk production.json via the real loader so
// the tests cover the same path used by the CLI / API.
func productionProfile(t *testing.T) *entity.StrategyProfile {
	t.Helper()
	path := productionProfilePath(t)
	baseDir := filepath.Dir(path)
	loader := strategyprofile.NewLoader(baseDir)
	profile, err := loader.Load("production")
	if err != nil {
		t.Fatalf("load production profile: %v", err)
	}
	return profile
}

func TestConfigurableStrategy_Name(t *testing.T) {
	profile := productionProfile(t)
	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}
	if got, want := s.Name(), "production"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestConfigurableStrategy_InvalidProfile(t *testing.T) {
	// Zero value: fails Validate (empty Name, zero periods, etc.).
	invalid := &entity.StrategyProfile{}
	s, err := NewConfigurableStrategy(invalid)
	if err == nil {
		t.Fatal("expected error for invalid profile, got nil")
	}
	if s != nil {
		t.Errorf("expected nil strategy on error, got %+v", s)
	}

	// Nil profile is also rejected.
	s, err = NewConfigurableStrategy(nil)
	if err == nil {
		t.Fatal("expected error for nil profile, got nil")
	}
	if s != nil {
		t.Errorf("expected nil strategy on nil-profile error, got %+v", s)
	}
}

func TestConfigurableStrategy_NilIndicators(t *testing.T) {
	profile := productionProfile(t)
	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}
	signal, err := s.Evaluate(context.Background(), nil, nil, 0, time.Now())
	if err == nil {
		t.Fatal("expected error for nil indicators, got nil")
	}
	if !errors.Is(err, ErrIndicatorsRequired) {
		t.Errorf("expected ErrIndicatorsRequired, got %v", err)
	}
	if signal != nil {
		t.Errorf("expected nil signal on error, got %+v", signal)
	}
}

// buildDefaultStrategyFromProfile constructs a DefaultStrategy wired to mirror
// ConfigurableStrategy(profile) — a fresh stateless RuleBasedStanceResolver
// with default thresholds and a vanilla StrategyEngine. This is the baseline
// we assert equivalence against; any divergence means the profile has drifted
// from the hard-coded defaults (or the options-threading has broken).
func buildDefaultStrategyFromProfile(t *testing.T) *DefaultStrategy {
	t.Helper()
	resolver := usecase.NewRuleBasedStanceResolverWithOptions(nil, usecase.RuleBasedStanceResolverOptions{
		DisableOverride:    true,
		DisablePersistence: true,
	})
	engine := usecase.NewStrategyEngine(resolver)
	return NewDefaultStrategy(engine)
}

// indicatorScenario drives both strategies through the same inputs so we can
// compare signal output field-for-field.
type indicatorScenario struct {
	name       string
	indicators entity.IndicatorSet
	higherTF   *entity.IndicatorSet
	lastPrice  float64
}

// fpRef returns a pointer to v so scenarios can be expressed inline.
func fpRef(v float64) *float64 { return &v }

// bpRef returns a pointer to v so scenarios can be expressed inline.
func bpRef(v bool) *bool { return &v }

// scenariosForEquivalence exercises multiple signal branches: TREND_FOLLOW
// buy, CONTRARIAN sell, BREAKOUT buy, and a HOLD counter-trend case. All four
// must match between DefaultStrategy and ConfigurableStrategy(production).
func scenariosForEquivalence() []indicatorScenario {
	return []indicatorScenario{
		{
			name: "trend-follow buy",
			indicators: entity.IndicatorSet{
				SymbolID:    7,
				SMA20:       fpRef(5_100_000),
				SMA50:       fpRef(5_000_000),
				EMA12:       fpRef(5_100_000),
				EMA26:       fpRef(5_000_000),
				RSI14:       fpRef(55),
				Histogram:   fpRef(5),
				VolumeRatio: fpRef(1.0),
			},
			higherTF: &entity.IndicatorSet{
				SMA20: fpRef(5_100_000),
				SMA50: fpRef(5_000_000),
			},
			lastPrice: 5_100_000,
		},
		{
			name: "contrarian sell (RSI overbought triggers CONTRARIAN stance)",
			indicators: entity.IndicatorSet{
				SymbolID:    7,
				SMA20:       fpRef(5_100_000),
				SMA50:       fpRef(5_000_000),
				RSI14:       fpRef(82),
				Histogram:   fpRef(-2),
				VolumeRatio: fpRef(1.0),
				BBUpper:     fpRef(5_200_000),
				BBLower:     fpRef(5_000_000),
			},
			higherTF:  nil,
			lastPrice: 5_150_000,
		},
		{
			// BREAKOUT stance (RecentSqueeze=true, price > BBUpper, VolumeRatio
			// >= 1.5) must also route identically between the two strategies.
			// This closes the breakout-volume-ratio dual-path coverage gap
			// (the threshold appears in both StanceRulesConfig and
			// SignalRulesConfig and they must stay in sync for production.json).
			name: "breakout buy (BB squeeze release with volume confirmation)",
			indicators: entity.IndicatorSet{
				SymbolID:      7,
				SMA20:         fpRef(5_050_000),
				SMA50:         fpRef(5_000_000),
				EMA12:         fpRef(5_050_000),
				EMA26:         fpRef(5_000_000),
				RSI14:         fpRef(55), // neutral: avoids CONTRARIAN override
				Histogram:     fpRef(5),  // >= 0: survives BreakoutRequireMACDConfirm
				BBUpper:       fpRef(5_100_000),
				BBMiddle:      fpRef(5_050_000),
				BBLower:       fpRef(5_000_000),
				VolumeRatio:   fpRef(1.6), // >= 1.5 default BreakoutVolumeRatio
				RecentSqueeze: bpRef(true),
			},
			higherTF:  nil,
			lastPrice: 5_150_000, // strictly above BBUpper
		},
		{
			name: "hold: TF buy blocked by higher-TF downtrend",
			indicators: entity.IndicatorSet{
				SymbolID:    7,
				SMA20:       fpRef(5_100_000),
				SMA50:       fpRef(5_000_000),
				EMA12:       fpRef(5_100_000),
				EMA26:       fpRef(5_000_000),
				RSI14:       fpRef(55),
				Histogram:   fpRef(5),
				VolumeRatio: fpRef(1.0),
			},
			higherTF: &entity.IndicatorSet{
				SMA20: fpRef(4_900_000),
				SMA50: fpRef(5_000_000),
			},
			lastPrice: 5_100_000,
		},
	}
}

// TestConfigurableStrategy_EquivalentToDefault is the spec-mandated critical
// test: production.json must produce signals identical to DefaultStrategy
// across multiple representative branches.
func TestConfigurableStrategy_EquivalentToDefault(t *testing.T) {
	profile := productionProfile(t)
	configurable, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}
	def := buildDefaultStrategyFromProfile(t)

	now := time.Unix(1_700_000_000, 0)
	ctx := context.Background()

	for _, sc := range scenariosForEquivalence() {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			defSignal, err := def.Evaluate(ctx, &sc.indicators, sc.higherTF, sc.lastPrice, now)
			if err != nil {
				t.Fatalf("DefaultStrategy.Evaluate: %v", err)
			}
			cfgSignal, err := configurable.Evaluate(ctx, &sc.indicators, sc.higherTF, sc.lastPrice, now)
			if err != nil {
				t.Fatalf("ConfigurableStrategy.Evaluate: %v", err)
			}
			if defSignal == nil || cfgSignal == nil {
				t.Fatalf("got nil signal: default=%v configurable=%v", defSignal, cfgSignal)
			}
			if defSignal.Action != cfgSignal.Action {
				t.Errorf("Action mismatch: default=%s configurable=%s", defSignal.Action, cfgSignal.Action)
			}
			if defSignal.SymbolID != cfgSignal.SymbolID {
				t.Errorf("SymbolID mismatch: default=%d configurable=%d", defSignal.SymbolID, cfgSignal.SymbolID)
			}
			if defSignal.Reason != cfgSignal.Reason {
				t.Errorf("Reason mismatch: default=%q configurable=%q", defSignal.Reason, cfgSignal.Reason)
			}
			if defSignal.Confidence != cfgSignal.Confidence {
				t.Errorf("Confidence mismatch: default=%v configurable=%v", defSignal.Confidence, cfgSignal.Confidence)
			}
			if defSignal.Timestamp != cfgSignal.Timestamp {
				t.Errorf("Timestamp mismatch: default=%d configurable=%d", defSignal.Timestamp, cfgSignal.Timestamp)
			}
		})
	}
}

// TestConfigurableStrategy_DisabledTrendFollow proves the Enabled toggle
// actually routes through the engine: inputs that would normally produce a
// TREND_FOLLOW buy yield HOLD once trend_follow.enabled = false.
func TestConfigurableStrategy_DisabledTrendFollow(t *testing.T) {
	profile := productionProfile(t)
	profile.SignalRules.TrendFollow.Enabled = false

	s, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	// Same inputs as the trend-follow buy scenario above.
	indicators := entity.IndicatorSet{
		SymbolID:    7,
		SMA20:       fpRef(5_100_000),
		SMA50:       fpRef(5_000_000),
		EMA12:       fpRef(5_100_000),
		EMA26:       fpRef(5_000_000),
		RSI14:       fpRef(55),
		Histogram:   fpRef(5),
		VolumeRatio: fpRef(1.0),
	}
	signal, err := s.Evaluate(context.Background(), &indicators, nil, 5_100_000, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD with trend_follow.enabled=false, got %s (reason=%q)", signal.Action, signal.Reason)
	}
}

// TestConfigurableStrategy_CustomRSIThresholds verifies a profile-level
// threshold override actually changes the decision. RSI 65 passes the
// production RSIBuyMax=70 ceiling but is blocked by a tightened 60 ceiling.
func TestConfigurableStrategy_CustomRSIThresholds(t *testing.T) {
	profile := productionProfile(t)

	// Sanity: baseline (production, RSIBuyMax=70) produces a BUY.
	baseline, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy baseline: %v", err)
	}

	indicators := entity.IndicatorSet{
		SymbolID:    7,
		SMA20:       fpRef(5_100_000),
		SMA50:       fpRef(5_000_000),
		EMA12:       fpRef(5_100_000),
		EMA26:       fpRef(5_000_000),
		RSI14:       fpRef(65),
		Histogram:   fpRef(5),
		VolumeRatio: fpRef(1.0),
	}
	now := time.Unix(1_700_000_000, 0)

	baselineSignal, err := baseline.Evaluate(context.Background(), &indicators, nil, 5_100_000, now)
	if err != nil {
		t.Fatalf("baseline Evaluate: %v", err)
	}
	if baselineSignal.Action != entity.SignalActionBuy {
		t.Fatalf("baseline expected BUY (RSI 65 < 70), got %s (reason=%q)", baselineSignal.Action, baselineSignal.Reason)
	}

	// Tighten: RSI 65 is now above the RSIBuyMax=60 ceiling → HOLD.
	profile.SignalRules.TrendFollow.RSIBuyMax = 60
	strict, err := NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy strict: %v", err)
	}
	strictSignal, err := strict.Evaluate(context.Background(), &indicators, nil, 5_100_000, now)
	if err != nil {
		t.Fatalf("strict Evaluate: %v", err)
	}
	if strictSignal.Action != entity.SignalActionHold {
		t.Fatalf("strict expected HOLD (RSI 65 > 60), got %s (reason=%q)", strictSignal.Action, strictSignal.Reason)
	}
}
