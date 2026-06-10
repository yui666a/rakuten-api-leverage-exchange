package positionsize

import (
	"math"
	"testing"
)

func TestConfidenceScale(t *testing.T) {
	t.Parallel()
	// legacyScale reproduces the old inline formula (floor fixed at 0.5)
	// so we can assert ConfidenceScale(.., 0.5) is bit-equivalent.
	legacyScale := func(conf, min float64) float64 {
		if conf < min {
			conf = min
		}
		if conf > 1 {
			conf = 1
		}
		return 0.5 + 0.5*(conf-min)/(1.0-min)
	}

	cases := []struct {
		name             string
		conf, min, floor float64
		want             float64
	}{
		{"legacy equiv mid", 0.6, 0.3, 0.5, legacyScale(0.6, 0.3)},
		{"legacy equiv high", 0.9, 0.3, 0.5, legacyScale(0.9, 0.3)},
		{"legacy equiv at min", 0.3, 0.3, 0.5, legacyScale(0.3, 0.3)},
		{"floor unset (0) falls back to 0.5", 0.6, 0.3, 0.0, legacyScale(0.6, 0.3)},
		{"floor 1.0 disables scaling", 0.3, 0.3, 1.0, 1.0},
		{"floor 0.7 maps min->0.7", 0.3, 0.3, 0.7, 0.7},
		{"conf>=1 full size", 1.0, 0.3, 0.5, 1.0},
		{"conf<min clamps to floor", 0.1, 0.3, 0.5, 0.5},
		{"min<=0 disables (no anchor)", 0.6, 0.0, 0.5, 1.0},
		{"min>=1 disables (no range)", 0.6, 1.0, 0.5, 1.0},
		{"NaN confidence fails closed to floor", math.NaN(), 0.3, 0.5, 0.5},
		{"NaN with unset floor fails closed to 0.5", math.NaN(), 0.3, 0.0, 0.5},
		{"-Inf confidence fails closed to floor", math.Inf(-1), 0.3, 0.5, 0.5},
		{"+Inf confidence is full size", math.Inf(1), 0.3, 0.5, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConfidenceScale(tc.conf, tc.min, tc.floor)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("ConfidenceScale(%v,%v,%v) = %v, want %v", tc.conf, tc.min, tc.floor, got, tc.want)
			}
		})
	}
}
