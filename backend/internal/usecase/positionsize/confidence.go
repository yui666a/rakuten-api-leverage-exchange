package positionsize

import "math"

// ConfidenceScale maps confidence in [minConfidence, 1.0] to a lot multiplier
// in [floor, 1.0] linearly. It is the single source of truth for how signal
// confidence shrinks the per-trade lot; both the risk_pct path (Sizer.Compute)
// and the legacy fixed-amount caller (cmd/pipeline.go) route through here so
// the formula can never drift between sites.
//
//   - confidence is NaN / -Inf   → floor  (fail closed: smallest lot)
//   - confidence >= 1.0          → 1.0  (full size)
//   - minConfidence not in (0,1) → 1.0  (scaling disabled; nothing to map)
//   - floor <= 0                 → 0.5  (legacy default; 0 is the "unset" sentinel)
//   - floor >= 1.0               → 1.0  (scaling disabled by config)
//   - otherwise → floor + (1-floor) * (confidence-minConfidence)/(1-minConfidence)
func ConfidenceScale(confidence, minConfidence, floor float64) float64 {
	// Normalise floor first so the NaN fail-closed path returns a sane value.
	if floor <= 0 {
		floor = 0.5 // legacy default
	}
	// Fail closed on non-finite confidence (NaN / -Inf): smallest lot.
	if math.IsNaN(confidence) || math.IsInf(confidence, -1) {
		return floor
	}
	// No usable anchor range → scaling disabled.
	if minConfidence <= 0 || minConfidence >= 1 {
		return 1.0
	}
	if floor >= 1.0 {
		return 1.0 // scaling disabled by config
	}
	conf := confidence
	if conf >= 1.0 {
		return 1.0
	}
	if conf < minConfidence {
		conf = minConfidence
	}
	return floor + (1.0-floor)*(conf-minConfidence)/(1.0-minConfidence)
}
