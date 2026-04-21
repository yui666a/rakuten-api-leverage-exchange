package entity

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// TestMultiPeriodAggregate_JSONRoundTripFinite verifies that finite values
// round-trip identically through JSON.
func TestMultiPeriodAggregate_JSONRoundTripFinite(t *testing.T) {
	want := MultiPeriodAggregate{
		GeomMeanReturn:  0.05,
		ReturnStdDev:    0.02,
		WorstReturn:     -0.01,
		BestReturn:      0.10,
		WorstDrawdown:   0.15,
		AllPositive:     false,
		RobustnessScore: 0.03,
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "null") {
		t.Fatalf("finite values should not contain null: %s", string(b))
	}
	var got MultiPeriodAggregate
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.GeomMeanReturn != want.GeomMeanReturn ||
		got.ReturnStdDev != want.ReturnStdDev ||
		got.WorstReturn != want.WorstReturn ||
		got.BestReturn != want.BestReturn ||
		got.WorstDrawdown != want.WorstDrawdown ||
		got.AllPositive != want.AllPositive ||
		got.RobustnessScore != want.RobustnessScore {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

// TestMultiPeriodAggregate_NaNMarshalsAsNull covers the ruin path: Go-side
// NaN must become JSON null (so Marshal cannot fail) and come back as NaN
// so downstream scoring logic preserves the "ruined" semantics.
func TestMultiPeriodAggregate_NaNMarshalsAsNull(t *testing.T) {
	a := MultiPeriodAggregate{
		GeomMeanReturn:  math.NaN(),
		ReturnStdDev:    0.04,
		WorstReturn:     -1.2,
		BestReturn:      0.03,
		WorstDrawdown:   0.50,
		AllPositive:     false,
		RobustnessScore: math.NaN(),
	}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal should succeed on NaN (got error: %v)", err)
	}
	s := string(b)
	if !strings.Contains(s, `"geomMeanReturn":null`) {
		t.Fatalf("expected geomMeanReturn to serialise as null, got %s", s)
	}
	if !strings.Contains(s, `"robustnessScore":null`) {
		t.Fatalf("expected robustnessScore to serialise as null, got %s", s)
	}

	var back MultiPeriodAggregate
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !math.IsNaN(back.GeomMeanReturn) {
		t.Fatalf("GeomMeanReturn round-trip lost NaN: %v", back.GeomMeanReturn)
	}
	if !math.IsNaN(back.RobustnessScore) {
		t.Fatalf("RobustnessScore round-trip lost NaN: %v", back.RobustnessScore)
	}
	if back.ReturnStdDev != 0.04 {
		t.Fatalf("ReturnStdDev round-trip failed: %v", back.ReturnStdDev)
	}
}

// TestMultiPeriodAggregate_InfMarshalsAsNull covers the same failure mode
// for ±Inf which json.Marshal also rejects.
func TestMultiPeriodAggregate_InfMarshalsAsNull(t *testing.T) {
	a := MultiPeriodAggregate{
		GeomMeanReturn:  math.Inf(1),
		RobustnessScore: math.Inf(-1),
	}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal should succeed on Inf (got error: %v)", err)
	}
	if !strings.Contains(string(b), `"geomMeanReturn":null`) {
		t.Fatalf("expected null serialisation, got %s", string(b))
	}
}
