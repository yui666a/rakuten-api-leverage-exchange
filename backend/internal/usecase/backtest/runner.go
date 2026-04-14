package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	infra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type RunInput struct {
	Config         entity.BacktestConfig
	RiskConfig     entity.RiskConfig
	TradeAmount    float64
	PrimaryCandles []entity.Candle
	HigherCandles  []entity.Candle
}

type BacktestRunner struct {
	reporter *SummaryReporter
}

func NewBacktestRunner() *BacktestRunner {
	return &BacktestRunner{
		reporter: NewSummaryReporter(),
	}
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

	stanceResolver := usecase.NewRuleBasedStanceResolverWithOptions(nil, usecase.RuleBasedStanceResolverOptions{
		DisableOverride:    true,
		DisablePersistence: true,
	})
	strategyEngine := usecase.NewStrategyEngine(stanceResolver)
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
	indicatorHandler := NewIndicatorHandler(input.Config.PrimaryInterval, input.Config.HigherTFInterval, 500)
	strategyHandler := &StrategyHandler{Engine: strategyEngine}
	riskHandler := &RiskHandler{
		RiskManager: riskMgr,
		TradeAmount: input.TradeAmount,
	}
	executionHandler := &ExecutionHandler{
		Executor:    simAdapter,
		TradeAmount: input.TradeAmount,
	}

	bus := NewEventBus()
	bus.Register(entity.EventTypeCandle, 5, tickGenerator)
	bus.Register(entity.EventTypeCandle, 10, indicatorHandler)
	bus.Register(entity.EventTypeTick, 15, tickRiskHandler)
	bus.Register(entity.EventTypeIndicator, 20, strategyHandler)
	bus.Register(entity.EventTypeSignal, 30, riskHandler)
	bus.Register(entity.EventTypeApproved, 40, executionHandler)

	engine := NewEventEngine(bus)
	events := mergeCandleEvents(
		filterCandlesByRange(input.PrimaryCandles, input.Config.FromTimestamp, input.Config.ToTimestamp),
		filterCandlesByRange(input.HigherCandles, input.Config.FromTimestamp, input.Config.ToTimestamp),
		input.Config.PrimaryInterval,
		input.Config.HigherTFInterval,
		input.Config.SymbolID,
	)

	if err := engine.Run(ctx, events); err != nil {
		return nil, err
	}

	lastCandle := input.PrimaryCandles[len(input.PrimaryCandles)-1]
	for _, pos := range sim.Positions() {
		_, _, _ = sim.Close(pos.PositionID, lastCandle.Close, "end_of_test", lastCandle.Time)
	}

	trades := sim.ClosedTrades()
	summary := r.reporter.BuildSummary(input.Config, sim.Balance(), trades)
	result := &entity.BacktestResult{
		ID:        fmt.Sprintf("bt-%d", time.Now().UnixMilli()),
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

func (a *simExecutorAdapter) Positions() []SimPosition {
	raw := a.sim.Positions()
	out := make([]SimPosition, 0, len(raw))
	for _, p := range raw {
		out = append(out, SimPosition{
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
