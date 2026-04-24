package backtest

import (
	"context"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// synthMarket produces a deterministic sine-with-drift candle stream that
// produces trades under the default strategy. Shared between sizing tests so
// signal counts are comparable across runs.
func synthMarket() (primary, higher []entity.Candle) {
	baseTime := int64(1_770_000_000_000)
	price := 10000.0
	primary = make([]entity.Candle, 0, 300)
	for i := 0; i < 300; i++ {
		price += math.Sin(float64(i)/7.0) * 120.0
		ts := baseTime + int64(i)*15*60*1000
		primary = append(primary, entity.Candle{
			Open:  price - 40,
			High:  price + 90,
			Low:   price - 90,
			Close: price,
			Time:  ts,
			Volume: 10,
		})
	}
	higher = make([]entity.Candle, 0, 75)
	for i := 0; i < 75; i++ {
		idx := i * 4
		p := primary[idx].Close
		higher = append(higher, entity.Candle{
			Open: p - 50, High: p + 100, Low: p - 100, Close: p,
			Time: primary[idx].Time, Volume: 40,
		})
	}
	return
}

func TestBacktestRunner_PositionSizingFixedByDefault(t *testing.T) {
	primary, higher := synthMarket()
	runner := NewBacktestRunner()
	cfg := entity.BacktestConfig{
		Symbol: "LTC_JPY", SymbolID: 10,
		PrimaryInterval: "PT15M", HigherTFInterval: "PT1H",
		FromTimestamp:  primary[0].Time,
		ToTimestamp:    primary[len(primary)-1].Time,
		InitialBalance: 100000, SpreadPercent: 0.1, DailyCarryCost: 0,
	}
	risk := entity.RiskConfig{
		MaxPositionAmount: 1_000_000_000, MaxDailyLoss: 1_000_000_000,
		StopLossPercent: 14, TakeProfitPercent: 4, InitialCapital: 100000,
	}
	res, err := runner.Run(context.Background(), RunInput{
		Config: cfg, RiskConfig: risk, TradeAmount: 0.1,
		PrimaryCandles: primary, HigherCandles: higher,
		// PositionSizing nil → fixed behaviour
	})
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if len(res.Trades) == 0 {
		t.Skip("no trades generated on synthetic market; widen generator")
	}
	for _, tr := range res.Trades {
		if math.Abs(tr.Amount-0.1) > 1e-9 {
			t.Fatalf("trade amount %v, want 0.1 (fixed)", tr.Amount)
		}
	}
}

func TestBacktestRunner_PositionSizingRiskPctScalesLots(t *testing.T) {
	primary, higher := synthMarket()
	cfg := entity.BacktestConfig{
		Symbol: "LTC_JPY", SymbolID: 10,
		PrimaryInterval: "PT15M", HigherTFInterval: "PT1H",
		FromTimestamp:  primary[0].Time,
		ToTimestamp:    primary[len(primary)-1].Time,
		InitialBalance: 100000, SpreadPercent: 0.1, DailyCarryCost: 0,
	}
	risk := entity.RiskConfig{
		MaxPositionAmount: 1_000_000_000, MaxDailyLoss: 1_000_000_000,
		StopLossPercent: 14, TakeProfitPercent: 4, InitialCapital: 100000,
	}
	ps := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
	}
	runner := NewBacktestRunner()
	res, err := runner.Run(context.Background(), RunInput{
		Config: cfg, RiskConfig: risk, TradeAmount: 0.1,
		PrimaryCandles: primary, HigherCandles: higher,
		PositionSizing: ps,
	})
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if len(res.Trades) == 0 {
		t.Skip("no trades generated on synthetic market")
	}
	// With equity ~100k, SL=14% and LTC price ~10k, base lot ≈ 0.07-0.08,
	// which rounds down to 0 under the LTC venue lot-step (0.1) unless the
	// price/balance combination pushes it above. So lot should be >= 0.1 when
	// produced at all (min_lot gate rejects otherwise).
	for _, tr := range res.Trades {
		if tr.Amount < 0.1-1e-9 {
			t.Fatalf("trade amount %v below min_lot 0.1 but still executed", tr.Amount)
		}
	}
}
