package entity

import (
	"encoding/json"
	"math"
)

// finiteOrNil returns a pointer to v when v is a finite float64, or nil when
// v is NaN/±Inf. The custom MarshalJSON on MultiPeriodAggregate uses this so
// non-finite values become JSON `null` instead of breaking json.Marshal
// (which rejects them outright).
func finiteOrNil(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

// nilToNaN is the inverse of finiteOrNil for UnmarshalJSON: a nil pointer
// (JSON null or missing key) becomes NaN so the Go-side ruin semantics of
// MultiPeriodAggregate survive the JSON round-trip.
func nilToNaN(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

// jsonMarshal/jsonUnmarshal wrap the stdlib functions so the MarshalJSON /
// UnmarshalJSON methods on MultiPeriodAggregate stay readable.
func jsonMarshal(v any) ([]byte, error)      { return json.Marshal(v) }
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
