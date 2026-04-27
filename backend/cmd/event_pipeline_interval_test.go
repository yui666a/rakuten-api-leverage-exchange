package main

import (
	"testing"
)

// TestPrimaryIntervalOrDefault pins the legacy PT15M behaviour: callers
// (including main.go before the env var is set) leave PrimaryInterval
// empty in EventDrivenPipelineConfig, and the pipeline must keep
// behaving as it did before this PR.
func TestPrimaryIntervalOrDefault(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty falls back to PT15M", in: "", want: "PT15M"},
		{name: "explicit PT5M is preserved", in: "PT5M", want: "PT5M"},
		{name: "explicit PT15M is preserved", in: "PT15M", want: "PT15M"},
		{name: "exotic interval is preserved verbatim (validation belongs to live source)", in: "PT3M", want: "PT3M"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := primaryIntervalOrDefault(tc.in); got != tc.want {
				t.Errorf("primaryIntervalOrDefault(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHigherTFIntervalOrDefault(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "PT1H"},
		{in: "PT4H", want: "PT4H"},
	}
	for _, tc := range tests {
		if got := higherTFIntervalOrDefault(tc.in); got != tc.want {
			t.Errorf("higherTFIntervalOrDefault(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNewEventDrivenPipeline_FillsIntervalDefaults verifies the constructor
// applies fallbacks rather than leaving the runtime fields empty (which
// would crash LiveSource when interval-to-duration parsing fails).
func TestNewEventDrivenPipeline_FillsIntervalDefaults(t *testing.T) {
	p := NewEventDrivenPipeline(
		EventDrivenPipelineConfig{SymbolID: 7},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if p.primaryInterval != "PT15M" {
		t.Errorf("primaryInterval = %q, want PT15M (legacy default)", p.primaryInterval)
	}
	if p.higherTFInterval != "PT1H" {
		t.Errorf("higherTFInterval = %q, want PT1H (legacy default)", p.higherTFInterval)
	}
}

func TestNewEventDrivenPipeline_HonoursConfiguredIntervals(t *testing.T) {
	p := NewEventDrivenPipeline(
		EventDrivenPipelineConfig{
			SymbolID:         7,
			PrimaryInterval:  "PT5M",
			HigherTFInterval: "PT4H",
		},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if p.primaryInterval != "PT5M" {
		t.Errorf("primaryInterval = %q, want PT5M", p.primaryInterval)
	}
	if p.higherTFInterval != "PT4H" {
		t.Errorf("higherTFInterval = %q, want PT4H", p.higherTFInterval)
	}
}
