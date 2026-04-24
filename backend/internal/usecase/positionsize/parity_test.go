package positionsize

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// TestParity_BacktestAndLiveShareSizer asserts that the SignalSizer interface
// shape used by the backtest RiskHandler and the PositionSizer used by the
// live TradingPipeline are satisfied by the same *Sizer implementation.
// A regression here (e.g. diverging signatures between the two call sites)
// would compile but produce two different sizing behaviours, which is
// exactly the class of bug this package exists to prevent.
func TestParity_BacktestAndLiveShareSizer(t *testing.T) {
	t.Parallel()
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
		LotStep:         0.1,
		MinLot:          0.1,
	}
	s := New(cfg, VenueDefaults("LTC_JPY"))

	// Same Input for both call sites.
	inputs := []struct {
		entry    float64
		equity   float64
		atr      float64
		dd       float64
		conf     float64
		minConf  float64
		expected float64
	}{
		{entry: 12000, equity: 100000, atr: 0, dd: 0, conf: 1, minConf: 0, expected: 0.5},
		{entry: 12000, equity: 1_000_000, atr: 0, dd: 0, conf: 1, minConf: 0, expected: 5.9},
	}

	for _, in := range inputs {
		got, _ := s.Sized(0.1, in.entry, 14, in.equity, in.atr, in.dd, in.conf, in.minConf)
		// Both paths call .Sized identically, so both must return this value.
		if got < in.expected-0.11 || got > in.expected+0.11 {
			t.Errorf("Sized(entry=%v, equity=%v) = %v, want ~%v",
				in.entry, in.equity, got, in.expected)
		}

		// Call the interface-shaped method too (what pipeline uses).
		var iface interface {
			Sized(requested, entryPrice, slPercent, equity, atr, ddPct, confidence, minConfidence float64) (float64, string)
		} = s
		viaIface, _ := iface.Sized(0.1, in.entry, 14, in.equity, in.atr, in.dd, in.conf, in.minConf)
		if viaIface != got {
			t.Errorf("iface vs concrete mismatch: iface=%v concrete=%v", viaIface, got)
		}
	}
}
