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
	infra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/indicator"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/booklimit"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

// infraThinBookError is a re-export alias so handlers can detect the
// orderbook-replay's "thin book" sentinel without leaking the infra package
// name everywhere.
type infraThinBookError = infra.ThinBookError

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

	// bbSqueezeLookback is the window used to detect a recent BB squeeze.
	// cycle44: defaults to 5 in NewIndicatorHandler to match legacy
	// behaviour; callers that load a profile should override via
	// SetBBSqueezeLookback so the profile's stance rule actually takes
	// effect (cycle43 discovered this field was a silent no-op).
	bbSqueezeLookback int

	primaryCandles map[int64][]entity.Candle
	higherCandles  map[int64][]entity.Candle
}

func NewIndicatorHandler(primaryInterval, higherTFInterval string, bufferSize int) *IndicatorHandler {
	if bufferSize <= 0 {
		bufferSize = 500
	}
	return &IndicatorHandler{
		PrimaryInterval:   primaryInterval,
		HigherTFInterval:  higherTFInterval,
		BufferSize:        bufferSize,
		bbSqueezeLookback: 5, // cycle44: legacy default, overridable via SetBBSqueezeLookback
		primaryCandles:    make(map[int64][]entity.Candle),
		higherCandles:     make(map[int64][]entity.Candle),
	}
}

// SetBBSqueezeLookback overrides the window used to detect a recent BB
// squeeze. cycle44: the legacy code hardcoded 5; routers / backtest runners
// now pass `profile.StanceRules.BBSqueezeLookback` here so the profile's
// stance-rule actually takes effect. Zero means "no squeeze window" which
// yields RecentSqueeze=false permanently (matches the "disable the gate"
// convention from the other cycle43-era int axes).
func (h *IndicatorHandler) SetBBSqueezeLookback(n int) {
	if n < 0 {
		n = 0
	}
	h.bbSqueezeLookback = n
}

func (h *IndicatorHandler) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
	candleEvent, ok := event.(entity.CandleEvent)
	if !ok {
		return nil, nil
	}

	switch candleEvent.Interval {
	case h.PrimaryInterval:
		h.primaryCandles[candleEvent.SymbolID] = appendCapped(h.primaryCandles[candleEvent.SymbolID], candleEvent.Candle, h.BufferSize)
		primary := calculateIndicatorSet(candleEvent.SymbolID, h.primaryCandles[candleEvent.SymbolID], h.bbSqueezeLookback)

		var higherTF *entity.IndicatorSet
		if h.HigherTFInterval != "" {
			if selected := selectCandlesAtOrBefore(h.higherCandles[candleEvent.SymbolID], candleEvent.Timestamp); len(selected) > 0 {
				set := calculateIndicatorSet(candleEvent.SymbolID, selected, h.bbSqueezeLookback)
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

	var atr float64
	if indicators.ATR14 != nil {
		atr = *indicators.ATR14
	}
	return []entity.Event{
		entity.SignalEvent{
			Signal:     *signal,
			Price:      indicatorEvent.LastPrice,
			Timestamp:  indicatorEvent.Timestamp,
			CurrentATR: atr,
		},
	}, nil
}

// EquityProvider exposes the running account equity to the risk handler so
// the position sizer can compute risk_pct lots from the *current* balance
// rather than the static initial balance. backtest wires it to the SimExecutor;
// live code can pass a PositionManager-backed adapter.
type EquityProvider interface {
	Equity() float64
}

// EquityFunc adapts a plain closure into an EquityProvider, useful for tests.
type EquityFunc func() float64

func (f EquityFunc) Equity() float64 { return f() }

// PeakTracker keeps the running peak equity and exposes the current drawdown
// in percent. A nil tracker is treated as "no DD scaling" — the sizer sees 0.
type PeakTracker struct {
	peak float64
}

func NewPeakTracker(initial float64) *PeakTracker {
	return &PeakTracker{peak: initial}
}

// Observe feeds the current equity into the tracker and returns the new DD%.
func (p *PeakTracker) Observe(equity float64) float64 {
	if p == nil {
		return 0
	}
	if equity > p.peak {
		p.peak = equity
	}
	if p.peak <= 0 {
		return 0
	}
	dd := (p.peak - equity) / p.peak
	if dd < 0 {
		return 0
	}
	return dd * 100
}

// SignalSizer is the narrow port the RiskHandler uses to compute a lot. It
// matches positionsize.Sizer.Compute so backtest and live code share one
// implementation without an import-cycle gymnastic.
type SignalSizer interface {
	Sized(requested, entryPrice, slPercent, equity, atr, ddPct, confidence, minConfidence float64) (amount float64, skipReason string)
}

// RiskHandler gates SignalEvents using RiskManager with injected event time
// and (optionally) a position sizer that decides the lot per trade.
type RiskHandler struct {
	RiskManager *usecase.RiskManager
	// TradeAmount is the fixed/requested lot in fixed-sizing mode and the
	// baseline for the sizer when one is attached.
	TradeAmount float64
	// Sizer is optional. When non-nil, every approved signal's Amount is the
	// sizer's output; when nil, approved signals inherit TradeAmount verbatim
	// to preserve pre-PR-A behaviour.
	Sizer SignalSizer
	// StopLossPercent mirrors riskCfg.StopLossPercent and is needed to
	// compute the JPY-per-unit SL distance inside the sizer.
	StopLossPercent float64
	// Equity / Peak provide the runtime context the sizer consumes. Either
	// may be nil; a nil Equity forces the sizer's fixed-mode fallback.
	Equity EquityProvider
	Peak   *PeakTracker
	// MinConfidence mirrors pipeline.minConfidence so the sizer's confidence
	// scaling matches the live path's cut-off semantics.
	MinConfidence float64
	// BookGate is an optional pre-trade gate that inspects the current
	// orderbook depth before approving a signal. nil disables the gate.
	BookGate *booklimit.Gate
	// BookGateRejects counts how many signals the gate vetoed across
	// the run, broken down by reason. Used for backtest reports.
	BookGateRejects map[string]int
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

	amount := h.TradeAmount
	if h.Sizer != nil {
		equity := 0.0
		if h.Equity != nil {
			equity = h.Equity.Equity()
		}
		var ddPct float64
		if h.Peak != nil && equity > 0 {
			ddPct = h.Peak.Observe(equity)
		}
		sized, skipReason := h.Sizer.Sized(
			h.TradeAmount,
			signalEvent.Price,
			h.StopLossPercent,
			equity,
			signalEvent.CurrentATR,
			ddPct,
			signalEvent.Signal.Confidence,
			h.MinConfidence,
		)
		if skipReason != "" || sized <= 0 {
			return nil, nil
		}
		amount = sized
	}

	side := entity.OrderSideBuy
	if signalEvent.Signal.Action == entity.SignalActionSell {
		side = entity.OrderSideSell
	}
	proposal := entity.OrderProposal{
		SymbolID: signalEvent.Signal.SymbolID,
		Side:     side,
		Amount:   amount,
		Price:    signalEvent.Price,
		IsClose:  false,
	}

	check := h.RiskManager.CheckOrderAt(ctx, time.UnixMilli(signalEvent.Timestamp), proposal)
	if !check.Approved {
		return nil, nil
	}

	// Pre-trade orderbook depth gate. Runs after RiskManager so the gate
	// only sees signals that have already cleared position / daily-loss /
	// cooldown checks. A nil BookGate short-circuits to allow.
	if h.BookGate != nil {
		decision := h.BookGate.Check(ctx, signalEvent.Signal.SymbolID, side, amount, signalEvent.Timestamp)
		if !decision.Allow {
			if h.BookGateRejects == nil {
				h.BookGateRejects = make(map[string]int)
			}
			h.BookGateRejects[decision.Reason]++
			return nil, nil
		}
	}

	return []entity.Event{
		entity.ApprovedSignalEvent{
			Signal:    signalEvent.Signal,
			Price:     signalEvent.Price,
			Timestamp: signalEvent.Timestamp,
			Amount:    amount,
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
	// ThinBookSkips counts SL/TP/trailing exits the simulator could not fill
	// because the orderbook side did not have enough depth (orderbook-replay
	// slippage model only). Surfaced for logging — the position stays open in
	// that case, so the next tick will re-attempt the close.
	ThinBookSkips int
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
// fresh primary-interval ATR value is available. NaN is ignored (the
// indicator calculator emits NaN when there is insufficient data). Zero
// *is* accepted so the handler correctly returns to the percent-only
// fallback path when the market genuinely has zero range — a previous
// version silently retained a stale positive ATR in that case, breaking
// the max(percent, ATR) policy when ATR transitioned back to 0.
func (h *TickRiskHandler) UpdateATR(atr float64) {
	if atr != atr { // NaN check
		return
	}
	if atr < 0 {
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
					var thin *infraThinBookError
					if errors.As(err, &thin) {
						h.ThinBookSkips++
						continue
					}
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
					var thin *infraThinBookError
					if errors.As(err, &thin) {
						h.ThinBookSkips++
						continue
					}
					return nil, err
				}
				emitted = append(emitted, orderEvent)
				delete(h.highWaterMarks, pos.PositionID)
			}
		} else {
			if best < pos.EntryPrice && tickEvent.Price-best >= distance {
				orderEvent, _, err := h.Executor.Close(pos.PositionID, tickEvent.Price, "trailing_stop", tickEvent.Timestamp)
				if err != nil {
					var thin *infraThinBookError
					if errors.As(err, &thin) {
						h.ThinBookSkips++
						continue
					}
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
//
// The executor trusts ApprovedSignalEvent.Amount when set (the risk handler
// has already sized the lot). TradeAmount remains as a legacy fallback for
// older callers that emit ApprovedSignalEvent with Amount == 0.
type ExecutionHandler struct {
	Executor    SignalExecutor
	TradeAmount float64
	// ThinBookSkips counts approved signals that the simulator could not fill
	// because the orderbook side did not have enough depth (only meaningful
	// with the orderbook-replay slippage model). Surfaced for logging.
	ThinBookSkips int
}

func (h *ExecutionHandler) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
	signalEvent, ok := event.(entity.ApprovedSignalEvent)
	if !ok {
		return nil, nil
	}
	if h.Executor == nil {
		return nil, fmt.Errorf("executor is nil")
	}

	amount := signalEvent.Amount
	if amount <= 0 {
		amount = h.TradeAmount
	}
	if amount <= 0 {
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
		amount,
		signalEvent.Signal.Reason,
		signalEvent.Timestamp,
	)
	if err != nil {
		var thin *infraThinBookError
		if errors.As(err, &thin) {
			h.ThinBookSkips++
			return nil, nil
		}
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

// calculateIndicatorSet builds an IndicatorSet from oldest-first candles.
// bbSqueezeLookback is the window (in bars) used to detect a recent BB
// squeeze; 0 disables the detection (RecentSqueeze stays false), matching
// the cycle43 "0 = disabled" convention for the other integer stance
// parameters. Legacy callers can pass 5 to preserve pre-cycle44 behaviour.
func calculateIndicatorSet(symbolID int64, candles []entity.Candle, bbSqueezeLookback int) entity.IndicatorSet {
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

	// PR-6: ADX family. Mirror the live-pipeline calculator.
	adxVal, plusDI, minusDI := indicator.ADX(highs, lows, closes, 14)
	result.ADX14 = floatToPtr(adxVal)
	result.PlusDI14 = floatToPtr(plusDI)
	result.MinusDI14 = floatToPtr(minusDI)

	// PR-7: Stochastics (14, 3, 3) + Stochastic RSI (14, 14). Mirror the
	// live-pipeline calculator.
	stochK, stochD := indicator.Stochastics(highs, lows, closes, 14, 3, 3)
	result.StochK14_3 = floatToPtr(stochK)
	result.StochD14_3 = floatToPtr(stochD)
	result.StochRSI14 = floatToPtr(indicator.StochasticRSI(closes, 14, 14))

	// PR-8: Ichimoku. Mirror the live pipeline; nil when all five lines
	// are still in warmup.
	if snap := buildIchimokuSnapshotBT(indicator.Ichimoku(highs, lows, closes, 9, 26, 52)); snap != nil {
		result.Ichimoku = snap
	}

	// PR-11: Donchian Channel (20-bar default). Mirror the live pipeline;
	// nil until 20 bars of history are available.
	donU, donL, donM := indicator.Donchian(highs, lows, 20)
	result.Donchian20Upper = floatToPtr(donU)
	result.Donchian20Lower = floatToPtr(donL)
	result.Donchian20Middle = floatToPtr(donM)

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

	// PR-9: OBV + CMF (volume-based). Mirror the live-pipeline calculator.
	result.OBV = floatToPtr(indicator.OBV(closes, volumes))
	result.OBVSlope20 = floatToPtr(indicator.OBVSlope(closes, volumes, 20))
	result.CMF20 = floatToPtr(indicator.CMF(highs, lows, closes, volumes, 20))

	// RecentSqueeze: check if any of the last `bbSqueezeLookback` candles
	// had BBBandwidth < 0.02. cycle44: now honours the profile field via
	// the handler's bbSqueezeLookback. 0 keeps RecentSqueeze false (gate
	// disabled). Capped by n-19 so small warmup windows do not read past
	// the start of BB computation.
	if n >= 20 && bbSqueezeLookback > 0 {
		recentSqueeze := false
		lookback := bbSqueezeLookback
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

// buildIchimokuSnapshotBT mirrors usecase.buildIchimokuSnapshot for the
// backtest path. Kept as a sibling helper (rather than exported) so both
// calculators evolve independently without cross-package coupling.
func buildIchimokuSnapshotBT(r indicator.IchimokuResult) *entity.IchimokuSnapshot {
	snap := &entity.IchimokuSnapshot{
		Tenkan:  floatToPtr(r.Tenkan),
		Kijun:   floatToPtr(r.Kijun),
		SenkouA: floatToPtr(r.SenkouA),
		SenkouB: floatToPtr(r.SenkouB),
		Chikou:  floatToPtr(r.Chikou),
	}
	if snap.Tenkan == nil && snap.Kijun == nil && snap.SenkouA == nil && snap.SenkouB == nil && snap.Chikou == nil {
		return nil
	}
	return snap
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
