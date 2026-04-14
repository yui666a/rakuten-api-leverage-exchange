package backtest

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/indicator"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// IndicatorHandler calculates indicator snapshots from buffered candles.
// Buffers are maintained oldest-first to keep indicator calculations path-correct.
type IndicatorHandler struct {
	PrimaryInterval  string
	HigherTFInterval string
	BufferSize       int

	primaryCandles map[int64][]entity.Candle
	higherCandles  map[int64][]entity.Candle
}

func NewIndicatorHandler(primaryInterval, higherTFInterval string, bufferSize int) *IndicatorHandler {
	if bufferSize <= 0 {
		bufferSize = 500
	}
	return &IndicatorHandler{
		PrimaryInterval:  primaryInterval,
		HigherTFInterval: higherTFInterval,
		BufferSize:       bufferSize,
		primaryCandles:   make(map[int64][]entity.Candle),
		higherCandles:    make(map[int64][]entity.Candle),
	}
}

func (h *IndicatorHandler) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
	candleEvent, ok := event.(entity.CandleEvent)
	if !ok {
		return nil, nil
	}

	switch candleEvent.Interval {
	case h.PrimaryInterval:
		h.primaryCandles[candleEvent.SymbolID] = appendCapped(h.primaryCandles[candleEvent.SymbolID], candleEvent.Candle, h.BufferSize)
		primary := calculateIndicatorSet(candleEvent.SymbolID, h.primaryCandles[candleEvent.SymbolID])

		var higherTF *entity.IndicatorSet
		if h.HigherTFInterval != "" {
			if selected := selectCandlesAtOrBefore(h.higherCandles[candleEvent.SymbolID], candleEvent.Timestamp); len(selected) > 0 {
				set := calculateIndicatorSet(candleEvent.SymbolID, selected)
				higherTF = &set
			}
		}

		return []entity.Event{
			entity.IndicatorEvent{
				SymbolID:  candleEvent.SymbolID,
				Interval:  candleEvent.Interval,
				Primary:   primary,
				HigherTF:  higherTF,
				LastPrice: candleEvent.Candle.Close,
				Timestamp: candleEvent.Timestamp,
			},
		}, nil

	case h.HigherTFInterval:
		h.higherCandles[candleEvent.SymbolID] = appendCapped(h.higherCandles[candleEvent.SymbolID], candleEvent.Candle, h.BufferSize)
	}

	return nil, nil
}

// StrategyHandler converts IndicatorEvent to SignalEvent using StrategyEngine.
type StrategyHandler struct {
	Engine *usecase.StrategyEngine
}

func (h *StrategyHandler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
	indicatorEvent, ok := event.(entity.IndicatorEvent)
	if !ok {
		return nil, nil
	}
	if h.Engine == nil {
		return nil, fmt.Errorf("strategy engine is nil")
	}

	signal, err := h.Engine.EvaluateWithHigherTFAt(
		ctx,
		indicatorEvent.Primary,
		indicatorEvent.HigherTF,
		indicatorEvent.LastPrice,
		time.UnixMilli(indicatorEvent.Timestamp),
	)
	if err != nil {
		return nil, err
	}
	if signal == nil || signal.Action == entity.SignalActionHold {
		return nil, nil
	}

	return []entity.Event{
		entity.SignalEvent{
			Signal:    *signal,
			Price:     indicatorEvent.LastPrice,
			Timestamp: indicatorEvent.Timestamp,
		},
	}, nil
}

// RiskHandler gates SignalEvents using RiskManager with injected event time.
type RiskHandler struct {
	RiskManager *usecase.RiskManager
	TradeAmount float64
}

func (h *RiskHandler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
	signalEvent, ok := event.(entity.SignalEvent)
	if !ok {
		return nil, nil
	}
	if h.RiskManager == nil {
		return nil, fmt.Errorf("risk manager is nil")
	}
	if h.TradeAmount <= 0 {
		return nil, fmt.Errorf("trade amount must be positive")
	}

	side := entity.OrderSideBuy
	if signalEvent.Signal.Action == entity.SignalActionSell {
		side = entity.OrderSideSell
	}
	proposal := entity.OrderProposal{
		SymbolID: signalEvent.Signal.SymbolID,
		Side:     side,
		Amount:   h.TradeAmount,
		Price:    signalEvent.Price,
		IsClose:  false,
	}

	check := h.RiskManager.CheckOrderAt(ctx, time.UnixMilli(signalEvent.Timestamp), proposal)
	if !check.Approved {
		return nil, nil
	}

	return []entity.Event{
		entity.ApprovedSignalEvent{
			Signal:    signalEvent.Signal,
			Price:     signalEvent.Price,
			Timestamp: signalEvent.Timestamp,
		},
	}, nil
}

// SignalExecutor opens simulated orders from approved signals.
type SignalExecutor interface {
	Open(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64) (entity.OrderEvent, error)
}

// ExecutionHandler converts approved SignalEvents into OrderEvents.
type ExecutionHandler struct {
	Executor    SignalExecutor
	TradeAmount float64
}

func (h *ExecutionHandler) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
	signalEvent, ok := event.(entity.ApprovedSignalEvent)
	if !ok {
		return nil, nil
	}
	if h.Executor == nil {
		return nil, fmt.Errorf("executor is nil")
	}
	if h.TradeAmount <= 0 {
		return nil, fmt.Errorf("trade amount must be positive")
	}

	side := entity.OrderSideBuy
	if signalEvent.Signal.Action == entity.SignalActionSell {
		side = entity.OrderSideSell
	}

	orderEvent, err := h.Executor.Open(
		signalEvent.Signal.SymbolID,
		side,
		signalEvent.Price,
		h.TradeAmount,
		signalEvent.Signal.Reason,
		signalEvent.Timestamp,
	)
	if err != nil {
		return nil, err
	}
	return []entity.Event{orderEvent}, nil
}

func appendCapped(candles []entity.Candle, candle entity.Candle, capSize int) []entity.Candle {
	candles = append(candles, candle)
	if len(candles) > capSize {
		candles = candles[len(candles)-capSize:]
	}
	return candles
}

func selectCandlesAtOrBefore(candles []entity.Candle, timestamp int64) []entity.Candle {
	idx := -1
	for i := len(candles) - 1; i >= 0; i-- {
		if candles[i].Time <= timestamp {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	return candles[:idx+1]
}

func calculateIndicatorSet(symbolID int64, candles []entity.Candle) entity.IndicatorSet {
	n := len(candles)
	if n == 0 {
		return entity.IndicatorSet{SymbolID: symbolID}
	}

	closes := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
	}

	result := entity.IndicatorSet{
		SymbolID:  symbolID,
		SMA20:     floatToPtr(indicator.SMA(closes, 20)),
		SMA50:     floatToPtr(indicator.SMA(closes, 50)),
		EMA12:     floatToPtr(indicator.EMA(closes, 12)),
		EMA26:     floatToPtr(indicator.EMA(closes, 26)),
		RSI14:     floatToPtr(indicator.RSI(closes, 14)),
		Timestamp: candles[n-1].Time,
	}

	macdLine, signalLine, histogram := indicator.MACD(closes, 12, 26, 9)
	result.MACDLine = floatToPtr(macdLine)
	result.SignalLine = floatToPtr(signalLine)
	result.Histogram = floatToPtr(histogram)

	bbUpper, bbMiddle, bbLower, bbBandwidth := indicator.BollingerBands(closes, 20, 2.0)
	result.BBUpper = floatToPtr(bbUpper)
	result.BBMiddle = floatToPtr(bbMiddle)
	result.BBLower = floatToPtr(bbLower)
	result.BBBandwidth = floatToPtr(bbBandwidth)

	result.ATR14 = floatToPtr(indicator.ATR(highs, lows, closes, 14))

	return result
}

func floatToPtr(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}
