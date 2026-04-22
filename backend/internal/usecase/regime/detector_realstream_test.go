package regime

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/indicator"
)

// TestDetector_RealLTCStream_RegimeHistogram is a *diagnostic* test (skipped
// by default unless BACKTEST_REAL_DATA=1 is set in the env) that loads the
// committed LTC 15m CSV, computes ADX/ATR per bar exactly the way the
// backtest runner does, drives the regime detector across the resulting
// stream, and prints a histogram of committed regimes.
//
// Cycle40's WFO sweep on TrendADXMin × VolatileATRPercentMin showed every
// single threshold cell produced byte-identical results — confirming the
// detector never emits anything other than the default. This test exists
// to prove that finding is *not* a wiring bug (the detector really does
// classify its way into one regime continuously) and to surface the
// distribution that motivates the cycle40 verdict.
func TestDetector_RealLTCStream_RegimeHistogram(t *testing.T) {
	if os.Getenv("BACKTEST_REAL_DATA") != "1" {
		t.Skip("set BACKTEST_REAL_DATA=1 to run the real-stream diagnostic")
	}
	csvPath := "../../../data/candles_LTC_JPY_PT15M.csv"
	highs, lows, closes := loadCSV(t, csvPath)
	t.Logf("loaded %d candles from %s", len(closes), csvPath)

	// Three configs to compare side by side.
	cases := map[string]Config{
		"defaults":     {},
		"loose ADX10":  {TrendADXMin: 10, VolatileATRPercentMin: 0.5, HysteresisBars: 3},
		"tight ADX50":  {TrendADXMin: 50, VolatileATRPercentMin: 5.0, HysteresisBars: 3},
	}

	const buf = 60 // enough for all indicators to warm up
	for label, cfg := range cases {
		d := NewDetector(cfg)
		hist := map[entity.Regime]int{}
		for i := buf; i < len(closes); i++ {
			adxVal, _, _ := indicator.ADX(highs[:i+1], lows[:i+1], closes[:i+1], 14)
			atrVal := indicator.ATR(highs[:i+1], lows[:i+1], closes[:i+1], 14)
			set := entity.IndicatorSet{}
			if !isNaN(adxVal) {
				set.ADX14 = &adxVal
			}
			if !isNaN(atrVal) {
				set.ATR14 = &atrVal
			}
			lastPrice := closes[i]
			c := d.Classify(set, nil, lastPrice)
			hist[c.Regime]++
		}
		t.Logf("[%s] regime histogram (n=%d): %v", label, len(closes)-buf, fmtHist(hist))
	}
}

func loadCSV(t *testing.T, path string) (highs, lows, closes []float64) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i == 0 || line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 8 {
			continue
		}
		hi, _ := strconv.ParseFloat(strings.Trim(fields[5], `"`), 64)
		lo, _ := strconv.ParseFloat(strings.Trim(fields[6], `"`), 64)
		cl, _ := strconv.ParseFloat(strings.Trim(fields[7], `"`), 64)
		highs = append(highs, hi)
		lows = append(lows, lo)
		closes = append(closes, cl)
	}
	return highs, lows, closes
}

func isNaN(f float64) bool { return f != f }

func fmtHist(h map[entity.Regime]int) string {
	parts := []string{}
	total := 0
	for _, v := range h {
		total += v
	}
	for _, r := range []entity.Regime{
		entity.RegimeUnknown, entity.RegimeBullTrend, entity.RegimeBearTrend,
		entity.RegimeRange, entity.RegimeVolatile,
	} {
		if v := h[r]; v > 0 {
			pct := float64(v) / float64(total) * 100
			parts = append(parts, fmt.Sprintf("%s=%d(%.1f%%)", r, v, pct))
		}
	}
	return strings.Join(parts, " ")
}
