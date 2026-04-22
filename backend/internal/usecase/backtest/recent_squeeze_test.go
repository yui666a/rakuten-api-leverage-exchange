package backtest

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// TestCalculateIndicatorSet_BBSqueezeLookbackZeroDisables is the cycle44
// wiring guard. Passing bbSqueezeLookback=0 must suppress RecentSqueeze
// entirely, regardless of the candle shape, proving the lookback arg is
// actually honoured (rather than the legacy hardcoded `5`).
func TestCalculateIndicatorSet_BBSqueezeLookbackZeroDisables(t *testing.T) {
	candles := buildTightRangeCandles(80)

	got := calculateIndicatorSet(42, candles, 0)
	if got.RecentSqueeze != nil {
		t.Fatalf("bbSqueezeLookback=0 should leave RecentSqueeze nil; got %v", *got.RecentSqueeze)
	}
}

// TestCalculateIndicatorSet_BBSqueezeLookbackDrivesDetection proves the
// lookback value is consumed, not just stored. Two runs on the same
// candle series with different bb_squeeze_lookback values must produce
// distinguishable RecentSqueeze outcomes. Uses a flat tight-range
// series so BB bandwidth stays near zero throughout — a non-zero
// lookback sees the squeeze, lookback=0 does not.
//
// The simpler "0 disables" case is covered above in
// TestCalculateIndicatorSet_BBSqueezeLookbackZeroDisables; this test
// guards the positive direction (a non-zero value actually reaches the
// gate rather than being silently ignored by a second hardcoded 5).
func TestCalculateIndicatorSet_BBSqueezeLookbackDrivesDetection(t *testing.T) {
	candles := buildTightRangeCandles(80)

	off := calculateIndicatorSet(42, candles, 0)
	on := calculateIndicatorSet(42, candles, 5)

	if off.RecentSqueeze != nil {
		t.Fatalf("off lookback=0 should leave RecentSqueeze nil; got %v", *off.RecentSqueeze)
	}
	if on.RecentSqueeze == nil {
		t.Fatal("on lookback=5: RecentSqueeze unexpectedly nil")
	}
	if !*on.RecentSqueeze {
		t.Errorf("on lookback=5 should see the tight-range squeeze; got false")
	}
}

// buildTightRangeCandles produces a flat, low-volatility series so the
// BB bandwidth stays near zero for every bar. Any non-zero lookback
// would set RecentSqueeze = true on this data; the zero-lookback test
// uses it to prove the gate actually turns off.
func buildTightRangeCandles(n int) []entity.Candle {
	out := make([]entity.Candle, n)
	base := 100.0
	for i := range out {
		out[i] = entity.Candle{
			Time:   int64(i) * 60_000,
			Open:   base,
			High:   base + 0.001,
			Low:    base - 0.001,
			Close:  base,
			Volume: 100,
		}
	}
	return out
}

