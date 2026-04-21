package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseGridInline_ValidPaths(t *testing.T) {
	pf := paramFlags{
		"signal_rules.contrarian.stoch_entry_max=0,10,20",
		"strategy_risk.stop_loss_percent=3,5,7",
	}
	got, err := parseGridInline(pf)
	if err != nil {
		t.Fatalf("parseGridInline: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 axes, got %d", len(got))
	}
	if got[0].Path != "signal_rules.contrarian.stoch_entry_max" {
		t.Fatalf("axis 0 path: %s", got[0].Path)
	}
	if !reflect.DeepEqual(got[0].Values, []float64{0, 10, 20}) {
		t.Fatalf("axis 0 values: %v", got[0].Values)
	}
	if !reflect.DeepEqual(got[1].Values, []float64{3, 5, 7}) {
		t.Fatalf("axis 1 values: %v", got[1].Values)
	}
}

func TestParseGridInline_RejectsBadSpec(t *testing.T) {
	bads := []string{
		"no-equal-sign",
		"=empty-path",
		"path=",
		"path=1,,3",
		"path=abc",
	}
	for _, b := range bads {
		if _, err := parseGridInline(paramFlags{b}); err == nil {
			t.Fatalf("expected error for %q, got nil", b)
		}
	}
}

func TestReadGridFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grid.json")
	content := `[
		{"path": "signal_rules.contrarian.adx_max", "values": [0, 15, 25]},
		{"path": "strategy_risk.take_profit_percent", "values": [2, 4, 6]}
	]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readGridFile(path)
	if err != nil {
		t.Fatalf("readGridFile: %v", err)
	}
	if len(got) != 2 || got[0].Path != "signal_rules.contrarian.adx_max" {
		t.Fatalf("unexpected grid: %+v", got)
	}
}

func TestResolveWalkForwardGrid_FileAndInlineAreMutuallyExclusive(t *testing.T) {
	_, err := resolveWalkForwardGrid(paramFlags{"foo=1,2"}, "some.json")
	if err == nil {
		t.Fatal("expected error when both --grid and --grid-file are set")
	}
}

func TestResolveWalkForwardGrid_EmptyIsNil(t *testing.T) {
	got, err := resolveWalkForwardGrid(nil, "")
	if err != nil {
		t.Fatalf("no-args should be allowed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil (baseline-only); got %+v", got)
	}
}

func TestParseWFDate_RoundTrip(t *testing.T) {
	ts, err := parseWFDate("2024-03-15")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ts.Year() != 2024 || ts.Month() != 3 || ts.Day() != 15 {
		t.Fatalf("unexpected date: %v", ts)
	}
}

func TestParseWFDate_InvalidRejected(t *testing.T) {
	if _, err := parseWFDate("not-a-date"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := parseWFDate(""); err == nil {
		t.Fatal("expected error for empty input")
	}
}
