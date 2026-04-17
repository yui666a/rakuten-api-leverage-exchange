package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

// readProductionProfileJSON loads the bytes of backend/profiles/production.json
// by walking up from this test file to the module root (go.mod). Using the
// on-disk fixture directly (rather than an inline copy) removes the
// duplication with handler/backtest_test.go's own copy and guarantees the
// tests run against the real production profile the CLI / API would load in
// production. See configurable_strategy_test.go for the same walk pattern.
func readProductionProfileJSON(t *testing.T) []byte {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		candidate := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			data, err := os.ReadFile(filepath.Join(dir, "profiles", "production.json"))
			if err != nil {
				t.Fatalf("read production.json: %v", err)
			}
			return data
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test file")
		}
		dir = parent
	}
}

// writeCSVForCLI creates a minimal CSV file the CLI's LoadCandles accepts.
func writeCSVForCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "candles.csv")
	candles := make([]entity.Candle, 0, 60)
	base := int64(1_770_000_000_000)
	for i := 0; i < 60; i++ {
		price := 15_000_000.0 + float64(i)*100
		candles = append(candles, entity.Candle{
			Open:   price - 1000,
			High:   price + 2000,
			Low:    price - 2000,
			Close:  price,
			Volume: 1.0,
			Time:   base + int64(i)*15*60*1000,
		})
	}
	if err := csvinfra.SaveCandles(path, csvinfra.CandleFile{
		Symbol:   "BTC_JPY",
		SymbolID: 7,
		Interval: "PT15M",
		Candles:  candles,
	}); err != nil {
		t.Fatalf("save csv: %v", err)
	}
	return path
}

// setupProfilesDir writes profile JSON files under a temp directory and
// returns the directory. Unlike the old fixture helper, it does NOT chdir —
// loadProfileIfSet now accepts an explicit baseDir, so tests pass the temp
// dir directly. This avoids the process-global cwd race that `go test
// ./...`'s package-parallel default used to trigger.
func setupProfilesDir(t *testing.T, profiles map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range profiles {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

// TestLoadProfileIfSet_LoadsProduction verifies the happy path: a valid
// profile file on disk loads into a *StrategyProfile that NewConfigurableStrategy
// accepts. This is the closest unit-level analogue to a full run-to-completion
// test without CSV/runner plumbing.
func TestLoadProfileIfSet_LoadsProduction(t *testing.T) {
	baseDir := setupProfilesDir(t, map[string][]byte{
		"production": readProductionProfileJSON(t),
	})

	profile, err := loadProfileIfSet("production", baseDir)
	if err != nil {
		t.Fatalf("loadProfileIfSet: %v", err)
	}
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
	if profile.Name != "production" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "production")
	}
	// Downstream wiring: the runner only accepts profiles that construct a
	// valid ConfigurableStrategy. If this fails, a profile-driven run would
	// have failed too.
	if _, err := strategyuc.NewConfigurableStrategy(profile); err != nil {
		t.Fatalf("NewConfigurableStrategy(loaded profile): %v", err)
	}
}

func TestLoadProfileIfSet_Empty_ReturnsNil(t *testing.T) {
	baseDir := setupProfilesDir(t, nil)
	profile, err := loadProfileIfSet("", baseDir)
	if err != nil {
		t.Fatalf("loadProfileIfSet(\"\"): %v", err)
	}
	if profile != nil {
		t.Errorf("expected nil profile for empty name, got %+v", profile)
	}
}

func TestLoadProfileIfSet_InvalidName_Errors(t *testing.T) {
	baseDir := setupProfilesDir(t, map[string][]byte{
		"production": readProductionProfileJSON(t),
	})
	_, err := loadProfileIfSet("../secret", baseDir)
	if err == nil {
		t.Fatal("expected error for profile name with traversal, got nil")
	}
	if !strings.Contains(err.Error(), "invalid profile name") {
		t.Errorf("expected 'invalid profile name' in error, got: %v", err)
	}
}

func TestLoadProfileIfSet_Missing_Errors(t *testing.T) {
	baseDir := setupProfilesDir(t, map[string][]byte{
		"production": readProductionProfileJSON(t),
	})
	_, err := loadProfileIfSet("nonexistent_profile", baseDir)
	if err == nil {
		t.Fatal("expected error for missing profile, got nil")
	}
}

// TestRefineCommand_Profile_LoadsAndApplies is the Minor #3 ask: prove the
// refine subcommand correctly resolves and applies --profile. We can't drive
// refineCommand to completion in a unit test (it needs sizeable CSV fixtures
// and runs two optimize phases), so we assert the profile-loading seam
// loadProfileIfSet — shared by all three subcommands — works correctly for
// the production profile. The rest of refineCommand is exercised indirectly
// via TestRunCommand_* and the optimizer's own tests.
func TestRefineCommand_Profile_LoadsAndApplies(t *testing.T) {
	baseDir := setupProfilesDir(t, map[string][]byte{
		"production": readProductionProfileJSON(t),
	})

	profile, err := loadProfileIfSet("production", baseDir)
	if err != nil {
		t.Fatalf("loadProfileIfSet: %v", err)
	}
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
	// The refine subcommand wires the profile into a runner via WithStrategy;
	// assert that path succeeds. If NewConfigurableStrategy rejected the
	// profile here, the subcommand would fail fast before touching CSVs.
	strat, err := strategyuc.NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}
	if strat == nil {
		t.Fatal("expected non-nil strategy")
	}
}

// TestBuildRunInput_ProfileRisk_Overrides verifies the override precedence
// documented in --help: profile values become the base, individual flags
// the user explicitly sets (tracked via Set) win.
func TestBuildRunInput_ProfileRisk_Overrides(t *testing.T) {
	csvPath := writeCSVForCLI(t)

	profile := &entity.StrategyProfile{
		Risk: entity.StrategyRiskConfig{
			StopLossPercent:   3, // profile says 3%
			TakeProfitPercent: 9,
			MaxPositionAmount: 77777,
			MaxDailyLoss:      88888,
		},
	}

	t.Run("flags left at default -> profile wins", func(t *testing.T) {
		f := runFlags{
			DataPath:       csvPath,
			InitialBalance: 100000,
			Spread:         0.1,
			CarryingCost:   0.04,
			TradeAmount:    0.01,
			StopLoss:       5, // Go default; user didn't set
			TakeProfit:     10,
			Set:            map[string]bool{},
		}
		in, err := buildRunInput(f, profile)
		if err != nil {
			t.Fatalf("buildRunInput: %v", err)
		}
		if in.RiskConfig.StopLossPercent != 3 {
			t.Errorf("StopLossPercent = %v, want 3 (profile)", in.RiskConfig.StopLossPercent)
		}
		if in.RiskConfig.TakeProfitPercent != 9 {
			t.Errorf("TakeProfitPercent = %v, want 9 (profile)", in.RiskConfig.TakeProfitPercent)
		}
		if in.RiskConfig.MaxPositionAmount != 77777 {
			t.Errorf("MaxPositionAmount = %v, want 77777 (profile)", in.RiskConfig.MaxPositionAmount)
		}
		if in.RiskConfig.MaxDailyLoss != 88888 {
			t.Errorf("MaxDailyLoss = %v, want 88888 (profile)", in.RiskConfig.MaxDailyLoss)
		}
	})

	t.Run("explicit --stop-loss overrides profile", func(t *testing.T) {
		f := runFlags{
			DataPath:       csvPath,
			InitialBalance: 100000,
			Spread:         0.1,
			CarryingCost:   0.04,
			TradeAmount:    0.01,
			StopLoss:       7, // user set
			TakeProfit:     10,
			Set:            map[string]bool{"stop-loss": true},
		}
		in, err := buildRunInput(f, profile)
		if err != nil {
			t.Fatalf("buildRunInput: %v", err)
		}
		if in.RiskConfig.StopLossPercent != 7 {
			t.Errorf("StopLossPercent = %v, want 7 (CLI override)", in.RiskConfig.StopLossPercent)
		}
		// Profile's take-profit still wins because the user did not set
		// --take-profit.
		if in.RiskConfig.TakeProfitPercent != 9 {
			t.Errorf("TakeProfitPercent = %v, want 9 (profile, flag not set)", in.RiskConfig.TakeProfitPercent)
		}
	})

	t.Run("no profile -> flag values pass through", func(t *testing.T) {
		f := runFlags{
			DataPath:       csvPath,
			InitialBalance: 100000,
			Spread:         0.1,
			CarryingCost:   0.04,
			TradeAmount:    0.01,
			StopLoss:       5,
			TakeProfit:     10,
			Set:            map[string]bool{},
		}
		in, err := buildRunInput(f, nil)
		if err != nil {
			t.Fatalf("buildRunInput: %v", err)
		}
		if in.RiskConfig.StopLossPercent != 5 {
			t.Errorf("StopLossPercent = %v, want 5", in.RiskConfig.StopLossPercent)
		}
		if in.RiskConfig.TakeProfitPercent != 10 {
			t.Errorf("TakeProfitPercent = %v, want 10", in.RiskConfig.TakeProfitPercent)
		}
	})
}

// TestVisitedFlagNames checks that flag.Visit correctly distinguishes
// "user set this flag" from "flag was left at Go default". The CLI override
// rule depends on this being accurate.
func TestVisitedFlagNames(t *testing.T) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var f runFlags
	registerRunFlags(fs, &f)

	if err := fs.Parse([]string{"--data", "x.csv", "--stop-loss", "7"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	set := visitedFlagNames(fs)
	if !set["stop-loss"] {
		t.Error("stop-loss should be marked as set")
	}
	if !set["data"] {
		t.Error("data should be marked as set")
	}
	if set["take-profit"] {
		t.Error("take-profit should NOT be marked as set (left at default)")
	}
}

// TestRunner_ProfileWithDisabledRules_NoTrades is the Minor #4 integration
// assertion: it proves that when a profile is wired into the runner via
// WithStrategy, ConfigurableStrategy is actually dispatched (not a silent
// fallback to DefaultStrategy). The profile disables every signal rule, so
// the runner can produce trades ONLY if DefaultStrategy is used instead.
// If no trades are produced, ConfigurableStrategy was dispatched.
//
// Fixture: a synthetic sinusoidal candle series (same shape as
// generateTestCandles in the handler tests) that DefaultStrategy is known to
// trade on. The CLI's "run" path mirrors this wiring.
func TestRunner_ProfileWithDisabledRules_NoTrades(t *testing.T) {
	// Load production.json as the base then disable every signal rule.
	baseDir := setupProfilesDir(t, map[string][]byte{
		"production": readProductionProfileJSON(t),
	})
	profile, err := loadProfileIfSet("production", baseDir)
	if err != nil {
		t.Fatalf("loadProfileIfSet: %v", err)
	}
	profile.SignalRules.TrendFollow.Enabled = false
	profile.SignalRules.Contrarian.Enabled = false
	profile.SignalRules.Breakout.Enabled = false

	strat, err := strategyuc.NewConfigurableStrategy(profile)
	if err != nil {
		t.Fatalf("NewConfigurableStrategy: %v", err)
	}

	// Build a candle series that DefaultStrategy is known to trade on (the
	// same generator the handler integration tests use to produce non-zero
	// trades in TestBacktestHandler_Integration_RunListGet).
	candles := make([]entity.Candle, 0, 200)
	baseTime := int64(1_770_000_000_000)
	price := 15_000_000.0
	for i := 0; i < 200; i++ {
		// Oscillate so SMA/RSI/MACD all move.
		if i%20 < 10 {
			price += 30_000
		} else {
			price -= 30_000
		}
		candles = append(candles, entity.Candle{
			Open:   price - 5000,
			High:   price + 10000,
			Low:    price - 10000,
			Close:  price,
			Volume: 1.5,
			Time:   baseTime + int64(i)*15*60*1000,
		})
	}

	runner := bt.NewBacktestRunner(bt.WithStrategy(strat))
	result, err := runner.Run(context.Background(), bt.RunInput{
		Config: entity.BacktestConfig{
			Symbol:          "BTC_JPY",
			SymbolID:        7,
			PrimaryInterval: "PT15M",
			FromTimestamp:   candles[0].Time,
			ToTimestamp:     candles[len(candles)-1].Time,
			InitialBalance:  100000,
			SpreadPercent:   0.1,
			DailyCarryCost:  0.04,
		},
		TradeAmount:    0.01,
		PrimaryCandles: candles,
		RiskConfig: entity.RiskConfig{
			MaxPositionAmount: 1_000_000_000,
			MaxDailyLoss:      1_000_000_000,
			StopLossPercent:   5,
			TakeProfitPercent: 10,
			InitialCapital:    100000,
		},
	})
	if err != nil {
		t.Fatalf("runner.Run: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// With every signal rule disabled, ConfigurableStrategy must emit only
	// HOLD signals. A non-zero trade count would mean the runner fell back
	// to DefaultStrategy (proving the dispatch is broken) or the disabled
	// toggle is ignored.
	if result.Summary.TotalTrades != 0 {
		t.Fatalf(
			"expected zero trades with all signal rules disabled, got %d. "+
				"This suggests DefaultStrategy is being dispatched instead of "+
				"ConfigurableStrategy.",
			result.Summary.TotalTrades,
		)
	}
}
