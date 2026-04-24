package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	infra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/positionsize"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

type RunInput struct {
	Config         entity.BacktestConfig
	RiskConfig     entity.RiskConfig
	TradeAmount    float64
	PrimaryCandles []entity.Candle
	HigherCandles  []entity.Candle

	// BBSqueezeLookback is the window (bars) the IndicatorHandler uses to
	// detect a recent BB squeeze. cycle44: plumbed through from the
	// profile's stance_rules.bb_squeeze_lookback so the legacy hardcoded
	// 5 no longer dominates. Zero means "use the legacy default of 5" for
	// callers that don't set a profile (baseline DefaultStrategy runs).
	BBSqueezeLookback int

	// PositionSizing declares dynamic lot sizing for the run. nil / zero-value
	// keeps the legacy fixed-lot behaviour (TradeAmount is used verbatim on
	// every approved signal).
	PositionSizing *entity.PositionSizingConfig

	// MinConfidence mirrors the live pipeline's minConfidence so the sizer's
	// confidence scaling matches the live path. 0 disables confidence
	// scaling (the sizer passes the multiplier through as 1.0).
	MinConfidence float64
}

// RunnerOption tunes optional aspects of a BacktestRunner at construction.
//
// Added to support PDCA strategy-profile selection (spec §8): callers that
// want to drive the run with a ConfigurableStrategy (or any other
// port.Strategy implementation) pass WithStrategy(...). Runners constructed
// without any option keep the historical behaviour of building a fresh
// DefaultStrategy per run.
type RunnerOption func(*BacktestRunner)

// WithStrategy sets a custom port.Strategy for the runner. A nil value is
// ignored so callers can pass a strategy that may or may not be configured
// without an extra branch at the call site.
func WithStrategy(s port.Strategy) RunnerOption {
	return func(r *BacktestRunner) {
		if s != nil {
			r.strategy = s
		}
	}
}

type BacktestRunner struct {
	reporter *SummaryReporter
	// strategy is optional. When nil, Run builds the legacy DefaultStrategy.
	// When non-nil (typically a ConfigurableStrategy), Run uses it directly.
	strategy port.Strategy
}

func NewBacktestRunner(opts ...RunnerOption) *BacktestRunner {
	r := &BacktestRunner{
		reporter: NewSummaryReporter(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

func (r *BacktestRunner) Run(ctx context.Context, input RunInput) (*entity.BacktestResult, error) {
	if len(input.PrimaryCandles) == 0 {
		return nil, fmt.Errorf("primary candles are required")
	}
	if input.TradeAmount <= 0 {
		return nil, fmt.Errorf("trade amount must be positive")
	}
	if input.Config.InitialBalance <= 0 {
		return nil, fmt.Errorf("initial balance must be positive")
	}

	riskCfg := input.RiskConfig
	if riskCfg.InitialCapital <= 0 {
		riskCfg.InitialCapital = input.Config.InitialBalance
	}
	if riskCfg.MaxPositionAmount <= 0 {
		riskCfg.MaxPositionAmount = math.MaxFloat64 / 4
	}
	if riskCfg.MaxDailyLoss <= 0 {
		riskCfg.MaxDailyLoss = math.MaxFloat64 / 4
	}
	if riskCfg.StopLossPercent <= 0 {
		riskCfg.StopLossPercent = 5
	}

	// Strategy selection: prefer the caller-supplied strategy (set via
	// WithStrategy) so PDCA runs can use a ConfigurableStrategy. Fall back
	// to the hard-coded DefaultStrategy for legacy callers.
	strategy := r.strategy
	if strategy == nil {
		stanceResolver := usecase.NewRuleBasedStanceResolverWithOptions(nil, usecase.RuleBasedStanceResolverOptions{
			DisableOverride:    true,
			DisablePersistence: true,
		})
		strategyEngine := usecase.NewStrategyEngine(stanceResolver)
		strategy = strategyuc.NewDefaultStrategy(strategyEngine)
	}
	riskMgr := usecase.NewRiskManager(riskCfg)

	sim := infra.NewSimExecutor(infra.SimConfig{
		InitialBalance:    input.Config.InitialBalance,
		SpreadPercent:     input.Config.SpreadPercent,
		DailyCarryingCost: input.Config.DailyCarryCost,
		SlippagePercent:   input.Config.SlippagePercent,
	})
	simAdapter := &simExecutorAdapter{sim: sim}

	tickGenerator := &TickGeneratorHandler{PrimaryInterval: input.Config.PrimaryInterval}
	tickRiskHandler := NewTickRiskHandler(
		input.Config.PrimaryInterval,
		simAdapter,
		riskCfg.StopLossPercent,
		riskCfg.TakeProfitPercent,
	)
	// PR-12: propagate ATR-based SL / trailing multipliers from the run's
	// RiskConfig so profile-driven ATR settings actually reach the tick
	// risk loop. Without this, the legacy percent path stays in effect
	// regardless of what the profile says.
	tickRiskHandler.SetATRMultipliers(riskCfg.StopLossATRMultiplier, riskCfg.TrailingATRMultiplier)
	indicatorHandler := NewIndicatorHandler(input.Config.PrimaryInterval, input.Config.HigherTFInterval, 500)
	// cycle44: honour the profile's bb_squeeze_lookback by overriding the
	// legacy default. Zero keeps the legacy 5 so DefaultStrategy runs (no
	// profile in scope) see the same RecentSqueeze behaviour as pre-cycle44.
	if input.BBSqueezeLookback > 0 {
		indicatorHandler.SetBBSqueezeLookback(input.BBSqueezeLookback)
	}
	strategyHandler := NewStrategyHandler(strategy)
	riskHandler := &RiskHandler{
		RiskManager:     riskMgr,
		TradeAmount:     input.TradeAmount,
		StopLossPercent: riskCfg.StopLossPercent,
		MinConfidence:   input.MinConfidence,
	}
	if ps := input.PositionSizing; ps != nil && ps.Mode != "" && ps.Mode != "fixed" {
		defaults := positionsize.VenueDefaults(input.Config.Symbol)
		riskHandler.Sizer = positionsize.New(ps, defaults)
		riskHandler.Equity = EquityFunc(func() float64 { return sim.Balance() })
		riskHandler.Peak = NewPeakTracker(input.Config.InitialBalance)
	}
	executionHandler := &ExecutionHandler{
		Executor:    simAdapter,
		TradeAmount: input.TradeAmount,
	}

	bus := eventengine.NewEventBus()
	bus.Register(entity.EventTypeCandle, 5, tickGenerator)
	bus.Register(entity.EventTypeCandle, 10, indicatorHandler)
	// Run tickRiskHandler on IndicatorEvent (priority 12, strictly before
	// the strategy at priority 20) so the new ATR value is already in place
	// by the time the risk loop sees the next TickEvent.
	bus.Register(entity.EventTypeIndicator, 12, tickRiskHandler)
	bus.Register(entity.EventTypeTick, 15, tickRiskHandler)
	bus.Register(entity.EventTypeIndicator, 20, strategyHandler)
	bus.Register(entity.EventTypeSignal, 30, riskHandler)
	bus.Register(entity.EventTypeApproved, 40, executionHandler)

	primaryCandles := filterCandlesByRange(input.PrimaryCandles, input.Config.FromTimestamp, input.Config.ToTimestamp)
	higherCandles := filterCandlesByRange(input.HigherCandles, input.Config.FromTimestamp, input.Config.ToTimestamp)
	if len(primaryCandles) == 0 {
		return nil, fmt.Errorf("no primary candles in requested range")
	}

	engine := eventengine.NewEventEngine(bus)
	events := mergeCandleEvents(
		primaryCandles,
		higherCandles,
		input.Config.PrimaryInterval,
		input.Config.HigherTFInterval,
		input.Config.SymbolID,
	)
	equityPoints := make([]EquityPoint, 0, len(primaryCandles)+1)
	equityPoints = append(equityPoints, EquityPoint{
		Timestamp: input.Config.FromTimestamp,
		Equity:    input.Config.InitialBalance,
	})

	for _, ev := range events {
		if err := engine.Run(ctx, []entity.Event{ev}); err != nil {
			return nil, err
		}
		candleEvent, ok := ev.(entity.CandleEvent)
		if !ok || candleEvent.Interval != input.Config.PrimaryInterval {
			continue
		}
		equityNow := sim.Equity(map[int64]float64{input.Config.SymbolID: candleEvent.Candle.Close})
		equityPoints = append(equityPoints, EquityPoint{
			Timestamp: candleEvent.Timestamp,
			Equity:    equityNow,
		})
		if riskHandler.Peak != nil {
			riskHandler.Peak.Observe(equityNow)
		}
	}

	lastCandle := primaryCandles[len(primaryCandles)-1]
	for _, pos := range sim.Positions() {
		_, _, _ = sim.Close(pos.PositionID, lastCandle.Close, "end_of_test", lastCandle.Time)
	}

	trades := sim.ClosedTrades()

	summary := r.reporter.BuildSummary(input.Config, sim.Balance(), trades, equityPoints)
	id, err := NewULID()
	if err != nil {
		return nil, err
	}
	result := &entity.BacktestResult{
		ID:        id,
		CreatedAt: time.Now().Unix(),
		Config:    input.Config,
		Summary:   summary,
		Trades:    trades,
	}
	return result, nil
}

func filterCandlesByRange(candles []entity.Candle, from, to int64) []entity.Candle {
	if from == 0 && to == 0 {
		out := make([]entity.Candle, len(candles))
		copy(out, candles)
		sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })
		return out
	}

	out := make([]entity.Candle, 0, len(candles))
	for _, c := range candles {
		if from > 0 && c.Time < from {
			continue
		}
		if to > 0 && c.Time > to {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out
}

func mergeCandleEvents(primary, higher []entity.Candle, primaryInterval, higherInterval string, symbolID int64) []entity.Event {
	events := make([]entity.Event, 0, len(primary)+len(higher))
	i := 0
	j := 0

	for i < len(primary) || j < len(higher) {
		if i >= len(primary) {
			c := higher[j]
			events = append(events, entity.CandleEvent{SymbolID: symbolID, Interval: higherInterval, Candle: c, Timestamp: c.Time})
			j++
			continue
		}
		if j >= len(higher) {
			c := primary[i]
			events = append(events, entity.CandleEvent{SymbolID: symbolID, Interval: primaryInterval, Candle: c, Timestamp: c.Time})
			i++
			continue
		}

		p := primary[i]
		h := higher[j]
		if h.Time <= p.Time {
			events = append(events, entity.CandleEvent{SymbolID: symbolID, Interval: higherInterval, Candle: h, Timestamp: h.Time})
			j++
		} else {
			events = append(events, entity.CandleEvent{SymbolID: symbolID, Interval: primaryInterval, Candle: p, Timestamp: p.Time})
			i++
		}
	}

	return events
}

type simExecutorAdapter struct {
	sim *infra.SimExecutor
}

func (a *simExecutorAdapter) Open(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64) (entity.OrderEvent, error) {
	return a.sim.Open(symbolID, side, signalPrice, amount, reason, timestamp)
}

func (a *simExecutorAdapter) Positions() []eventengine.Position {
	raw := a.sim.Positions()
	out := make([]eventengine.Position, 0, len(raw))
	for _, p := range raw {
		out = append(out, eventengine.Position{
			PositionID:     p.PositionID,
			SymbolID:       p.SymbolID,
			Side:           p.Side,
			EntryPrice:     p.EntryPrice,
			Amount:         p.Amount,
			EntryTimestamp: p.EntryTimestamp,
		})
	}
	return out
}

func (a *simExecutorAdapter) SelectSLTPExit(side entity.OrderSide, stopLossPrice, takeProfitPrice, barLow, barHigh float64) (float64, string, bool) {
	return a.sim.SelectSLTPExit(side, stopLossPrice, takeProfitPrice, barLow, barHigh)
}

func (a *simExecutorAdapter) Close(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error) {
	return a.sim.Close(positionID, signalPrice, reason, timestamp)
}
