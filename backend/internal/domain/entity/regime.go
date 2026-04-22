package entity

// Regime is the high-level market state the Strategy layer can branch on.
// The four labels correspond one-to-one to the regimes the PDCA cycle28-37
// finalists were specialised for: bull-trend / bear-trend favour the
// aggressive sl14_tf60_35 lineage, range / volatile favour the defensive
// sl6_tr30_tp6_tf60_35 lineage. RegimeUnknown is emitted while indicators
// are still warming up so callers can fall through to a baseline profile
// instead of guessing a side.
type Regime string

const (
	RegimeUnknown   Regime = ""
	RegimeBullTrend Regime = "bull-trend"
	RegimeBearTrend Regime = "bear-trend"
	RegimeRange     Regime = "range"
	RegimeVolatile  Regime = "volatile"
)

// IsKnown reports whether r is a real regime label (not the warmup-only
// RegimeUnknown sentinel). Centralised so callers do not hard-code the
// "" comparison.
func (r Regime) IsKnown() bool {
	return r != RegimeUnknown
}

// RegimeClassification is one detector emission. ADXValue / ATRPercent /
// CloudPosition are the raw inputs the detector saw, copied into the
// snapshot so logs and tests can explain *why* a Regime was chosen
// without re-deriving from the IndicatorSet (which may have been
// discarded by the time logs are read).
//
// Confidence is 0..1: 1.0 means every classifier dimension agreed,
// lower values mean the regime is the best-of-N pick but the call is
// close. Strategy callers can use it to widen the safety margin (e.g.
// reduce trade size when confidence < 0.5) but the routing decision
// itself is binary on Regime.
type RegimeClassification struct {
	Regime        Regime  `json:"regime"`
	Confidence    float64 `json:"confidence"`
	ADXValue      float64 `json:"adxValue"`
	ATRPercent    float64 `json:"atrPercent"`    // ATR / lastPrice * 100, in % units
	CloudPosition string  `json:"cloudPosition"` // "above" | "below" | "inside" | "" when Ichimoku missing
	Timestamp     int64   `json:"timestamp"`
}
