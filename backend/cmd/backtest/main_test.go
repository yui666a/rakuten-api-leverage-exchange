package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
)

// productionProfileJSON is an inline copy of profiles/production.json so the
// CLI tests don't depend on cwd. Keep in sync with the on-disk file.
const productionProfileJSON = `{
  "name": "production",
  "description": "test copy of production defaults",
  "indicators": {
    "sma_short": 20,
    "sma_long": 50,
    "rsi_period": 14,
    "macd_fast": 12,
    "macd_slow": 26,
    "macd_signal": 9,
    "bb_period": 20,
    "bb_multiplier": 2.0,
    "atr_period": 14
  },
  "stance_rules": {
    "rsi_oversold": 25,
    "rsi_overbought": 75,
    "sma_convergence_threshold": 0.001,
    "bb_squeeze_lookback": 5,
    "breakout_volume_ratio": 1.5
  },
  "signal_rules": {
    "trend_follow": {
      "enabled": true,
      "require_macd_confirm": true,
      "require_ema_cross": true,
      "rsi_buy_max": 70,
      "rsi_sell_min": 30
    },
    "contrarian": {
      "enabled": true,
      "rsi_entry": 30,
      "rsi_exit": 70,
      "macd_histogram_limit": 10
    },
    "breakout": {
      "enabled": true,
      "volume_ratio_min": 1.5,
      "require_macd_confirm": true
    }
  },
  "strategy_risk": {
    "stop_loss_percent": 5,
    "take_profit_percent": 10,
    "stop_loss_atr_multiplier": 0,
    "max_position_amount": 100000,
    "max_daily_loss": 50000
  },
  "htf_filter": {
    "enabled": true,
    "block_counter_trend": true,
    "alignment_boost": 0.1
  }
}`

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

// chdirTo temporarily changes cwd so loadProfileIfSet (which resolves
// "profiles/<name>.json" relative to cwd) finds the fixture. Uses t.Cleanup
// to restore on exit.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
}

// setupProfilesFixture writes profiles/<name>.json files under a temp
// directory and chdirs there. Returns the directory for convenience.
func setupProfilesFixture(t *testing.T, profiles map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	pdir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	for name, content := range profiles {
		path := filepath.Join(pdir, name+".json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	chdirTo(t, dir)
	return dir
}

func TestRunCommand_Profile_RunsToCompletion(t *testing.T) {
	setupProfilesFixture(t, map[string]string{"production": productionProfileJSON})
	csvPath := writeCSVForCLI(t)

	if err := runCommand([]string{"--profile", "production", "--data", csvPath}); err != nil {
		t.Fatalf("runCommand with valid profile: %v", err)
	}
}

func TestRunCommand_Profile_InvalidName_Errors(t *testing.T) {
	setupProfilesFixture(t, map[string]string{"production": productionProfileJSON})
	csvPath := writeCSVForCLI(t)

	err := runCommand([]string{"--profile", "../secret", "--data", csvPath})
	if err == nil {
		t.Fatal("expected error for profile name with traversal, got nil")
	}
	if !strings.Contains(err.Error(), "invalid profile name") {
		t.Errorf("expected 'invalid profile name' in error, got: %v", err)
	}
}

func TestRunCommand_Profile_Missing_Errors(t *testing.T) {
	// Fixture has ONLY production.json; request something else.
	setupProfilesFixture(t, map[string]string{"production": productionProfileJSON})
	csvPath := writeCSVForCLI(t)

	err := runCommand([]string{"--profile", "nonexistent_profile", "--data", csvPath})
	if err == nil {
		t.Fatal("expected error for missing profile, got nil")
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
