package backtest

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// TestCalculateIndicatorSet_PeriodsDriveValues is the PR-B / PR-C wiring
// guard: it proves that calculateIndicatorSet actually consumes the
// IndicatorConfig argument, rather than ignoring it the way
// SetBBSqueezeLookback was originally a silent no-op (cycle43). Two runs
// on the same noisy candle series with different periods must produce
// numerically distinguishable values for every indicator.
//
// We use sinusoidal noise on top of a linear drift so longer lookbacks
// see different running means / variances than shorter ones — every
// period axis becomes a visible signal. A passive guard (just checking
// that the values are non-nil) would have missed cycle43-style "the
// field is set but the period is hardcoded" regressions.
//
// Candle count 320 = scaled IchimokuSenkouB (156) + Kijun (78) + buffer
// so the longest profile completes warmup.
func TestCalculateIndicatorSet_PeriodsDriveValues(t *testing.T) {
	candles := buildTrendingCandles(320)

	defaults := calculateIndicatorSet(42, candles, entity.IndicatorConfig{}, 0)
	scaled := calculateIndicatorSet(42, candles, entity.IndicatorConfig{
		SMAShort:            60,
		SMALong:             100,
		EMAFast:             36,
		EMASlow:             78,
		RSIPeriod:           42,
		MACDFast:            24,
		MACDSlow:            52,
		MACDSignal:          18,
		BBPeriod:            60,
		BBMultiplier:        2.0,
		ATRPeriod:           42,
		VolumeSMAPeriod:     60,
		ADXPeriod:           42,
		StochKPeriod:        42,
		StochSmoothK:        9,
		StochSmoothD:        9,
		StochRSIRSIPeriod:   42,
		StochRSIStochPeriod: 42,
		DonchianPeriod:      60,
		OBVSlopePeriod:      60,
		CMFPeriod:           60,
		IchimokuTenkan:      27,
		IchimokuKijun:       78,
		IchimokuSenkouB:     156,
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
		{"ADX", defaults.ADX, scaled.ADX},
		{"StochK", defaults.StochK, scaled.StochK},
		{"StochD", defaults.StochD, scaled.StochD},
		{"StochRSI", defaults.StochRSI, scaled.StochRSI},
		{"DonchianUpper", defaults.DonchianUpper, scaled.DonchianUpper},
		{"OBVSlope", defaults.OBVSlope, scaled.OBVSlope},
		{"CMF", defaults.CMF, scaled.CMF},
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

	// Ichimoku: separate path because its values live inside a nested snapshot.
	// Tenkan / Kijun must differ between the default 9/26/52 series and the
	// scaled 27/78/156 series — the longer windows lag the noisy uptrend
	// noticeably more, so the values land at different points along the same
	// curve.
	t.Run("IchimokuTenkan", func(t *testing.T) {
		if defaults.Ichimoku == nil || scaled.Ichimoku == nil {
			t.Fatalf("expected non-nil Ichimoku snapshots (defaults=%v scaled=%v)", defaults.Ichimoku, scaled.Ichimoku)
		}
		if defaults.Ichimoku.Tenkan == nil || scaled.Ichimoku.Tenkan == nil {
			t.Fatalf("Tenkan: expected both non-nil (defaults=%v scaled=%v)", defaults.Ichimoku.Tenkan, scaled.Ichimoku.Tenkan)
		}
		if math.Abs(*defaults.Ichimoku.Tenkan-*scaled.Ichimoku.Tenkan) < 1e-6 {
			t.Errorf("Tenkan: defaults=%.6f scaled=%.6f — too close, period likely ignored", *defaults.Ichimoku.Tenkan, *scaled.Ichimoku.Tenkan)
		}
	})
	t.Run("IchimokuKijun", func(t *testing.T) {
		if defaults.Ichimoku.Kijun == nil || scaled.Ichimoku.Kijun == nil {
			t.Fatalf("Kijun: expected both non-nil (defaults=%v scaled=%v)", defaults.Ichimoku.Kijun, scaled.Ichimoku.Kijun)
		}
		if math.Abs(*defaults.Ichimoku.Kijun-*scaled.Ichimoku.Kijun) < 1e-6 {
			t.Errorf("Kijun: defaults=%.6f scaled=%.6f — too close, period likely ignored", *defaults.Ichimoku.Kijun, *scaled.Ichimoku.Kijun)
		}
	})
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

// buildTrendingCandles produces a noisy quasi-random walk that exercises
// every indicator family — RSI gain/loss balance, ATR window, Donchian
// extremes, Stoch oscillators, etc. all change with the look-back window.
//
// Pure linear / monotonic uptrends were rejected because:
//   - RSI saturates at 100 (no losing bars), masking period changes;
//   - StochRSI inherits the saturation and stays at 100;
//   - Donchian's high-of-N collapses to the latest bar's high regardless
//     of N because the series is monotonic.
//
// We use overlapping sinusoids (3 frequencies) plus a mild positive drift
// so the series advances overall but every window contains genuine
// max/min variation.
func buildTrendingCandles(n int) []entity.Candle {
	out := make([]entity.Candle, n)
	for i := range out {
		drift := float64(i) * 0.05
		osc := 12.0*math.Sin(float64(i)*0.13) +
			8.0*math.Sin(float64(i)*0.41) +
			4.0*math.Sin(float64(i)*1.05)
		base := 100.0 + drift + osc
		span := 1.5 + 1.0*math.Cos(float64(i)*0.27)
		if span < 0.2 {
			span = 0.2
		}
		out[i] = entity.Candle{
			Time:   int64(i) * 60_000,
			Open:   base,
			High:   base + span,
			Low:    base - span,
			Close:  base + 0.6*math.Sin(float64(i)*0.71),
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
