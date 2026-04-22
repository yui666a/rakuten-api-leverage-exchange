// Package regime classifies the current market state into one of the four
// labels strategy routing branches on (bull-trend / bear-trend / range /
// volatile / unknown). The detector reads from the existing IndicatorSet
// — no new indicator calculation — so a regime emission is "free" once a
// pipeline tick has produced the indicators it already produces today.
//
// Why a separate package: the Strategy port can later wire a router that
// owns one detector and N strategies, but the classifier itself must
// stay free of any Strategy/profile imports. Putting it under usecase/
// keeps it side-effect free and unit-testable without spinning up a
// backtest runner.
//
// The classification is deliberately rule-based (no learning) so the
// behaviour is auditable from the PDCA cycle records and a single
// thresholds change can be diffed and rolled back. See
// docs/pdca/2026-04-22_cycle28-37.md for the regime decomposition that
// motivated the four-label space.
package regime

import (
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// Config bundles the thresholds and hysteresis depth so the routing
// layer can tune them per profile pair without recompiling. All fields
// are zero-default safe — a Detector built with Config{} uses the
// defaults documented next to each field.
type Config struct {
	// TrendADXMin is the ADX value at or above which a directional
	// regime (bull-trend / bear-trend) becomes eligible. Below this, the
	// market is treated as ranging or volatile. Default 20 (Wilder's
	// "trend present" threshold).
	TrendADXMin float64

	// VolatileATRPercentMin is the ATR/price threshold (in percent
	// units, e.g. 2.5 = 2.5%) at or above which a non-trending bar is
	// classified as volatile rather than range. Default 2.5%, calibrated
	// against the LTC 15m 2022 crash window where ATR/price routinely
	// exceeded 3% while ADX stayed in the 15-20 chop band.
	VolatileATRPercentMin float64

	// HysteresisBars is the minimum number of consecutive bars a new
	// candidate regime must persist before the detector switches to it.
	// 0 disables hysteresis. Default 3 (one strategy bar = one tick;
	// 3 bars on 15m candles = 45 minutes minimum dwell time).
	HysteresisBars int
}

// DefaultConfig returns the production-tuned thresholds. Unit tests
// build their own Config so a future tuning change does not silently
// break the test fixtures.
func DefaultConfig() Config {
	return Config{
		TrendADXMin:           20,
		VolatileATRPercentMin: 2.5,
		HysteresisBars:        3,
	}
}

// Detector is stateful: the hysteresis logic needs to remember the
// committed regime and how many bars the current candidate has
// persisted for. One Detector per backtest run / live pipeline.
type Detector struct {
	cfg Config

	committed  entity.Regime
	candidate  entity.Regime
	candidateN int
}

// NewDetector builds a Detector with the supplied Config. Zero-valued
// Config fields fall back to DefaultConfig values.
func NewDetector(cfg Config) *Detector {
	d := DefaultConfig()
	if cfg.TrendADXMin > 0 {
		d.TrendADXMin = cfg.TrendADXMin
	}
	if cfg.VolatileATRPercentMin > 0 {
		d.VolatileATRPercentMin = cfg.VolatileATRPercentMin
	}
	if cfg.HysteresisBars > 0 {
		d.HysteresisBars = cfg.HysteresisBars
	}
	return &Detector{cfg: d, committed: entity.RegimeUnknown}
}

// Classify reads the supplied indicator snapshots and returns the
// committed regime (after hysteresis). lastPrice is the close used to
// normalise ATR into a percentage and to read Ichimoku cloud position.
//
// The Ichimoku snapshot, when present, comes from the higher timeframe;
// when nil the detector falls back to SMA20/SMA50 cross for direction.
// This matches the existing htfTrendDirection convention so a regime
// router can reuse the same higher-TF wiring the HTF filter uses.
//
// When critical inputs (ADX or ATR) are missing the classifier emits
// RegimeUnknown so callers can route to a baseline profile rather than
// guessing a direction during warmup.
func (d *Detector) Classify(indicators entity.IndicatorSet, higherTF *entity.IndicatorSet, lastPrice float64) entity.RegimeClassification {
	out := entity.RegimeClassification{
		Timestamp:     indicators.Timestamp,
		CloudPosition: classifyCloudPosition(higherTF, lastPrice),
	}

	// ADX and ATR are the load-bearing inputs. If either is missing the
	// classifier cannot speak — committing Unknown also resets the
	// candidate counter so the next valid bar starts the dwell clock
	// from one, not from a stale partial count.
	if indicators.ADX14 == nil || indicators.ATR14 == nil || lastPrice <= 0 {
		d.committed = entity.RegimeUnknown
		d.candidate = entity.RegimeUnknown
		d.candidateN = 0
		out.Regime = entity.RegimeUnknown
		return out
	}

	adx := *indicators.ADX14
	atrPct := *indicators.ATR14 / lastPrice * 100
	out.ADXValue = adx
	out.ATRPercent = atrPct

	candidate, conf := d.classifyOnce(indicators, higherTF, lastPrice, adx, atrPct, out.CloudPosition)
	out.Confidence = conf

	committed := d.applyHysteresis(candidate)
	out.Regime = committed
	return out
}

// classifyOnce produces a regime from one bar's inputs, ignoring
// hysteresis. Split out so the test suite can exercise the rule logic
// without juggling Detector state.
//
// Confidence is a coarse 4-step ladder (0.25 / 0.5 / 0.75 / 1.0) rather
// than a continuous score: it captures "barely qualified" vs. "every
// dimension agreed" without the false precision a continuous score
// would imply for a hand-tuned classifier.
func (d *Detector) classifyOnce(
	indicators entity.IndicatorSet,
	higherTF *entity.IndicatorSet,
	lastPrice, adx, atrPct float64,
	cloud string,
) (entity.Regime, float64) {
	trendStrong := adx >= d.cfg.TrendADXMin
	highVol := atrPct >= d.cfg.VolatileATRPercentMin

	if trendStrong {
		direction := classifyDirection(indicators, higherTF, cloud)
		switch direction {
		case "up":
			conf := 0.5
			if cloud == "above" {
				conf += 0.25
			}
			if adx >= d.cfg.TrendADXMin*1.5 {
				conf += 0.25
			}
			if conf > 1.0 {
				conf = 1.0
			}
			return entity.RegimeBullTrend, conf
		case "down":
			conf := 0.5
			if cloud == "below" {
				conf += 0.25
			}
			if adx >= d.cfg.TrendADXMin*1.5 {
				conf += 0.25
			}
			if conf > 1.0 {
				conf = 1.0
			}
			return entity.RegimeBearTrend, conf
		}
		// Direction was undetermined despite trend strength — treat as
		// volatile/range based on ATR rather than guessing a direction.
	}

	if highVol {
		conf := 0.5
		if atrPct >= d.cfg.VolatileATRPercentMin*1.5 {
			conf += 0.25
		}
		if !trendStrong {
			conf += 0.25
		}
		if conf > 1.0 {
			conf = 1.0
		}
		return entity.RegimeVolatile, conf
	}

	conf := 0.5
	if adx < d.cfg.TrendADXMin*0.75 {
		conf += 0.25
	}
	if atrPct < d.cfg.VolatileATRPercentMin*0.75 {
		conf += 0.25
	}
	if conf > 1.0 {
		conf = 1.0
	}
	return entity.RegimeRange, conf
}

// applyHysteresis decides whether to commit the new candidate. The rule
// is intentionally asymmetric: switching INTO a regime requires
// HysteresisBars consecutive matches; switching to RegimeUnknown is
// never throttled because warmup loss should be reflected immediately.
func (d *Detector) applyHysteresis(candidate entity.Regime) entity.Regime {
	if !candidate.IsKnown() {
		d.committed = entity.RegimeUnknown
		d.candidate = entity.RegimeUnknown
		d.candidateN = 0
		return entity.RegimeUnknown
	}

	// First-ever real classification: commit immediately so the very
	// first non-warmup bar already routes (no dead window where the
	// detector is "thinking").
	if !d.committed.IsKnown() {
		d.committed = candidate
		d.candidate = candidate
		d.candidateN = 1
		return d.committed
	}

	if candidate == d.committed {
		// Reaffirmation: keep the committed regime, reset the dwell
		// counter so a brief opposite blip does not chip away at the
		// switch threshold.
		d.candidate = candidate
		d.candidateN = 0
		return d.committed
	}

	if candidate == d.candidate {
		d.candidateN++
	} else {
		d.candidate = candidate
		d.candidateN = 1
	}
	if d.candidateN >= d.cfg.HysteresisBars {
		d.committed = candidate
		d.candidateN = 0
	}
	return d.committed
}

// Reset puts the Detector back into the warmup state. Used when a
// backtest runner starts a new window so the previous window's hysteresis
// state does not bleed into the new one.
func (d *Detector) Reset() {
	d.committed = entity.RegimeUnknown
	d.candidate = entity.RegimeUnknown
	d.candidateN = 0
}

// Committed exposes the current committed regime without taking a new
// classification. Useful for tests and for the router to peek at
// state without forcing a re-classify.
func (d *Detector) Committed() entity.Regime { return d.committed }

// classifyCloudPosition returns "above" / "below" / "inside" when the
// higher-timeframe Ichimoku snapshot is complete enough, or the empty
// string when it is missing or warming up. The semantics intentionally
// mirror htfTrendDirection in usecase/strategy.go.
func classifyCloudPosition(higherTF *entity.IndicatorSet, lastPrice float64) string {
	if higherTF == nil || higherTF.Ichimoku == nil {
		return ""
	}
	ic := higherTF.Ichimoku
	if ic.SenkouA == nil || ic.SenkouB == nil {
		return ""
	}
	upper := *ic.SenkouA
	lower := *ic.SenkouB
	if lower > upper {
		upper, lower = lower, upper
	}
	switch {
	case lastPrice > upper:
		return "above"
	case lastPrice < lower:
		return "below"
	default:
		return "inside"
	}
}

// classifyDirection picks between "up" / "down" / "" using Ichimoku
// cloud position when available, falling back to higher-TF SMA20/SMA50
// cross, falling back to primary-TF SMA cross. Returns the empty
// string when no source has enough data — the caller treats that as
// "trend strong but undirected" and routes to volatile/range instead.
func classifyDirection(indicators entity.IndicatorSet, higherTF *entity.IndicatorSet, cloud string) string {
	switch cloud {
	case "above":
		return "up"
	case "below":
		return "down"
	}
	if higherTF != nil && higherTF.SMA20 != nil && higherTF.SMA50 != nil {
		if *higherTF.SMA20 > *higherTF.SMA50 {
			return "up"
		}
		return "down"
	}
	if indicators.SMA20 != nil && indicators.SMA50 != nil {
		if *indicators.SMA20 > *indicators.SMA50 {
			return "up"
		}
		return "down"
	}
	return ""
}
