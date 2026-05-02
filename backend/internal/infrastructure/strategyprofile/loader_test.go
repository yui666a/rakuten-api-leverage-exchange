package strategyprofile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validProfileJSON is the spec §3.2 fixture, inlined to avoid disk deps.
const validProfileJSON = `{
  "name": "ltc_aggressive_v3",
  "description": "LTC向け攻めの短期戦略",
  "indicators": {
    "sma_short": 10,
    "sma_long": 30,
    "rsi_period": 14,
    "macd_fast": 12,
    "macd_slow": 26,
    "macd_signal": 9,
    "bb_period": 20,
    "bb_multiplier": 2.0,
    "atr_period": 14
  },
  "stance_rules": {
    "rsi_oversold": 20,
    "rsi_overbought": 80,
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

// writeProfile writes the given JSON content to <baseDir>/<name>.json and
// returns the full path.
func writeProfile(t *testing.T, baseDir, name, content string) string {
	t.Helper()
	path := filepath.Join(baseDir, name+".json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	return path
}

func TestLoader_Load_Valid(t *testing.T) {
	tmp := t.TempDir()
	writeProfile(t, tmp, "production", validProfileJSON)

	loader := NewLoader(tmp)
	profile, err := loader.Load("production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if profile.Name != "ltc_aggressive_v3" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "ltc_aggressive_v3")
	}
	if profile.Indicators.SMAShort != 10 || profile.Indicators.SMALong != 30 {
		t.Errorf("SMA short/long = %d/%d, want 10/30", profile.Indicators.SMAShort, profile.Indicators.SMALong)
	}
	if profile.Risk.MaxPositionAmount != 100000 {
		t.Errorf("Risk.MaxPositionAmount = %v, want 100000", profile.Risk.MaxPositionAmount)
	}
	if !profile.HTFFilter.BlockCounterTrend {
		t.Errorf("HTFFilter.BlockCounterTrend = false, want true")
	}
	if !profile.SignalRules.TrendFollow.RequireEMACross {
		t.Errorf("SignalRules.TrendFollow.RequireEMACross = false, want true")
	}
}

// TestLoader_Load_NameMismatch documents the (intentional) behaviour that
// the on-disk filename and the in-JSON `name` field may differ. Callers
// decide how to interpret the mismatch — the loader does not enforce it.
func TestLoader_Load_NameMismatch(t *testing.T) {
	tmp := t.TempDir()
	// File is called "snapshot.json" but internal name is "ltc_aggressive_v3".
	writeProfile(t, tmp, "snapshot", validProfileJSON)

	loader := NewLoader(tmp)
	profile, err := loader.Load("snapshot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Name != "ltc_aggressive_v3" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "ltc_aggressive_v3")
	}
}

func TestLoader_Load_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	writeProfile(t, tmp, "bad", `{"name": "oops" `) // unclosed brace

	loader := NewLoader(tmp)
	_, err := loader.Load("bad")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("expected error to mention profile name, got %v", err)
	}
}

func TestLoader_Load_UnknownField(t *testing.T) {
	tmp := t.TempDir()
	// Add a spurious top-level key.
	badJSON := strings.Replace(
		validProfileJSON,
		`"name": "ltc_aggressive_v3",`,
		`"name": "ltc_aggressive_v3", "surprise": 123,`,
		1,
	)
	writeProfile(t, tmp, "typo", badJSON)

	loader := NewLoader(tmp)
	_, err := loader.Load("typo")
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "surprise") {
		t.Errorf("expected error to mention unknown field 'surprise', got %v", err)
	}
}

func TestLoader_Load_FileMissing(t *testing.T) {
	tmp := t.TempDir()
	loader := NewLoader(tmp)
	_, err := loader.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected errors.Is fs.ErrNotExist, got %v", err)
	}
}

func TestLoader_Load_ValidationFailure(t *testing.T) {
	tmp := t.TempDir()
	// Set atr_period to a negative value — trips Validate() but decodes fine.
	badJSON := strings.Replace(
		validProfileJSON,
		`"atr_period": 14`,
		`"atr_period": -1`,
		1,
	)
	writeProfile(t, tmp, "badparams", badJSON)

	loader := NewLoader(tmp)
	_, err := loader.Load("badparams")
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "atr_period") {
		t.Errorf("expected validation error to mention atr_period, got %v", err)
	}
}

func TestLoader_Load_InvalidName(t *testing.T) {
	tmp := t.TempDir()
	loader := NewLoader(tmp)
	_, err := loader.Load("../etc/passwd")
	if err == nil {
		t.Fatal("expected error for invalid name, got nil")
	}
	if !errors.Is(err, ErrInvalidProfileName) {
		t.Errorf("expected errors.Is ErrInvalidProfileName, got %v", err)
	}
}

func TestParseProfile_Valid(t *testing.T) {
	profile, err := ParseProfile(strings.NewReader(validProfileJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Name != "ltc_aggressive_v3" {
		t.Errorf("profile.Name = %q, want %q", profile.Name, "ltc_aggressive_v3")
	}
}

func TestParseProfile_UnknownField(t *testing.T) {
	bad := `{"name": "x", "mystery": 1}`
	_, err := ParseProfile(strings.NewReader(bad))
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

// TestLoader_List returns the metadata summary for every *.json file in
// the base directory, sorted by name, and skips non-JSON files + sub-
// directories. Invalid profiles are still included (with an error
// description) so the FE picker can surface the problem instead of
// silently dropping the row.
func TestLoader_List(t *testing.T) {
	tmp := t.TempDir()
	// Two valid profiles + one bad (validation fails) + one non-JSON file
	// that must be ignored.
	writeProfile(t, tmp, "alpha", strings.Replace(validProfileJSON, `"ltc_aggressive_v3"`, `"alpha"`, 1))
	writeProfile(t, tmp, "beta", strings.Replace(validProfileJSON, `"ltc_aggressive_v3"`, `"beta"`, 1))
	badJSON := strings.Replace(validProfileJSON, `"atr_period": 14`, `"atr_period": -1`, 1)
	writeProfile(t, tmp, "gamma_broken", badJSON)
	// Non-JSON should be ignored.
	if err := os.WriteFile(filepath.Join(tmp, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	loader := NewLoader(tmp)
	summaries, err := loader.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries (alpha, beta, gamma_broken), got %d: %+v", len(summaries), summaries)
	}
	// Sorted by name.
	if summaries[0].Name != "alpha" || summaries[1].Name != "beta" {
		t.Errorf("unexpected ordering: %+v", summaries)
	}
	if summaries[2].Name != "gamma_broken" {
		t.Errorf("broken profile should still appear with filename as Name, got %q", summaries[2].Name)
	}
	if !strings.Contains(summaries[2].Description, "load error") {
		t.Errorf("broken profile description should flag load error, got %q", summaries[2].Description)
	}
}

// TestLoader_List_EmptyDir returns an empty slice (not nil) so the FE
// does not crash on a fresh install with no profiles yet.
func TestLoader_List_EmptyDir(t *testing.T) {
	tmp := t.TempDir()
	loader := NewLoader(tmp)
	summaries, err := loader.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if summaries == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries, got %d", len(summaries))
	}
}

// TestLoader_Load_ProductionLTC60kReadsPR4Fields ensures the on-disk
// production profile parses cleanly and exposes the BookGate / EntryCooldown
// values introduced in PR4. This guards against silent dropouts (e.g. someone
// removing a JSON tag and the field defaulting to 0).
func TestLoader_Load_ProductionLTC60kReadsPR4Fields(t *testing.T) {
	loader := NewLoader("../../../profiles")
	profile, err := loader.Load("production_ltc_60k")
	if err != nil {
		t.Fatalf("Load production_ltc_60k: %v", err)
	}
	if profile.Risk.MaxSlippageBps != 15 {
		t.Errorf("Risk.MaxSlippageBps = %v, want 15", profile.Risk.MaxSlippageBps)
	}
	if profile.Risk.MaxBookSidePct != 20 {
		t.Errorf("Risk.MaxBookSidePct = %v, want 20", profile.Risk.MaxBookSidePct)
	}
	if profile.Risk.EntryCooldownSec != 60 {
		t.Errorf("Risk.EntryCooldownSec = %v, want 60", profile.Risk.EntryCooldownSec)
	}
}
