package backtest

import (
	"context"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	infrabt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/backtest"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

func newOrderbookReplayForTest(snaps []entity.Orderbook) *infrabt.OrderbookReplay {
	return infrabt.NewOrderbookReplay(snaps, 60_000)
}

func TestMergeCandleEvents_PrefersHigherOnSameTimestamp(t *testing.T) {
	primary := []entity.Candle{
		{Time: 2000, Close: 2},
	}
	higher := []entity.Candle{
		{Time: 2000, Close: 20},
	}
	events := mergeCandleEvents(primary, higher, "PT15M", "PT1H", 7)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	first, ok := events[0].(entity.CandleEvent)
	if !ok {
		t.Fatalf("expected CandleEvent, got %T", events[0])
	}
	if first.Interval != "PT1H" {
		t.Fatalf("expected higher timeframe first, got %s", first.Interval)
	}
}

func TestBacktestRunner_Run(t *testing.T) {
	primary := make([]entity.Candle, 0, 80)
	higher := make([]entity.Candle, 0, 20)
	baseTime := int64(1_770_000_000_000)

	price := 100.0
	for i := 0; i < 80; i++ {
		price += math.Sin(float64(i)/7.0) * 0.8
		ts := baseTime + int64(i)*15*60*1000
		primary = append(primary, entity.Candle{
			Open:  price - 0.5,
			High:  price + 1.0,
			Low:   price - 1.0,
			Close: price,
			Time:  ts,
		})
	}
	for i := 0; i < 20; i++ {
		idx := i * 4
		ts := primary[idx].Time
		p := primary[idx].Close
		higher = append(higher, entity.Candle{
			Open:  p - 0.6,
			High:  p + 1.2,
			Low:   p - 1.2,
			Close: p,
			Time:  ts,
		})
	}

	runner := NewBacktestRunner()
	result, err := runner.Run(context.Background(), RunInput{
		Config: entity.BacktestConfig{
			Symbol:           "BTC_JPY",
			SymbolID:         7,
			PrimaryInterval:  "PT15M",
			HigherTFInterval: "PT1H",
			FromTimestamp:    primary[0].Time,
			ToTimestamp:      primary[len(primary)-1].Time,
			InitialBalance:   100000,
			SpreadPercent:    0.1,
			DailyCarryCost:   0.04,
		},
		RiskConfig: entity.RiskConfig{
			MaxPositionAmount:    1_000_000_000,
			MaxDailyLoss:         1_000_000_000,
			StopLossPercent:      5,
			TakeProfitPercent:    10,
			InitialCapital:       100000,
			MaxConsecutiveLosses: 0,
			CooldownMinutes:      0,
		},
		TradeAmount:    0.01,
		PrimaryCandles: primary,
		HigherCandles:  higher,
	})
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if result == nil {
		t.Fatal("result must not be nil")
	}
	if result.Summary.InitialBalance != 100000 {
		t.Fatalf("unexpected initial balance: %f", result.Summary.InitialBalance)
	}
	if result.Summary.FinalBalance <= 0 {
		t.Fatalf("final balance must be positive: %f", result.Summary.FinalBalance)
	}
	if len(result.ID) != 26 {
		t.Fatalf("expected ULID length 26, got %d id=%s", len(result.ID), result.ID)
	}
}

// TestBacktestRunner_RouterStrategyChangesResult is the PR-5 part C
// runner-level wiring confirmation. It mirrors the ATR trailing test
// below: same synthetic candle stream, two strategies — a flat
// ConfigurableStrategy on a child profile vs. a regime-routing
// ProfileRouter that pairs two distinct child profiles — and asserts
// the resulting trade counts differ.
//
// Without this guard, a future change that silently bypassed the
// router (e.g. always returning the default child from the runner)
// would not be caught by any unit test.
//
// The two child profiles use deliberately different RSI thresholds
// so they emit different signal patterns; the regime detector must
// actually swap delegation to make the trade counts diverge from the
// flat baseline.
func TestBacktestRunner_RouterStrategyChangesResult(t *testing.T) {
	primary := make([]entity.Candle, 0, 200)
	higher := make([]entity.Candle, 0, 50)
	baseTime := int64(1_770_000_000_000)
	price := 100.0
	for i := 0; i < 200; i++ {
		// Sine-with-drift: enough swings + ATR motion for the regime
		// detector to commit different regimes across the run.
		price += math.Sin(float64(i)/5.0) * 1.5
		ts := baseTime + int64(i)*15*60*1000
		primary = append(primary, entity.Candle{
			Open: price - 0.5, High: price + 1.2, Low: price - 1.2, Close: price, Time: ts,
		})
	}
	for i := 0; i < 50; i++ {
		idx := i * 4
		p := primary[idx].Close
		higher = append(higher, entity.Candle{
			Open: p - 0.6, High: p + 1.3, Low: p - 1.3, Close: p, Time: primary[idx].Time,
		})
	}
	cfg := entity.BacktestConfig{
		Symbol: "BTC_JPY", SymbolID: 7,
		PrimaryInterval: "PT15M", HigherTFInterval: "PT1H",
		FromTimestamp:  primary[0].Time,
		ToTimestamp:    primary[len(primary)-1].Time,
		InitialBalance: 100000, SpreadPercent: 0.1, DailyCarryCost: 0.04,
	}
	risk := entity.RiskConfig{
		MaxPositionAmount: 1_000_000_000, MaxDailyLoss: 1_000_000_000,
		StopLossPercent: 5, TakeProfitPercent: 10, InitialCapital: 100000,
	}
	run := func(strat port.Strategy) *entity.BacktestResult {
		t.Helper()
		runner := NewBacktestRunner(WithStrategy(strat))
		res, err := runner.Run(context.Background(), RunInput{
			Config: cfg, RiskConfig: risk, TradeAmount: 0.01,
			PrimaryCandles: primary, HigherCandles: higher,
		})
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}
		return res
	}

	// Two child profiles with deliberately different RSI thresholds
	// so they pick different entry/exit moments on the same candle
	// stream. Built via NewConfigurableStrategy so they go through
	// exactly the production path.
	bullProfile := newRouterChildProfile("bull_child", 60, 40, 25, 75)
	bearProfile := newRouterChildProfile("bear_child", 50, 50, 35, 65)

	bullStrat, err := strategyuc.NewConfigurableStrategy(bullProfile)
	if err != nil {
		t.Fatalf("bull strat: %v", err)
	}
	bearStrat, err := strategyuc.NewConfigurableStrategy(bearProfile)
	if err != nil {
		t.Fatalf("bear strat: %v", err)
	}

	// Baseline: flat profile (bull-only).
	baseline := run(bullStrat)

	// Router: bull as default + bear-trend override. The candle stream
	// trips the bear regime mid-run (ATR rises, SMA crosses), so the
	// router must delegate to bear at least once and produce a
	// different trade pattern than the bull-only baseline.
	router, err := strategyuc.NewProfileRouter(strategyuc.ProfileRouterInput{
		Name:            "router_runner_test",
		DefaultStrategy: bullStrat,
		Overrides: map[entity.Regime]port.Strategy{
			entity.RegimeBearTrend: bearStrat,
		},
	})
	if err != nil {
		t.Fatalf("NewProfileRouter: %v", err)
	}
	routerRun := run(router)

	if baseline.Summary.TotalTrades == routerRun.Summary.TotalTrades &&
		baseline.Summary.TotalReturn == routerRun.Summary.TotalReturn {
		t.Fatalf("router strategy had no effect on backtest output (trades=%d, return=%v) — wiring regression: ProfileRouter never delegated to a non-default child",
			baseline.Summary.TotalTrades, baseline.Summary.TotalReturn)
	}
}

// newRouterChildProfile builds a minimal valid StrategyProfile for
// the wiring-confirmation test above. Returning a fully-populated
// (non-router) profile so NewConfigurableStrategy(p) accepts it
// without weakening the existing strict Validate.
func newRouterChildProfile(name string, rsiBuyMax, rsiSellMin, rsiOversold, rsiOverbought float64) *entity.StrategyProfile {
	return &entity.StrategyProfile{
		Name: name,
		Indicators: entity.IndicatorConfig{
			SMAShort: 20, SMALong: 50, RSIPeriod: 14,
			MACDFast: 12, MACDSlow: 26, MACDSignal: 9,
			BBPeriod: 20, BBMultiplier: 2.0, ATRPeriod: 14,
		},
		StanceRules: entity.StanceRulesConfig{
			RSIOversold:   rsiOversold,
			RSIOverbought: rsiOverbought,
		},
		SignalRules: entity.SignalRulesConfig{
			TrendFollow: entity.TrendFollowConfig{
				Enabled:    true,
				RSIBuyMax:  rsiBuyMax,
				RSISellMin: rsiSellMin,
			},
			Contrarian: entity.ContrarianConfig{
				Enabled:  true,
				RSIEntry: rsiOversold,
				RSIExit:  rsiOverbought,
			},
			Breakout: entity.BreakoutConfig{
				Enabled:        true,
				VolumeRatioMin: 1.5,
			},
		},
		Risk: entity.StrategyRiskConfig{StopLossPercent: 5, TakeProfitPercent: 10},
	}
}

// TestBacktestRunner_ATRTrailingChangesResult is the PR-12 wiring
// confirmation test mandated by the design doc (§5 配線確認). It runs the
// same synthetic data twice — once with ATR multipliers disabled, once
// with them enabled — and asserts the two results differ. This is the
// contract that stops future changes from silently reverting PR-12 to the
// pre-wiring behaviour (cycle08 regression).
func TestBacktestRunner_ATRTrailingChangesResult(t *testing.T) {
	primary := make([]entity.Candle, 0, 200)
	higher := make([]entity.Candle, 0, 50)
	baseTime := int64(1_770_000_000_000)

	price := 100.0
	for i := 0; i < 200; i++ {
		// Sine-wave-with-drift price path, enough swings for trailing
		// stops to matter and create divergent exit timing between the
		// two runs.
		price += math.Sin(float64(i)/5.0) * 1.5
		ts := baseTime + int64(i)*15*60*1000
		primary = append(primary, entity.Candle{
			Open:  price - 0.5,
			High:  price + 1.2,
			Low:   price - 1.2,
			Close: price,
			Time:  ts,
		})
	}
	for i := 0; i < 50; i++ {
		idx := i * 4
		p := primary[idx].Close
		higher = append(higher, entity.Candle{
			Open: p - 0.6, High: p + 1.3, Low: p - 1.3, Close: p, Time: primary[idx].Time,
		})
	}

	baseRisk := entity.RiskConfig{
		MaxPositionAmount:    1_000_000_000,
		MaxDailyLoss:         1_000_000_000,
		StopLossPercent:      5,
		TakeProfitPercent:    10,
		InitialCapital:       100000,
		MaxConsecutiveLosses: 0,
	}
	cfg := entity.BacktestConfig{
		Symbol:           "BTC_JPY",
		SymbolID:         7,
		PrimaryInterval:  "PT15M",
		HigherTFInterval: "PT1H",
		FromTimestamp:    primary[0].Time,
		ToTimestamp:      primary[len(primary)-1].Time,
		InitialBalance:   100000,
		SpreadPercent:    0.1,
		DailyCarryCost:   0.04,
	}
	run := func(r entity.RiskConfig) *entity.BacktestResult {
		t.Helper()
		runner := NewBacktestRunner()
		res, err := runner.Run(context.Background(), RunInput{
			Config:         cfg,
			RiskConfig:     r,
			TradeAmount:    0.01,
			PrimaryCandles: primary,
			HigherCandles:  higher,
		})
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}
		return res
	}

	baseline := run(baseRisk)
	withATR := baseRisk
	withATR.TrailingATRMultiplier = 3.0 // ATR ベースで trail 距離を大きく緩める
	withATR.StopLossATRMultiplier = 3.0
	atrRun := run(withATR)

	// The ATR settings must change *something* observable at the boundary.
	// If the handler silently ignored them (pre-PR-12 cycle08 scenario),
	// both summaries would be identical down to the trade count.
	if baseline.Summary.TotalTrades == atrRun.Summary.TotalTrades &&
		baseline.Summary.TotalReturn == atrRun.Summary.TotalReturn {
		t.Fatalf("ATR trailing had no effect on backtest output (trades=%d, return=%v) — wiring regression",
			baseline.Summary.TotalTrades, baseline.Summary.TotalReturn)
	}
}

// TestBacktestRunner_OrderbookReplayDiffersFromPercent verifies that a run
// driven by a fully-populated OrderbookReplay produces different trade
// economics than the legacy percent model. We build a synthetic series and
// hand-craft per-bar orderbooks so the orderbook fills are systematically
// inferior (wider spread + thin top of book).
func TestBacktestRunner_OrderbookReplayDiffersFromPercent(t *testing.T) {
	primary := make([]entity.Candle, 0, 60)
	baseTime := int64(1_770_000_000_000)
	price := 100.0
	for i := 0; i < 60; i++ {
		price += math.Sin(float64(i)/5.0) * 1.5
		ts := baseTime + int64(i)*15*60*1000
		primary = append(primary, entity.Candle{
			Open:  price - 0.5,
			High:  price + 1.0,
			Low:   price - 1.0,
			Close: price,
			Time:  ts,
		})
	}

	// Build one orderbook snapshot per bar with a 1% spread (much wider than
	// the percent baseline below, 0.1%).
	snaps := make([]entity.Orderbook, 0, len(primary))
	for _, c := range primary {
		snaps = append(snaps, entity.Orderbook{
			SymbolID:  7,
			Timestamp: c.Time,
			Asks:      []entity.OrderbookEntry{{Price: c.Close * 1.01, Amount: 1.0}},
			Bids:      []entity.OrderbookEntry{{Price: c.Close * 0.99, Amount: 1.0}},
		})
	}
	replay := newOrderbookReplayForTest(snaps)

	cfg := entity.BacktestConfig{
		Symbol:          "BTC_JPY",
		SymbolID:        7,
		PrimaryInterval: "PT15M",
		FromTimestamp:   primary[0].Time,
		ToTimestamp:     primary[len(primary)-1].Time,
		InitialBalance:  100000,
		SpreadPercent:   0.1, // legacy model: 0.1% spread
	}
	risk := entity.RiskConfig{
		MaxPositionAmount: 1_000_000_000,
		MaxDailyLoss:      1_000_000_000,
		StopLossPercent:   5,
		TakeProfitPercent: 10,
		InitialCapital:    100000,
	}

	runner := NewBacktestRunner()
	pctResult, err := runner.Run(context.Background(), RunInput{
		Config: cfg, RiskConfig: risk, TradeAmount: 0.01,
		PrimaryCandles: primary,
	})
	if err != nil {
		t.Fatalf("percent run: %v", err)
	}

	cfg.SlippageModel = "orderbook"
	obResult, err := runner.Run(context.Background(), RunInput{
		Config: cfg, RiskConfig: risk, TradeAmount: 0.01,
		PrimaryCandles:  primary,
		FillPriceSource: replay,
	})
	if err != nil {
		t.Fatalf("orderbook run: %v", err)
	}

	if pctResult.Summary.TotalTrades == 0 {
		t.Skip("baseline produced no trades — extend the sine to ensure signal coverage")
	}
	if pctResult.Summary.FinalBalance == obResult.Summary.FinalBalance {
		t.Fatalf("expected different final balances (orderbook spread is 10x wider), got both = %f",
			pctResult.Summary.FinalBalance)
	}
}

// TestBacktestRunner_OrderbookReplayMissingSourceErrors makes sure runner
// rejects "orderbook" without a FillPriceSource — the handler is responsible
// for loading snapshots, not the runner.
func TestBacktestRunner_OrderbookReplayMissingSourceErrors(t *testing.T) {
	primary := []entity.Candle{
		{Time: 1, Open: 100, High: 100, Low: 100, Close: 100},
		{Time: 2, Open: 100, High: 100, Low: 100, Close: 100},
	}
	runner := NewBacktestRunner()
	_, err := runner.Run(context.Background(), RunInput{
		Config: entity.BacktestConfig{
			Symbol:          "BTC_JPY",
			SymbolID:        7,
			PrimaryInterval: "PT15M",
			FromTimestamp:   1, ToTimestamp: 2,
			InitialBalance: 100000,
			SlippageModel:  "orderbook",
		},
		RiskConfig:     entity.RiskConfig{InitialCapital: 100000, StopLossPercent: 5},
		TradeAmount:    0.01,
		PrimaryCandles: primary,
	})
	if err == nil {
		t.Fatal("expected error when slippageModel=orderbook but no FillPriceSource")
	}
}

// TestBacktestRunner_BookGateBlocksThinTradesEntirely confirms the runner
// wires the pre-trade gate into RiskHandler. With a snapshot whose ask side
// is far thinner than the requested lot, every signal must be rejected and
// the run produces zero trades.
func TestBacktestRunner_BookGateBlocksThinTradesEntirely(t *testing.T) {
	primary := make([]entity.Candle, 0, 60)
	baseTime := int64(1_770_000_000_000)
	price := 100.0
	for i := 0; i < 60; i++ {
		price += math.Sin(float64(i)/5.0) * 1.5
		ts := baseTime + int64(i)*15*60*1000
		primary = append(primary, entity.Candle{
			Open: price - 0.5, High: price + 1.0, Low: price - 1.0, Close: price, Time: ts,
		})
	}
	// Each bar gets a 1-tick-deep snapshot — way below the 0.01 trade size.
	snaps := make([]entity.Orderbook, 0, len(primary))
	for _, c := range primary {
		snaps = append(snaps, entity.Orderbook{
			SymbolID:  7,
			Timestamp: c.Time,
			Asks:      []entity.OrderbookEntry{{Price: c.Close * 1.001, Amount: 0.0001}},
			Bids:      []entity.OrderbookEntry{{Price: c.Close * 0.999, Amount: 0.0001}},
			BestAsk:   c.Close * 1.001,
			BestBid:   c.Close * 0.999,
			MidPrice:  c.Close,
		})
	}
	replay := newOrderbookReplayForTest(snaps)

	cfg := entity.BacktestConfig{
		Symbol:          "BTC_JPY",
		SymbolID:        7,
		PrimaryInterval: "PT15M",
		FromTimestamp:   primary[0].Time,
		ToTimestamp:     primary[len(primary)-1].Time,
		InitialBalance:  100000,
		SlippageModel:   "orderbook",
	}
	risk := entity.RiskConfig{
		MaxPositionAmount: 1_000_000_000,
		MaxDailyLoss:      1_000_000_000,
		StopLossPercent:   5,
		TakeProfitPercent: 10,
		InitialCapital:    100000,
		MaxSlippageBps:    50,
		MaxBookSidePct:    30,
	}
	runner := NewBacktestRunner()
	result, err := runner.Run(context.Background(), RunInput{
		Config:          cfg,
		RiskConfig:      risk,
		TradeAmount:     0.01,
		PrimaryCandles:  primary,
		FillPriceSource: replay,
		BookSource:      replay,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Summary.TotalTrades != 0 {
		t.Fatalf("expected book gate to block all trades, got %d", result.Summary.TotalTrades)
	}
}
