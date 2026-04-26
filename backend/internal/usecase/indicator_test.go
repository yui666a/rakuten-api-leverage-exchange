package usecase

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestIndicatorCalculator_Calculate(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	// Generate 50 candles (needed for SMALong)
	candles := make([]entity.Candle, 50)
	for i := range candles {
		candles[i] = entity.Candle{
			Close: float64(100 + i),
			Time:  int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	calc := NewIndicatorCalculator(repo)

	result, err := calc.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.SymbolID != 7 {
		t.Fatalf("expected symbolID 7, got %d", result.SymbolID)
	}

	if result.SMAShort == nil {
		t.Fatal("SMAShort should not be nil with 50 data points")
	}

	if result.SMALong == nil {
		t.Fatal("SMALong should not be nil with 50 data points")
	}

	if result.RSI == nil {
		t.Fatal("RSI should not be nil with 50 data points")
	}

	if *result.RSI < 0 || *result.RSI > 100 {
		t.Fatalf("RSI should be 0-100, got %f", *result.RSI)
	}
}

func TestIndicatorCalculator_InsufficientData(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	// Only 5 candles - not enough for any meaningful indicator
	candles := make([]entity.Candle, 5)
	for i := range candles {
		candles[i] = entity.Candle{
			Close: float64(100 + i),
			Time:  int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	calc := NewIndicatorCalculator(repo)

	result, err := calc.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SMAShort requires 20 data points, so should be nil
	if result.SMAShort != nil {
		t.Fatalf("SMAShort should be nil with only 5 data points, got %f", *result.SMAShort)
	}
}

// TestIndicatorCalculator_SetIndicatorPeriods is the PR-B wiring guard for
// the live calculator. SetIndicatorPeriods must actually feed into the SMA /
// EMA / RSI / BB / ATR / VolumeSMA computations, not just store a value.
//
// Two runs on the same noisy candle series with different periods must
// produce numerically distinguishable values. A passive guard (just
// checking the call doesn't panic) would have missed cycle43-style "the
// field is set but the period is hardcoded" regressions.
func TestIndicatorCalculator_SetIndicatorPeriods(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	// 200 noisy candles so longer-period lookbacks have a visibly different
	// running mean from shorter ones. Linear-only data would let RSI saturate.
	candles := make([]entity.Candle, 200)
	for i := range candles {
		drift := float64(i) * 0.7
		osc := 5.0 * math.Sin(float64(i)*0.4)
		base := 100.0 + drift + osc
		span := 1.0 + 0.5*math.Cos(float64(i)*0.3)
		candles[i] = entity.Candle{
			Time:   int64(1700000000000 + i*60000),
			Open:   base,
			High:   base + span,
			Low:    base - span,
			Close:  base + 0.3*math.Sin(float64(i)*0.7),
			Volume: 100 + 30*math.Sin(float64(i)*0.2),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	defaults := NewIndicatorCalculator(repo)
	defaultsRes, err := defaults.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}

	scaled := NewIndicatorCalculator(repo)
	scaled.SetIndicatorPeriods(entity.IndicatorConfig{
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
	})
	scaledRes, err := scaled.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("scaled: %v", err)
	}

	cases := []struct {
		name             string
		def, scl         *float64
	}{
		{"SMAShort", defaultsRes.SMAShort, scaledRes.SMAShort},
		{"SMALong", defaultsRes.SMALong, scaledRes.SMALong},
		{"EMAFast", defaultsRes.EMAFast, scaledRes.EMAFast},
		{"EMASlow", defaultsRes.EMASlow, scaledRes.EMASlow},
		{"RSI", defaultsRes.RSI, scaledRes.RSI},
		{"MACDLine", defaultsRes.MACDLine, scaledRes.MACDLine},
		{"BBMiddle", defaultsRes.BBMiddle, scaledRes.BBMiddle},
		{"ATR", defaultsRes.ATR, scaledRes.ATR},
		{"VolumeSMA", defaultsRes.VolumeSMA, scaledRes.VolumeSMA},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.def == nil || tc.scl == nil {
				t.Fatalf("%s: expected both non-nil (def=%v scl=%v)", tc.name, tc.def, tc.scl)
			}
			if math.Abs(*tc.def-*tc.scl) < 1e-6 {
				t.Errorf("%s: defaults=%.6f scaled=%.6f — values too close; period flag likely silently ignored", tc.name, *tc.def, *tc.scl)
			}
		})
	}
}

func TestIndicatorCalculator_JSONSafe(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	// Only 5 candles - most indicators will be nil
	candles := make([]entity.Candle, 5)
	for i := range candles {
		candles[i] = entity.Candle{
			Close: float64(100 + i),
			Time:  int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	calc := NewIndicatorCalculator(repo)
	result, err := calc.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// NaN fields are nil, so JSON serialization should succeed
	_, err = json.Marshal(result)
	if err != nil {
		t.Fatalf("JSON marshal should not fail: %v", err)
	}
}
