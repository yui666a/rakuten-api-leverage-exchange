package backtest

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// TestCalculateIndicatorSet_PeriodsDriveValues is the PR-B wiring guard:
// it proves that calculateIndicatorSet actually consumes the IndicatorConfig
// argument, rather than ignoring it the way SetBBSqueezeLookback was
// originally a silent no-op (cycle43). Two runs on the same trending
// candle series with different SMA / EMA / RSI / BB / ATR / VolumeSMA
// periods must produce numerically distinguishable values.
//
// We use a strong linear uptrend so longer lookbacks lag the price
// noticeably more than shorter ones — every period axis becomes a visible
// signal. A passive guard (just checking that the values are non-nil)
// would have missed cycle43-style "the field is set but the period is
// hardcoded" regressions.
func TestCalculateIndicatorSet_PeriodsDriveValues(t *testing.T) {
	candles := buildTrendingCandles(120)

	defaults := calculateIndicatorSet(42, candles, entity.IndicatorConfig{}, 0)
	scaled := calculateIndicatorSet(42, candles, entity.IndicatorConfig{
		SMAShort:        60,
		SMALong:         100,
		EMAFast:         36,
		EMASlow:         78,
		RSIPeriod:       42,
		MACDFast:        24,
		MACDSlow:        52,
		MACDSignal:      18,
		BBPeriod:        60,
		BBMultiplier:    2.0,
		ATRPeriod:       42,
		VolumeSMAPeriod: 60,
	}, 0)

	cases := []struct {
		name string
		// short/long extractor — both must be non-nil and differ between
		// the two runs to count as proof the period is honoured.
		gotDefault, gotScaled *float64
	}{
		{"SMAShort", defaults.SMAShort, scaled.SMAShort},
		{"SMALong", defaults.SMALong, scaled.SMALong},
		{"EMAFast", defaults.EMAFast, scaled.EMAFast},
		{"EMASlow", defaults.EMASlow, scaled.EMASlow},
		{"RSI", defaults.RSI, scaled.RSI},
		{"MACDLine", defaults.MACDLine, scaled.MACDLine},
		{"BBMiddle", defaults.BBMiddle, scaled.BBMiddle},
		{"ATR", defaults.ATR, scaled.ATR},
		{"VolumeSMA", defaults.VolumeSMA, scaled.VolumeSMA},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.gotDefault == nil || tc.gotScaled == nil {
				t.Fatalf("%s: expected non-nil values for both runs (defaults=%v, scaled=%v)", tc.name, tc.gotDefault, tc.gotScaled)
			}
			if math.Abs(*tc.gotDefault-*tc.gotScaled) < 1e-6 {
				t.Errorf("%s: defaults=%.6f, scaled=%.6f — values are too close; the period flag is likely silently ignored", tc.name, *tc.gotDefault, *tc.gotScaled)
			}
		})
	}
}

// TestCalculateIndicatorSet_DefaultsMatchLegacy proves that an empty
// IndicatorConfig is bit-identical to the legacy hardcoded periods (SMA
// 20/50, EMA 12/26, RSI 14, MACD 12/26/9, BB 20×2.0, ATR 14, VolumeSMA 20).
// This is the pre-PR-B equivalence guarantee — production.json is allowed to
// omit the indicator block entirely without disturbing existing results.
func TestCalculateIndicatorSet_DefaultsMatchLegacy(t *testing.T) {
	candles := buildTrendingCandles(120)

	withZero := calculateIndicatorSet(42, candles, entity.IndicatorConfig{}, 0)
	withExplicit := calculateIndicatorSet(42, candles, entity.IndicatorConfig{
		SMAShort:        20,
		SMALong:         50,
		EMAFast:         12,
		EMASlow:         26,
		RSIPeriod:       14,
		MACDFast:        12,
		MACDSlow:        26,
		MACDSignal:      9,
		BBPeriod:        20,
		BBMultiplier:    2.0,
		ATRPeriod:       14,
		VolumeSMAPeriod: 20,
	}, 0)

	if !floatPtrEq(withZero.SMAShort, withExplicit.SMAShort, 1e-9) {
		t.Errorf("SMAShort: zero=%v explicit=%v", deref(withZero.SMAShort), deref(withExplicit.SMAShort))
	}
	if !floatPtrEq(withZero.SMALong, withExplicit.SMALong, 1e-9) {
		t.Errorf("SMALong: zero=%v explicit=%v", deref(withZero.SMALong), deref(withExplicit.SMALong))
	}
	if !floatPtrEq(withZero.EMAFast, withExplicit.EMAFast, 1e-9) {
		t.Errorf("EMAFast: zero=%v explicit=%v", deref(withZero.EMAFast), deref(withExplicit.EMAFast))
	}
	if !floatPtrEq(withZero.EMASlow, withExplicit.EMASlow, 1e-9) {
		t.Errorf("EMASlow: zero=%v explicit=%v", deref(withZero.EMASlow), deref(withExplicit.EMASlow))
	}
	if !floatPtrEq(withZero.RSI, withExplicit.RSI, 1e-9) {
		t.Errorf("RSI: zero=%v explicit=%v", deref(withZero.RSI), deref(withExplicit.RSI))
	}
	if !floatPtrEq(withZero.BBMiddle, withExplicit.BBMiddle, 1e-9) {
		t.Errorf("BBMiddle: zero=%v explicit=%v", deref(withZero.BBMiddle), deref(withExplicit.BBMiddle))
	}
	if !floatPtrEq(withZero.ATR, withExplicit.ATR, 1e-9) {
		t.Errorf("ATR: zero=%v explicit=%v", deref(withZero.ATR), deref(withExplicit.ATR))
	}
	if !floatPtrEq(withZero.VolumeSMA, withExplicit.VolumeSMA, 1e-9) {
		t.Errorf("VolumeSMA: zero=%v explicit=%v", deref(withZero.VolumeSMA), deref(withExplicit.VolumeSMA))
	}
}

// buildTrendingCandles produces a noisy uptrend that exercises every
// indicator family — different lookbacks see different running averages,
// RSI gain/loss balance shifts, ATR window includes more / fewer outlier
// bars, etc. A pure linear trend was rejected because RSI saturates at
// 100 and ATR collapses to a constant, masking period-flag regressions.
func buildTrendingCandles(n int) []entity.Candle {
	out := make([]entity.Candle, n)
	for i := range out {
		// Sinusoidal noise on top of a linear drift gives every period
		// axis a different running mean / variance, so longer lookbacks
		// produce numerically distinct values from shorter ones.
		drift := float64(i) * 0.8
		osc := 5.0 * math.Sin(float64(i)*0.4)
		base := 100.0 + drift + osc
		// Variable bar range so ATR depends on which bars the window
		// includes, not just the slope.
		span := 1.0 + 0.5*math.Cos(float64(i)*0.3)
		out[i] = entity.Candle{
			Time:   int64(i) * 60_000,
			Open:   base,
			High:   base + span,
			Low:    base - span,
			Close:  base + 0.3*math.Sin(float64(i)*0.7),
			Volume: 100 + 30*math.Sin(float64(i)*0.2),
		}
	}
	return out
}

func floatPtrEq(a, b *float64, eps float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return math.Abs(*a-*b) < eps
}

func deref(p *float64) any {
	if p == nil {
		return "<nil>"
	}
	return *p
}
