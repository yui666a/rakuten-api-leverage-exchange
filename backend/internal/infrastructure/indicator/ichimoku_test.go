package indicator

import (
	"math"
	"testing"
)

func TestIchimoku_InsufficientDataGivesNaN(t *testing.T) {
	// 10 bars isn't even enough for Kijun (26).
	h, l, c := buildFlatSeries(10, 100)
	ic := Ichimoku(h, l, c, 9, 26, 52)
	if !math.IsNaN(ic.Kijun) {
		t.Fatalf("Kijun should be NaN on 10 bars; got %v", ic.Kijun)
	}
	if !math.IsNaN(ic.SenkouA) || !math.IsNaN(ic.SenkouB) {
		t.Fatalf("Senkou should be NaN without 26 bars of source; got A=%v B=%v", ic.SenkouA, ic.SenkouB)
	}
}

func TestIchimoku_LengthMismatchGivesAllNaN(t *testing.T) {
	h := make([]float64, 80)
	l := make([]float64, 80)
	c := make([]float64, 79)
	ic := Ichimoku(h, l, c, 9, 26, 52)
	if !math.IsNaN(ic.Tenkan) || !math.IsNaN(ic.Kijun) || !math.IsNaN(ic.SenkouA) || !math.IsNaN(ic.SenkouB) {
		t.Fatalf("mismatched lengths must give all-NaN, got %+v", ic)
	}
}

func TestIchimoku_FlatSeriesAllLinesEqualPrice(t *testing.T) {
	// A flat price series: every line should equal the price itself.
	h, l, c := buildFlatSeries(100, 100)
	ic := Ichimoku(h, l, c, 9, 26, 52)
	if ic.Tenkan != 100 || ic.Kijun != 100 {
		t.Fatalf("flat: expected 100/100, got tenkan=%v kijun=%v", ic.Tenkan, ic.Kijun)
	}
	if ic.SenkouA != 100 || ic.SenkouB != 100 {
		t.Fatalf("flat: expected senkouA=B=100, got A=%v B=%v", ic.SenkouA, ic.SenkouB)
	}
	if ic.Chikou != 100 {
		t.Fatalf("flat: expected chikou=100, got %v", ic.Chikou)
	}
}

func TestIchimoku_UptrendTenkanLeadsKijun(t *testing.T) {
	// Monotonic uptrend: Tenkan is shorter, so it sits above Kijun.
	h, l, c := buildMonotonicUptrend(100, 100, 1.0)
	ic := Ichimoku(h, l, c, 9, 26, 52)
	if math.IsNaN(ic.Tenkan) || math.IsNaN(ic.Kijun) {
		t.Fatalf("expected non-NaN tenkan/kijun in uptrend, got %+v", ic)
	}
	if ic.Tenkan <= ic.Kijun {
		t.Fatalf("uptrend: expected Tenkan > Kijun, got %v vs %v", ic.Tenkan, ic.Kijun)
	}
	// Price should be above the cloud in a sustained uptrend (after 100
	// bars of monotonic rise, the displayed Senkou spans come from 26 bars
	// ago, so price is well above them).
	price := c[len(c)-1]
	pos := IchimokuCloudPosition(price, ic)
	if pos != "above" {
		t.Fatalf("uptrend: expected price above cloud, got %q (price=%v A=%v B=%v)", pos, price, ic.SenkouA, ic.SenkouB)
	}
}

func TestIchimoku_DowntrendPriceBelowCloud(t *testing.T) {
	n := 100
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	price := 200.0
	for i := 0; i < n; i++ {
		closes[i] = price
		highs[i] = price + 0.5
		lows[i] = price - 0.5
		price -= 1.0
	}
	ic := Ichimoku(highs, lows, closes, 9, 26, 52)
	pos := IchimokuCloudPosition(closes[n-1], ic)
	if pos != "below" {
		t.Fatalf("downtrend: expected below, got %q (price=%v A=%v B=%v)", pos, closes[n-1], ic.SenkouA, ic.SenkouB)
	}
	if ic.Tenkan >= ic.Kijun {
		t.Fatalf("downtrend: expected Tenkan < Kijun, got %v vs %v", ic.Tenkan, ic.Kijun)
	}
}

func TestIchimokuCloudPosition_EmptyWhenCloudUndefined(t *testing.T) {
	ic := IchimokuResult{
		Tenkan:  50,
		Kijun:   50,
		SenkouA: math.NaN(),
		SenkouB: math.NaN(),
		Chikou:  50,
	}
	if pos := IchimokuCloudPosition(100, ic); pos != "" {
		t.Fatalf("NaN cloud should give empty string, got %q", pos)
	}
}

func TestIchimokuCloudPosition_InsideCloud(t *testing.T) {
	ic := IchimokuResult{SenkouA: 110, SenkouB: 90}
	for _, price := range []float64{95, 100, 109} {
		if pos := IchimokuCloudPosition(price, ic); pos != "inside" {
			t.Fatalf("price=%v expected inside, got %q", price, pos)
		}
	}
	if pos := IchimokuCloudPosition(111, ic); pos != "above" {
		t.Fatalf("price=111 expected above, got %q", pos)
	}
	if pos := IchimokuCloudPosition(89, ic); pos != "below" {
		t.Fatalf("price=89 expected below, got %q", pos)
	}
}
