package regime

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// fp returns a *float64 from a literal. Pointer-based optional fields
// are everywhere in IndicatorSet; this just keeps the test bodies
// readable.
func fp(v float64) *float64 { return &v }

// indicatorsWith builds an IndicatorSet with ADX/ATR set and the rest
// nil — the two required inputs. SMA20/SMA50 are optional direction
// inputs, set when the test wants the SMA-cross fallback to fire.
func indicatorsWith(adx, atr float64) entity.IndicatorSet {
	return entity.IndicatorSet{
		ADX14:     fp(adx),
		ATR14:     fp(atr),
		Timestamp: 1700000000000,
	}
}

func htfWithSMA(sma20, sma50 float64) *entity.IndicatorSet {
	return &entity.IndicatorSet{SMA20: fp(sma20), SMA50: fp(sma50)}
}

func htfWithCloud(senkouA, senkouB float64) *entity.IndicatorSet {
	return &entity.IndicatorSet{
		Ichimoku: &entity.IchimokuSnapshot{SenkouA: fp(senkouA), SenkouB: fp(senkouB)},
	}
}

// -------------- single-bar classification --------------

func TestClassifyOnce_BullTrend_FromCloudAbove(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 1}) // disable dwell so we read the first bar
	got := d.Classify(indicatorsWith(35, 1.0), htfWithCloud(95, 90), 100)
	if got.Regime != entity.RegimeBullTrend {
		t.Fatalf("regime = %q, want bull-trend (above strong cloud, ADX 35)", got.Regime)
	}
	if got.Confidence < 0.75 {
		t.Fatalf("confidence = %v, want >= 0.75 (above + ADX strong)", got.Confidence)
	}
	if got.CloudPosition != "above" {
		t.Fatalf("cloud = %q", got.CloudPosition)
	}
}

func TestClassifyOnce_BearTrend_FromCloudBelow(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 1})
	got := d.Classify(indicatorsWith(35, 1.0), htfWithCloud(110, 105), 100)
	if got.Regime != entity.RegimeBearTrend {
		t.Fatalf("regime = %q, want bear-trend", got.Regime)
	}
	if got.CloudPosition != "below" {
		t.Fatalf("cloud = %q", got.CloudPosition)
	}
}

func TestClassifyOnce_BullTrend_FromHTFSMACross_NoCloud(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 1})
	got := d.Classify(indicatorsWith(25, 1.0), htfWithSMA(110, 100), 100)
	if got.Regime != entity.RegimeBullTrend {
		t.Fatalf("regime = %q, want bull-trend (SMA20>SMA50 fallback)", got.Regime)
	}
	if got.CloudPosition != "" {
		t.Fatalf("cloud should be empty without ichimoku, got %q", got.CloudPosition)
	}
}

func TestClassifyOnce_BearTrend_FromHTFSMACross_NoCloud(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 1})
	got := d.Classify(indicatorsWith(25, 1.0), htfWithSMA(95, 100), 100)
	if got.Regime != entity.RegimeBearTrend {
		t.Fatalf("regime = %q, want bear-trend", got.Regime)
	}
}

func TestClassifyOnce_Volatile_HighATR_LowADX(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 1})
	got := d.Classify(indicatorsWith(15, 4.0), nil, 100) // ATR 4% on 100 = 4.0
	if got.Regime != entity.RegimeVolatile {
		t.Fatalf("regime = %q, want volatile (ATR%% 4.0, ADX 15)", got.Regime)
	}
	if got.ATRPercent != 4.0 {
		t.Fatalf("ATRPercent = %v, want 4.0", got.ATRPercent)
	}
}

func TestClassifyOnce_Range_LowATR_LowADX(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 1})
	got := d.Classify(indicatorsWith(12, 0.8), nil, 100)
	if got.Regime != entity.RegimeRange {
		t.Fatalf("regime = %q, want range (calm + chop)", got.Regime)
	}
	if got.Confidence < 0.75 {
		t.Fatalf("confidence = %v, deeply-calm range should score >= 0.75", got.Confidence)
	}
}

func TestClassifyOnce_Unknown_ADXMissing(t *testing.T) {
	d := NewDetector(DefaultConfig())
	in := entity.IndicatorSet{ATR14: fp(1.0)} // no ADX
	got := d.Classify(in, nil, 100)
	if got.Regime != entity.RegimeUnknown {
		t.Fatalf("regime = %q, want unknown when ADX missing", got.Regime)
	}
}

func TestClassifyOnce_Unknown_ATRMissing(t *testing.T) {
	d := NewDetector(DefaultConfig())
	in := entity.IndicatorSet{ADX14: fp(30)}
	got := d.Classify(in, nil, 100)
	if got.Regime != entity.RegimeUnknown {
		t.Fatalf("regime = %q, want unknown when ATR missing", got.Regime)
	}
}

func TestClassifyOnce_Unknown_LastPriceZero(t *testing.T) {
	d := NewDetector(DefaultConfig())
	got := d.Classify(indicatorsWith(30, 1.0), nil, 0)
	if got.Regime != entity.RegimeUnknown {
		t.Fatalf("regime = %q, want unknown when lastPrice <= 0", got.Regime)
	}
}

// classifyOnce direction fallback: ADX strong but no Ichimoku and no
// SMAs anywhere → no direction → falls through to volatility-based
// classification rather than guessing a side.
func TestClassifyOnce_TrendStrongNoDirection_FallsToRangeOrVolatile(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 1})
	got := d.Classify(indicatorsWith(40, 0.5), nil, 100)
	if got.Regime == entity.RegimeBullTrend || got.Regime == entity.RegimeBearTrend {
		t.Fatalf("regime = %q; without direction inputs the detector must not guess a side", got.Regime)
	}
}

// classifyDirection prefers Ichimoku cloud over SMA cross when both
// are present, so a contradiction ("cloud above" + "SMA bear cross")
// resolves to the cloud's direction.
func TestClassifyOnce_DirectionPrefersCloudOverSMA(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 1})
	htf := &entity.IndicatorSet{
		SMA20:    fp(95),
		SMA50:    fp(100),
		Ichimoku: &entity.IchimokuSnapshot{SenkouA: fp(95), SenkouB: fp(90)}, // cloud below price 100
	}
	got := d.Classify(indicatorsWith(35, 1.0), htf, 100)
	if got.Regime != entity.RegimeBullTrend {
		t.Fatalf("regime = %q; cloud=above must outvote SMA bear cross", got.Regime)
	}
}

// -------------- hysteresis --------------

// Switching from one committed regime to a new candidate must require
// HysteresisBars consecutive matches; a single-bar opposite blip should
// not move the committed state.
func TestApplyHysteresis_RequiresMinimumDwell(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 3})
	// Establish bull-trend baseline.
	for i := 0; i < 5; i++ {
		d.Classify(indicatorsWith(30, 1.0), htfWithCloud(95, 90), 100)
	}
	if d.Committed() != entity.RegimeBullTrend {
		t.Fatalf("baseline regime = %q, want bull-trend", d.Committed())
	}
	// Single bear-trend bar — must NOT switch yet.
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(110, 105), 100)
	if d.Committed() != entity.RegimeBullTrend {
		t.Fatalf("regime flipped after 1 opposite bar: got %q", d.Committed())
	}
	// Two more bear bars → 3 consecutive → switch.
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(110, 105), 100)
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(110, 105), 100)
	if d.Committed() != entity.RegimeBearTrend {
		t.Fatalf("regime did not switch after 3 consecutive opposite bars: got %q", d.Committed())
	}
}

// A single opposite bar surrounded by reaffirmations should leave the
// committed regime untouched and the candidate counter must reset
// each time the candidate diverges.
func TestApplyHysteresis_NonConsecutiveCandidatesResetCounter(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 3})
	for i := 0; i < 5; i++ {
		d.Classify(indicatorsWith(30, 1.0), htfWithCloud(95, 90), 100)
	}
	// bear, bull(reaffirm), bear, bull, bear — never 3 bears in a row.
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(110, 105), 100)
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(95, 90), 100)
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(110, 105), 100)
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(95, 90), 100)
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(110, 105), 100)
	if d.Committed() != entity.RegimeBullTrend {
		t.Fatalf("regime flipped despite no 3 consecutive opposite bars: got %q", d.Committed())
	}
}

// First bar after warmup commits immediately — there is no "thinking"
// dead window between RegimeUnknown and the first real classification.
func TestApplyHysteresis_FirstRealBarCommitsImmediately(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 5})
	got := d.Classify(indicatorsWith(30, 1.0), htfWithCloud(95, 90), 100)
	if got.Regime != entity.RegimeBullTrend {
		t.Fatalf("first real bar did not commit: got %q", got.Regime)
	}
}

// Returning to RegimeUnknown (warmup loss / missing inputs) must
// short-circuit hysteresis so a router does not keep trading on a
// stale regime when indicators stop arriving.
func TestApplyHysteresis_UnknownResetsImmediately(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 3})
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(95, 90), 100)
	if d.Committed() != entity.RegimeBullTrend {
		t.Fatalf("setup failed: %q", d.Committed())
	}
	d.Classify(entity.IndicatorSet{}, nil, 100) // ADX missing → unknown
	if d.Committed() != entity.RegimeUnknown {
		t.Fatalf("regime did not reset to unknown: got %q", d.Committed())
	}
}

func TestReset_ClearsAllState(t *testing.T) {
	d := NewDetector(Config{HysteresisBars: 3})
	d.Classify(indicatorsWith(30, 1.0), htfWithCloud(95, 90), 100)
	d.Reset()
	if d.Committed() != entity.RegimeUnknown {
		t.Fatalf("Reset did not clear committed: %q", d.Committed())
	}
	// After reset, the next bar must commit immediately again (no
	// stale candidate counter).
	got := d.Classify(indicatorsWith(30, 1.0), htfWithCloud(110, 105), 100)
	if got.Regime != entity.RegimeBearTrend {
		t.Fatalf("post-reset regime = %q, want bear-trend immediately", got.Regime)
	}
}

// -------------- config defaults --------------

func TestNewDetector_ZeroConfigUsesDefaults(t *testing.T) {
	d := NewDetector(Config{})
	if d.cfg.TrendADXMin != 20 {
		t.Errorf("TrendADXMin = %v, want 20 default", d.cfg.TrendADXMin)
	}
	if d.cfg.VolatileATRPercentMin != 2.5 {
		t.Errorf("VolatileATRPercentMin = %v, want 2.5 default", d.cfg.VolatileATRPercentMin)
	}
	if d.cfg.HysteresisBars != 3 {
		t.Errorf("HysteresisBars = %v, want 3 default", d.cfg.HysteresisBars)
	}
}

func TestNewDetector_PartialConfigOverridesPerField(t *testing.T) {
	d := NewDetector(Config{TrendADXMin: 30}) // only override one field
	if d.cfg.TrendADXMin != 30 {
		t.Errorf("TrendADXMin override lost: %v", d.cfg.TrendADXMin)
	}
	if d.cfg.VolatileATRPercentMin != 2.5 {
		t.Errorf("VolatileATRPercentMin should default: %v", d.cfg.VolatileATRPercentMin)
	}
}

// -------------- regime entity --------------

func TestRegime_IsKnown(t *testing.T) {
	cases := map[entity.Regime]bool{
		entity.RegimeUnknown:   false,
		entity.RegimeBullTrend: true,
		entity.RegimeBearTrend: true,
		entity.RegimeRange:     true,
		entity.RegimeVolatile:  true,
		entity.Regime("typo"):  true, // IsKnown is "non-empty", not "valid label"
	}
	for r, want := range cases {
		if r.IsKnown() != want {
			t.Errorf("Regime(%q).IsKnown() = %v, want %v", r, r.IsKnown(), want)
		}
	}
}

// IsValidLabel is the strict-validation predicate used by config
// loading; it differs from IsKnown by rejecting typos.
func TestRegime_IsValidLabel(t *testing.T) {
	cases := map[entity.Regime]bool{
		entity.RegimeUnknown:       false, // sentinel, never valid as a key
		entity.RegimeBullTrend:     true,
		entity.RegimeBearTrend:     true,
		entity.RegimeRange:         true,
		entity.RegimeVolatile:      true,
		entity.Regime("typo"):      false, // unknown string -> invalid
		entity.Regime("Bull"):      false, // case-sensitive
		entity.Regime("bear-trnd"): false,
	}
	for r, want := range cases {
		if r.IsValidLabel() != want {
			t.Errorf("Regime(%q).IsValidLabel() = %v, want %v", r, r.IsValidLabel(), want)
		}
	}
}
