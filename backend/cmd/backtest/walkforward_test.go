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
	got, strs, err := parseGridInline(pf)
	if err != nil {
		t.Fatalf("parseGridInline: %v", err)
	}
	if len(strs) != 0 {
		t.Fatalf("expected no string axes, got %d", len(strs))
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 numeric axes, got %d", len(got))
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

func TestParseGridInline_StringAxis(t *testing.T) {
	pf := paramFlags{"htf_filter.mode=ema,ichimoku"}
	nums, strs, err := parseGridInline(pf)
	if err != nil {
		t.Fatalf("parseGridInline: %v", err)
	}
	if len(nums) != 0 {
		t.Fatalf("expected no numeric axes, got %d", len(nums))
	}
	if len(strs) != 1 {
		t.Fatalf("expected 1 string axis, got %d", len(strs))
	}
	if strs[0].Path != "htf_filter.mode" {
		t.Fatalf("axis path: %s", strs[0].Path)
	}
	if !reflect.DeepEqual(strs[0].Values, []string{"ema", "ichimoku"}) {
		t.Fatalf("axis values: %v", strs[0].Values)
	}
}

func TestParseGridInline_MixedNumericAndStringAxes(t *testing.T) {
	pf := paramFlags{
		"strategy_risk.stop_loss_percent=4,5,6",
		"htf_filter.mode=ema,ichimoku",
	}
	nums, strs, err := parseGridInline(pf)
	if err != nil {
		t.Fatalf("parseGridInline: %v", err)
	}
	if len(nums) != 1 || len(strs) != 1 {
		t.Fatalf("expected 1 numeric + 1 string axis, got %d + %d", len(nums), len(strs))
	}
	if nums[0].Path != "strategy_risk.stop_loss_percent" {
		t.Fatalf("numeric axis: %+v", nums[0])
	}
	if strs[0].Path != "htf_filter.mode" {
		t.Fatalf("string axis: %+v", strs[0])
	}
}

func TestParseGridInline_RejectsBadSpec(t *testing.T) {
	bads := []string{
		"no-equal-sign",
		"=empty-path",
		"path=",
		"path=1,,3",
	}
	for _, b := range bads {
		if _, _, err := parseGridInline(paramFlags{b}); err == nil {
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
	got, strs, err := readGridFile(path)
	if err != nil {
		t.Fatalf("readGridFile: %v", err)
	}
	if len(strs) != 0 {
		t.Fatalf("expected 0 string axes, got %d", len(strs))
	}
	if len(got) != 2 || got[0].Path != "signal_rules.contrarian.adx_max" {
		t.Fatalf("unexpected grid: %+v", got)
	}
}

func TestReadGridFile_StringAxis(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grid.json")
	content := `[
		{"path": "htf_filter.mode", "values": ["ema", "ichimoku"]},
		{"path": "strategy_risk.stop_loss_percent", "values": [5, 8]}
	]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nums, strs, err := readGridFile(path)
	if err != nil {
		t.Fatalf("readGridFile: %v", err)
	}
	if len(nums) != 1 || nums[0].Path != "strategy_risk.stop_loss_percent" {
		t.Fatalf("numeric: %+v", nums)
	}
	if len(strs) != 1 || strs[0].Path != "htf_filter.mode" {
		t.Fatalf("string: %+v", strs)
	}
	if !reflect.DeepEqual(strs[0].Values, []string{"ema", "ichimoku"}) {
		t.Fatalf("string values: %v", strs[0].Values)
	}
}

func TestReadGridFile_RejectsMixedAxis(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grid.json")
	content := `[{"path": "htf_filter.mode", "values": ["ema", 42]}]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := readGridFile(path); err == nil {
		t.Fatal("expected error for mixed-type axis")
	}
}

func TestResolveWalkForwardGrid_FileAndInlineAreMutuallyExclusive(t *testing.T) {
	_, _, err := resolveWalkForwardGrid(paramFlags{"foo=1,2"}, "some.json")
	if err == nil {
		t.Fatal("expected error when both --grid and --grid-file are set")
	}
}

func TestResolveWalkForwardGrid_EmptyIsNil(t *testing.T) {
	nums, strs, err := resolveWalkForwardGrid(nil, "")
	if err != nil {
		t.Fatalf("no-args should be allowed: %v", err)
	}
	if nums != nil || strs != nil {
		t.Fatalf("expected nil (baseline-only); got nums=%v strs=%v", nums, strs)
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
