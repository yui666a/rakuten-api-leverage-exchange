package backtest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/indicator"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

// ErrBacktestStrategyMissing is returned by StrategyHandler.Handle when the
// handler was constructed with a nil port.Strategy. Callers should use
// NewStrategyHandler so this path is only reachable through struct-literal
// construction that bypasses the constructor.
var ErrBacktestStrategyMissing = errors.New("backtest: strategy handler has no strategy")

// TickGeneratorHandler creates deterministic synthetic in-bar ticks from primary candles.
type TickGeneratorHandler struct {
	PrimaryInterval string
}

func (h *TickGeneratorHandler) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
	candleEvent, ok := event.(entity.CandleEvent)
	if !ok {
		return nil, nil
	}
	if h.PrimaryInterval == "" || candleEvent.Interval != h.PrimaryInterval {
		return nil, nil
	}

	durationMs, err := intervalDurationMillis(candleEvent.Interval)
	if err != nil {
		return nil, err
	}
	intervalStart := candleEvent.Candle.Time - durationMs

	t1 := intervalStart + durationMs/4
	t2 := intervalStart + durationMs/2
	t3 := intervalStart + durationMs*3/4
	t4 := candleEvent.Candle.Time

	openPrice := candleEvent.Candle.Open
	highPrice := candleEvent.Candle.High
	lowPrice := candleEvent.Candle.Low
	closePrice := candleEvent.Candle.Close

	prices := []struct {
		typ string
		val float64
		ts  int64
	}{
		{typ: "open", val: openPrice, ts: t1},
	}

	if closePrice >= openPrice {
		prices = append(prices,
			struct {
				typ string
				val float64
				ts  int64
			}{typ: "high", val: highPrice, ts: t2},
			struct {
				typ string
				val float64
				ts  int64
			}{typ: "low", val: lowPrice, ts: t3},
		)
	} else {
		prices = append(prices,
			struct {
				typ string
				val float64
				ts  int64
			}{typ: "low", val: lowPrice, ts: t2},
			struct {
				typ string
				val float64
				ts  int64
			}{typ: "high", val: highPrice, ts: t3},
		)
	}
	prices = append(prices, struct {
		typ string
		val float64
		ts  int64
	}{typ: "close", val: closePrice, ts: t4})

	events := make([]entity.Event, 0, len(prices))
	for _, p := range prices {
		events = append(events, entity.TickEvent{
			SymbolID:   candleEvent.SymbolID,
			Interval:   candleEvent.Interval,
			Price:      p.val,
			Timestamp:  p.ts,
			TickType:   p.typ,
			ParentTime: candleEvent.Candle.Time,
			BarLow:     candleEvent.Candle.Low,
			BarHigh:    candleEvent.Candle.High,
		})
	}
	return events, nil
}

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

// StrategyHandler converts IndicatorEvent to SignalEvent using a Strategy.
// It depends on the port.Strategy abstraction so the concrete implementation
// (DefaultStrategy wrapping StrategyEngine today, a ConfigurableStrategy later)
// can be swapped at the composition root without touching the handler chain.
//
// Construct via NewStrategyHandler to guarantee a non-nil strategy. The
// Handle method keeps a sentinel check as defense-in-depth for struct-literal
// construction that bypasses the constructor.
type StrategyHandler struct {
	Strategy port.Strategy
}

// NewStrategyHandler returns a StrategyHandler that delegates to s. It panics
// if s is nil — the non-nil strategy is a composition-root invariant, so a nil
// argument represents a programmer error that should fail loudly at startup.
func NewStrategyHandler(s port.Strategy) *StrategyHandler {
	if s == nil {
		panic("backtest: NewStrategyHandler strategy must not be nil")
	}
	return &StrategyHandler{Strategy: s}
}

func (h *StrategyHandler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
	indicatorEvent, ok := event.(entity.IndicatorEvent)
	if !ok {
		return nil, nil
	}
	if h.Strategy == nil {
		return nil, ErrBacktestStrategyMissing
	}

	indicators := indicatorEvent.Primary
	signal, err := h.Strategy.Evaluate(
		ctx,
		&indicators,
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

// TickRiskExecutor exposes minimum close-related operations for tick-driven risk checks.
type TickRiskExecutor interface {
	Positions() []eventengine.Position
	SelectSLTPExit(side entity.OrderSide, stopLossPrice, takeProfitPrice, barLow, barHigh float64) (float64, string, bool)
	Close(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error)
}

// TickRiskHandler evaluates SL/TP/TrailingStop on synthetic ticks.
//
// Trailing distance policy (PR-12):
//   - TrailingATRMultiplier > 0 かつ currentATR > 0 → ATR × multiplier
//   - それ以外 → EntryPrice × StopLossPercent / 100 （従来挙動）
//   - 両方に値があるときは「より大きい距離（保守的＝早期決済を抑える）」を採用
//
// StopLossATRMultiplier はエントリー時の静的 SL に反映される。TP/SL 判定に
// 使うハード SL 価格をエントリー価格からの距離で計算する calcSLTP に
// 流し込む設計にするため、現行の SL 計算も同等のポリシー（percent
// フォールバック＋ ATR 上書き）で扱う。
type TickRiskHandler struct {
	PrimaryInterval       string
	Executor              TickRiskExecutor
	StopLossPercent       float64
	StopLossATRMultiplier float64 // >0 なら ATR×mult の大きい方を SL 距離として採用
	TrailingATRMultiplier float64 // >0 ならトレイリング距離も ATR ベース
	TakeProfitPercent     float64
	highWaterMarks        map[int64]float64
	currentATR            float64
}

func NewTickRiskHandler(primaryInterval string, executor TickRiskExecutor, stopLossPercent, takeProfitPercent float64) *TickRiskHandler {
	return &TickRiskHandler{
		PrimaryInterval:   primaryInterval,
		Executor:          executor,
		StopLossPercent:   stopLossPercent,
		TakeProfitPercent: takeProfitPercent,
		highWaterMarks:    make(map[int64]float64),
	}
}

// SetATRMultipliers configures the ATR-based stop-loss and trailing-stop
// multipliers after construction. Zero multipliers keep the handler on its
// legacy percent-based behaviour.
func (h *TickRiskHandler) SetATRMultipliers(stopLossATR, trailingATR float64) {
	h.StopLossATRMultiplier = stopLossATR
	h.TrailingATRMultiplier = trailingATR
}

// UpdateATR is called by the IndicatorHandler (or a test fixture) whenever a
// fresh primary-interval ATR value is available. Zero or NaN is ignored.
func (h *TickRiskHandler) UpdateATR(atr float64) {
	if atr <= 0 || atr != atr { // NaN check
		return
	}
	h.currentATR = atr
}

// stopLossDistance returns the per-side SL distance in price units used by
// the SL/TP check and by trailing-stop reversal. When both a percent SL and
// an ATR SL are active, the farther (more conservative) one wins so a
// volatile tick cannot immediately stop the position out.
func (h *TickRiskHandler) stopLossDistance(entryPrice float64) float64 {
	percentDist := entryPrice * h.StopLossPercent / 100.0
	atrDist := 0.0
	if h.StopLossATRMultiplier > 0 && h.currentATR > 0 {
		atrDist = h.currentATR * h.StopLossATRMultiplier
	}
	if atrDist > percentDist {
		return atrDist
	}
	return percentDist
}

// trailingDistance applies the same policy for the trailing reversal: ATR
// when configured and known, otherwise fall back to the percent-derived
// distance; take the bigger of the two when both are active.
func (h *TickRiskHandler) trailingDistance(entryPrice float64) float64 {
	percentDist := entryPrice * h.StopLossPercent / 100.0
	atrDist := 0.0
	if h.TrailingATRMultiplier > 0 && h.currentATR > 0 {
		atrDist = h.currentATR * h.TrailingATRMultiplier
	}
	if atrDist > percentDist {
		return atrDist
	}
	return percentDist
}

func (h *TickRiskHandler) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
	// ATR は IndicatorEvent から抽出。IndicatorHandler が CandleEvent を
	// 受けて IndicatorEvent を emit するので、その直後に ATR が最新化される。
	// Trailing stop / ATR SL はこの最新値を使う。
	if indicatorEvent, ok := event.(entity.IndicatorEvent); ok {
		if indicatorEvent.Primary.ATR14 != nil {
			h.UpdateATR(*indicatorEvent.Primary.ATR14)
		}
		return nil, nil
	}

	tickEvent, ok := event.(entity.TickEvent)
	if !ok {
		return nil, nil
	}
	if h.PrimaryInterval != "" && tickEvent.Interval != h.PrimaryInterval {
		return nil, nil
	}
	if h.Executor == nil {
		return nil, fmt.Errorf("tick risk executor is nil")
	}

	positions := h.Executor.Positions()
	active := make(map[int64]bool, len(positions))
	emitted := make([]entity.Event, 0)

	for _, pos := range positions {
		if pos.SymbolID != tickEvent.SymbolID {
			continue
		}
		active[pos.PositionID] = true

		// TP/SL: decide with bar range and worst-case policy.
		if h.StopLossPercent > 0 && h.TakeProfitPercent > 0 {
			slDistance := h.stopLossDistance(pos.EntryPrice)
			tpDistance := pos.EntryPrice * h.TakeProfitPercent / 100.0
			stopLossPrice, takeProfitPrice := calcSLTPFromDistances(pos, slDistance, tpDistance)
			exitPrice, reason, hit := h.Executor.SelectSLTPExit(
				pos.Side,
				stopLossPrice,
				takeProfitPrice,
				tickEvent.BarLow,
				tickEvent.BarHigh,
			)
			if hit {
				orderEvent, _, err := h.Executor.Close(pos.PositionID, exitPrice, reason, tickEvent.Timestamp)
				if err != nil {
					return nil, err
				}
				emitted = append(emitted, orderEvent)
				delete(h.highWaterMarks, pos.PositionID)
				continue
			}
		}

		// Trailing stop: use stop-loss distance for reversal distance.
		best, ok := h.highWaterMarks[pos.PositionID]
		if !ok {
			best = pos.EntryPrice
		}
		if pos.Side == entity.OrderSideBuy {
			if tickEvent.Price > best {
				best = tickEvent.Price
			}
		} else {
			if tickEvent.Price < best {
				best = tickEvent.Price
			}
		}
		h.highWaterMarks[pos.PositionID] = best

		distance := h.trailingDistance(pos.EntryPrice)
		if distance <= 0 {
			continue
		}
		if pos.Side == entity.OrderSideBuy {
			if best > pos.EntryPrice && best-tickEvent.Price >= distance {
				orderEvent, _, err := h.Executor.Close(pos.PositionID, tickEvent.Price, "trailing_stop", tickEvent.Timestamp)
				if err != nil {
					return nil, err
				}
				emitted = append(emitted, orderEvent)
				delete(h.highWaterMarks, pos.PositionID)
			}
		} else {
			if best < pos.EntryPrice && tickEvent.Price-best >= distance {
				orderEvent, _, err := h.Executor.Close(pos.PositionID, tickEvent.Price, "trailing_stop", tickEvent.Timestamp)
				if err != nil {
					return nil, err
				}
				emitted = append(emitted, orderEvent)
				delete(h.highWaterMarks, pos.PositionID)
			}
		}
	}

	for positionID := range h.highWaterMarks {
		if !active[positionID] {
			delete(h.highWaterMarks, positionID)
		}
	}

	return emitted, nil
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

	// Volume indicators
	volumes := make([]float64, n)
	for i, c := range candles {
		volumes[i] = c.Volume
	}
	volSMA := indicator.VolumeSMA(volumes, 20)
	result.VolumeSMA20 = floatToPtr(volSMA)
	if !math.IsNaN(volSMA) && volSMA > 0 && n > 0 {
		vr := indicator.VolumeRatio(volumes[n-1], volSMA)
		result.VolumeRatio = floatToPtr(vr)
	}

	// RecentSqueeze: check if any of the last 5 candles had BBBandwidth < 0.02
	if n >= 20 {
		recentSqueeze := false
		lookback := 5
		if lookback > n-19 {
			lookback = n - 19
		}
		for i := 0; i < lookback; i++ {
			offset := n - 1 - i
			windowCloses := closes[:offset+1]
			_, _, _, bw := indicator.BollingerBands(windowCloses, 20, 2.0)
			if !math.IsNaN(bw) && bw < 0.02 {
				recentSqueeze = true
				break
			}
		}
		result.RecentSqueeze = &recentSqueeze
	}

	return result
}

func floatToPtr(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}

func intervalDurationMillis(interval string) (int64, error) {
	if !strings.HasPrefix(interval, "PT") {
		return 0, fmt.Errorf("unsupported interval: %s", interval)
	}
	body := strings.TrimPrefix(interval, "PT")
	if strings.HasSuffix(body, "M") {
		n, err := strconv.Atoi(strings.TrimSuffix(body, "M"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid minute interval: %s", interval)
		}
		return int64(n) * int64(time.Minute/time.Millisecond), nil
	}
	if strings.HasSuffix(body, "H") {
		n, err := strconv.Atoi(strings.TrimSuffix(body, "H"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid hour interval: %s", interval)
		}
		return int64(n) * int64(time.Hour/time.Millisecond), nil
	}
	return 0, fmt.Errorf("unsupported interval: %s", interval)
}

// calcSLTPFromDistances produces the same (stopLossPrice, takeProfitPrice)
// shape as calcSLTP but from pre-computed price-unit distances. PR-12 uses
// this so the SL distance can be ATR-derived without re-encoding the
// per-side sign handling.
func calcSLTPFromDistances(pos eventengine.Position, slDistance, tpDistance float64) (stopLossPrice float64, takeProfitPrice float64) {
	if pos.Side == entity.OrderSideSell {
		stopLossPrice = pos.EntryPrice + slDistance
		takeProfitPrice = pos.EntryPrice - tpDistance
	} else {
		stopLossPrice = pos.EntryPrice - slDistance
		takeProfitPrice = pos.EntryPrice + tpDistance
	}
	return
}

func calcSLTP(pos eventengine.Position, stopLossPercent, takeProfitPercent float64) (stopLossPrice float64, takeProfitPrice float64) {
	switch pos.Side {
	case entity.OrderSideSell:
		stopLossPrice = pos.EntryPrice * (1 + stopLossPercent/100.0)
		takeProfitPrice = pos.EntryPrice * (1 - takeProfitPercent/100.0)
	default:
		stopLossPrice = pos.EntryPrice * (1 - stopLossPercent/100.0)
		takeProfitPrice = pos.EntryPrice * (1 + takeProfitPercent/100.0)
	}
	return stopLossPrice, takeProfitPrice
}
