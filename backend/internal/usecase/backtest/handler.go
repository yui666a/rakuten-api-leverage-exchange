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

	// periods drives the lookback periods for SMA / EMA / RSI / MACD / BB /
	// ATR / VolumeSMA. Filled from a StrategyProfile via SetIndicatorPeriods
	// at composition time; zero-valued fields fall back to legacy defaults
	// via IndicatorConfig.WithDefaults so callers without a profile keep the
	// pre-PR-B behaviour.
	periods entity.IndicatorConfig

	primaryCandles map[int64][]entity.Candle
	higherCandles  map[int64][]entity.Candle

	// bookSource and OFI calculators are optional (PR-J). When wired,
	// every primary-interval Candle event also computes Microprice and
	// short/long-window OFI from the most recent orderbook snapshot at or
	// before the candle close timestamp. nil keeps the legacy behaviour
	// (Microprice / OFI fields stay nil on the IndicatorSet).
	bookSource    indicatorBookSource
	ofiShort      map[int64]*indicator.OFICalculator
	ofiLong       map[int64]*indicator.OFICalculator
	ofiShortMs    int64
	ofiLongMs     int64
	ofiTopN       int
}

// indicatorBookSource is the narrow port the handler needs to look up the
// book at signal time. It mirrors booklimit.BookSource so callers can pass
// the same MarketDataService / OrderbookReplay used elsewhere.
type indicatorBookSource interface {
	LatestBefore(ctx context.Context, symbolID, ts int64) (entity.Orderbook, bool, error)
}

// SetBookSource enables the orderbook-derived signals. windowShortMs and
// windowLongMs default to 10s / 60s when zero. topN defaults to 5.
func (h *IndicatorHandler) SetBookSource(src indicatorBookSource, windowShortMs, windowLongMs int64, topN int) {
	if topN <= 0 {
		topN = 5
	}
	if windowShortMs <= 0 {
		windowShortMs = 10_000
	}
	if windowLongMs <= 0 {
		windowLongMs = 60_000
	}
	h.bookSource = src
	h.ofiShortMs = windowShortMs
	h.ofiLongMs = windowLongMs
	h.ofiTopN = topN
	h.ofiShort = make(map[int64]*indicator.OFICalculator)
	h.ofiLong = make(map[int64]*indicator.OFICalculator)
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
		periods:           entity.IndicatorConfig{}.WithDefaults(),
		primaryCandles:    make(map[int64][]entity.Candle),
		higherCandles:     make(map[int64][]entity.Candle),
	}
}

// SeedPrimary primes the primary-interval candle buffer for symbolID with
// historical bars. Live mode uses this at pipeline start so the very first
// CandleEvent emitted from the WebSocket already has enough history to
// compute SMA / RSI / MACD / BB / ATR — without it, those indicators stay
// nil for ~20-26 bars and every decision logs as HOLD.
//
// candles must be PT-PrimaryInterval bars ordered oldest-first. Bars are
// appended in order and capped at BufferSize. Calling SeedPrimary after
// CandleEvents have already arrived is allowed but unusual; it appends as if
// the seeded bars came in next, which would corrupt indicator paths — so
// callers should seed before subscribing to live ticks.
//
// SeedHigherTF mirrors this for the higher-TF buffer.
func (h *IndicatorHandler) SeedPrimary(symbolID int64, candles []entity.Candle) {
	if len(candles) == 0 {
		return
	}
	for _, c := range candles {
		h.primaryCandles[symbolID] = appendCapped(h.primaryCandles[symbolID], c, h.BufferSize)
	}
}

// SeedHigherTF primes the higher-TF candle buffer for symbolID. See
// SeedPrimary for the contract.
func (h *IndicatorHandler) SeedHigherTF(symbolID int64, candles []entity.Candle) {
	if len(candles) == 0 {
		return
	}
	for _, c := range candles {
		h.higherCandles[symbolID] = appendCapped(h.higherCandles[symbolID], c, h.BufferSize)
	}
}

// SetIndicatorPeriods overrides every indicator lookback used inside
// calculateIndicatorSet — SMA / EMA / RSI / MACD / BB / ATR / VolumeSMA /
// ADX / Stochastics / StochRSI / Donchian / OBVSlope / CMF / Ichimoku.
// Zero-valued fields fall back to the legacy defaults via
// IndicatorConfig.WithDefaults. Mirrors usecase.IndicatorCalculator's
// SetIndicatorPeriods so live and backtest paths share one knob set.
func (h *IndicatorHandler) SetIndicatorPeriods(p entity.IndicatorConfig) {
	h.periods = p.WithDefaults()
}

// attachBookDerived populates Microprice / OFI on the supplied IndicatorSet
// when a BookSource is wired and a usable snapshot exists. No-ops when the
// gate is disabled (legacy path).
func (h *IndicatorHandler) attachBookDerived(set *entity.IndicatorSet, symbolID, ts int64) {
	if h == nil || h.bookSource == nil {
		return
	}
	ob, found, err := h.bookSource.LatestBefore(context.Background(), symbolID, ts)
	if err != nil || !found {
		return
	}
	if mp, ok := indicator.Microprice(ob); ok {
		v := mp
		set.Microprice = &v
	}
	short := h.ofiShort[symbolID]
	if short == nil {
		short = indicator.NewOFICalculator(h.ofiTopN, h.ofiShortMs)
		h.ofiShort[symbolID] = short
	}
	short.Add(ob)
	if v, ok := short.Compute(); ok {
		x := v
		set.OFIShort = &x
	}
	long := h.ofiLong[symbolID]
	if long == nil {
		long = indicator.NewOFICalculator(h.ofiTopN, h.ofiLongMs)
		h.ofiLong[symbolID] = long
	}
	long.Add(ob)
	if v, ok := long.Compute(); ok {
		x := v
		set.OFILong = &x
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
		primary := calculateIndicatorSet(candleEvent.SymbolID, h.primaryCandles[candleEvent.SymbolID], h.periods, h.bbSqueezeLookback)
		h.attachBookDerived(&primary, candleEvent.SymbolID, candleEvent.Timestamp)

		var higherTF *entity.IndicatorSet
		if h.HigherTFInterval != "" {
			if selected := selectCandlesAtOrBefore(h.higherCandles[candleEvent.SymbolID], candleEvent.Timestamp); len(selected) > 0 {
				set := calculateIndicatorSet(candleEvent.SymbolID, selected, h.periods, h.bbSqueezeLookback)
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
	if signal == nil {
		return nil, nil
	}

	var atr float64
	if indicators.ATR != nil {
		atr = *indicators.ATR
	}

	events := make([]entity.Event, 0, 2)

	// Legacy route (PR1 / earlier): SignalEvent fires only on actionable
	// (non-HOLD) signals. PR2 deliberately preserves this asymmetry so the
	// downstream RiskHandler keeps consuming the same events as before — the
	// new route is shadow-only until PR3.
	if signal.Action != entity.SignalActionHold {
		events = append(events, entity.SignalEvent{
			Signal:     *signal,
			Price:      indicatorEvent.LastPrice,
			Timestamp:  indicatorEvent.Timestamp,
			CurrentATR: atr,
		})
	}

	// New route (PR2 of Signal/Decision/ExecutionPolicy three-layer
	// separation): always publish a MarketSignalEvent — including HOLD bars,
	// where Direction=NEUTRAL — so the recorder can populate the Phase 1
	// columns on every bar. The DecisionHandler consumes this event at
	// priority 27 and emits an ActionDecisionEvent in turn.
	events = append(events, entity.MarketSignalEvent{
		Signal:     toMarketSignal(*signal, indicators),
		Price:      indicatorEvent.LastPrice,
		CurrentATR: atr,
		Timestamp:  indicatorEvent.Timestamp,
	})

	return events, nil
}

// toMarketSignal is the thin translation that lets the existing
// BUY/SELL/HOLD-based StrategyEngine feed the new Direction/Strength-based
// route without rewriting its internals. Phase 6+ may rework StrategyEngine
// itself to emit MarketSignal directly; for now the adapter keeps both
// routes correct without touching the strategy logic.
func toMarketSignal(s entity.Signal, indicators entity.IndicatorSet) entity.MarketSignal {
	var dir entity.SignalDirection
	switch s.Action {
	case entity.SignalActionBuy:
		dir = entity.DirectionBullish
	case entity.SignalActionSell:
		dir = entity.DirectionBearish
	default:
		dir = entity.DirectionNeutral
	}
	return entity.MarketSignal{
		SymbolID:   s.SymbolID,
		Direction:  dir,
		Strength:   s.Confidence,
		Source:     "legacy_strategy_engine",
		Reason:     s.Reason,
		Indicators: indicators,
		Timestamp:  s.Timestamp,
	}
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

// Handle dispatches on event type:
//   - ActionDecisionEvent (PR3+): the new execution input. Decision-layer
//     output drives sizing / risk gate / book gate / approved emission.
//   - OrderEvent: observed for close-fill detection so the entry cooldown
//     can be armed via RiskManager.NoteClose. The handler does not emit
//     anything in this branch.
func (h *RiskHandler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
	switch ev := event.(type) {
	case entity.ActionDecisionEvent:
		return h.handleDecision(ctx, ev)
	case entity.OrderEvent:
		h.handleOrderEvent(ev)
		return nil, nil
	}
	return nil, nil
}

func (h *RiskHandler) handleDecision(ctx context.Context, decisionEvent entity.ActionDecisionEvent) ([]entity.Event, error) {
	if h.RiskManager == nil {
		return nil, fmt.Errorf("risk manager is nil")
	}
	if h.TradeAmount <= 0 {
		return nil, fmt.Errorf("trade amount must be positive")
	}

	decision := decisionEvent.Decision

	// HOLD / COOLDOWN_BLOCKED: nothing to do — recorder already captured the
	// decision, and there is no order to attempt.
	// EXIT_CANDIDATE is intentionally skipped in PR3: real exits stay on the
	// TP/SL/Trailing path. A future PR may introduce an `exit_on_signal`
	// profile flag to opt into signal-driven closes.
	if decision.Intent != entity.IntentNewEntry {
		return nil, nil
	}

	// Re-construct a Signal compatible with the existing downstream contract
	// (sizer, RejectedSignalEvent, ApprovedSignalEvent). Confidence comes
	// straight from MarketSignal.Strength (which itself was Confidence in
	// the legacy translator), keeping bit-identical behaviour for sizing.
	side := decision.Side
	if side == "" {
		// NEW_ENTRY without a side is a Decision-layer bug; treat as hold.
		return nil, nil
	}
	synthSignal := entity.Signal{
		SymbolID:   decision.SymbolID,
		Action:     sideToAction(side),
		Confidence: decision.Strength,
		Reason:     decision.Reason,
		Timestamp:  decision.Timestamp,
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
			decisionEvent.Price,
			h.StopLossPercent,
			equity,
			decisionEvent.CurrentATR,
			ddPct,
			synthSignal.Confidence,
			h.MinConfidence,
		)
		if skipReason != "" || sized <= 0 {
			reason := "sizer skipped"
			if skipReason != "" {
				reason = "sizer skipped: " + skipReason
			} else if sized <= 0 {
				reason = "sizer returned zero lot"
			}
			return []entity.Event{entity.RejectedSignalEvent{
				Signal:    synthSignal,
				Stage:     entity.RejectedStageRisk,
				Reason:    reason,
				Price:     decisionEvent.Price,
				Timestamp: decisionEvent.Timestamp,
			}}, nil
		}
		amount = sized
	}

	proposal := entity.OrderProposal{
		SymbolID: decision.SymbolID,
		Side:     side,
		Amount:   amount,
		Price:    decisionEvent.Price,
		IsClose:  false,
	}

	check := h.RiskManager.CheckOrderAt(ctx, time.UnixMilli(decisionEvent.Timestamp), proposal)
	if !check.Approved {
		return []entity.Event{entity.RejectedSignalEvent{
			Signal:    synthSignal,
			Stage:     entity.RejectedStageRisk,
			Reason:    check.Reason,
			Price:     decisionEvent.Price,
			Timestamp: decisionEvent.Timestamp,
		}}, nil
	}

	if h.BookGate != nil {
		gateDecision := h.BookGate.Check(ctx, decision.SymbolID, side, amount, decisionEvent.Timestamp)
		if !gateDecision.Allow {
			if h.BookGateRejects == nil {
				h.BookGateRejects = make(map[string]int)
			}
			h.BookGateRejects[gateDecision.Reason]++
			return []entity.Event{entity.RejectedSignalEvent{
				Signal:    synthSignal,
				Stage:     entity.RejectedStageBookGate,
				Reason:    gateDecision.Reason,
				Price:     decisionEvent.Price,
				Timestamp: decisionEvent.Timestamp,
			}}, nil
		}
	}

	return []entity.Event{
		entity.ApprovedSignalEvent{
			Signal:    synthSignal,
			Price:     decisionEvent.Price,
			Timestamp: decisionEvent.Timestamp,
			Amount:    amount,
			Urgency:   synthSignal.Urgency,
		},
	}, nil
}

// handleOrderEvent arms the entry cooldown when an OrderEvent represents a
// realised close (ClosedPositionID > 0 with a non-zero OrderID). All other
// order events — opens, failures (OrderID == 0), tick-driven trailing stops
// already encoded by the executor — pass through untouched.
func (h *RiskHandler) handleOrderEvent(ev entity.OrderEvent) {
	if h.RiskManager == nil {
		return
	}
	if ev.ClosedPositionID > 0 && ev.OrderID > 0 {
		h.RiskManager.NoteClose(time.UnixMilli(ev.Timestamp))
	}
}

func sideToAction(side entity.OrderSide) entity.SignalAction {
	if side == entity.OrderSideSell {
		return entity.SignalActionSell
	}
	return entity.SignalActionBuy
}

// TickRiskExecutor exposes minimum close-related operations for tick-driven risk checks.
type TickRiskExecutor interface {
	Positions() []eventengine.Position
	SelectSLTPExit(side entity.OrderSide, stopLossPrice, takeProfitPrice, barLow, barHigh float64) (float64, string, bool)
	Close(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error)
}

// TickRiskHandler evaluates SL/TP/TrailingStop on synthetic ticks.
//
// Distance policy (driven by risk.RiskPolicy):
//   - SL: max(entry × policy.StopLoss.Percent / 100, currentATR × policy.StopLoss.ATRMultiplier).
//     The ATR branch only engages when the multiplier is > 0 and ATR is known.
//   - TP: entry × policy.TakeProfit.Percent / 100.
//   - Trailing: depends on policy.Trailing.Mode —
//     Disabled → trailing path is skipped (no high-water-mark tracking, no
//     trailing-driven exit);
//     Percent → entry × policy.StopLoss.Percent / 100, identical to the legacy
//     percent-only behaviour;
//     ATR     → max(percent fallback, currentATR × Trailing.ATRMultiplier)
//     so a stale / zero ATR cannot collapse the trailing distance to 0.
//
// The handler holds the policy by value (not pointer) so callers cannot
// reach in and mutate distance knobs after construction — the only way a
// distance changes is through ATR updates, which are scoped to the ATR
// branch alone.
type TickRiskHandler struct {
	PrimaryInterval string
	Executor        TickRiskExecutor
	policy          riskPolicy

	highWaterMarks map[int64]float64
	currentATR     float64
	// ThinBookSkips counts SL/TP/trailing exits the simulator could not fill
	// because the orderbook side did not have enough depth (orderbook-replay
	// slippage model only). Surfaced for logging — the position stays open in
	// that case, so the next tick will re-attempt the close.
	ThinBookSkips int
}

// riskPolicy is a flattened view of risk.RiskPolicy private to this
// package. It exists so usecase/backtest does not pull domain/risk into
// its public API surface (handler tests already construct the handler
// with raw float64s and we want that path to keep working).
type riskPolicy struct {
	stopLossPercent       float64
	stopLossATRMultiplier float64
	takeProfitPercent     float64
	trailingMode          int // 0=Disabled, 1=Percent, 2=ATR — must match risk.TrailingMode*
	trailingATRMultiplier float64
}

const (
	trailingModeDisabled = 0
	trailingModePercent  = 1
	trailingModeATR      = 2
)

// Public mirrors of the package-private trailing mode constants. cmd/
// callers reference these when populating PolicyView so the int values
// in PolicyView are not magic numbers.
const (
	TrailingModeDisabled = trailingModeDisabled
	TrailingModePercent  = trailingModePercent
	TrailingModeATR      = trailingModeATR
)

// NewTickRiskHandler is the legacy constructor: callers pass percent SL
// / TP only and the handler defaults to TrailingModePercent (the
// pre-RiskPolicy behaviour). New callers should prefer
// NewTickRiskHandlerWithPolicy.
func NewTickRiskHandler(primaryInterval string, executor TickRiskExecutor, stopLossPercent, takeProfitPercent float64) *TickRiskHandler {
	return &TickRiskHandler{
		PrimaryInterval: primaryInterval,
		Executor:        executor,
		policy: riskPolicy{
			stopLossPercent:   stopLossPercent,
			takeProfitPercent: takeProfitPercent,
			trailingMode:      trailingModePercent,
		},
		highWaterMarks: make(map[int64]float64),
	}
}

// PolicyView is the package-public projection of risk.RiskPolicy used by
// callers that already imported domain/risk to build the handler. The
// fields mirror risk.RiskPolicy.* but are flat float64 / int so this
// package can stay free of the domain/risk import (avoiding a cycle if
// risk ever needs to import backtest types for any reason). Wire from
// risk.RiskPolicy via PolicyFromRiskPolicy in cmd/.
type PolicyView struct {
	StopLossPercent       float64
	StopLossATRMultiplier float64
	TakeProfitPercent     float64
	TrailingMode          int // 0=Disabled, 1=Percent, 2=ATR
	TrailingATRMultiplier float64
}

// NewTickRiskHandlerWithPolicy is the policy-driven constructor. It is
// the only entry point that lets the trailing path be disabled or run
// in ATR mode. SetATRMultipliers and direct field mutation are not
// supported on the result — the policy is locked in at construction.
func NewTickRiskHandlerWithPolicy(primaryInterval string, executor TickRiskExecutor, view PolicyView) *TickRiskHandler {
	return &TickRiskHandler{
		PrimaryInterval: primaryInterval,
		Executor:        executor,
		policy: riskPolicy{
			stopLossPercent:       view.StopLossPercent,
			stopLossATRMultiplier: view.StopLossATRMultiplier,
			takeProfitPercent:     view.TakeProfitPercent,
			trailingMode:          view.TrailingMode,
			trailingATRMultiplier: view.TrailingATRMultiplier,
		},
		highWaterMarks: make(map[int64]float64),
	}
}

// StopLossPercent returns the configured SL percent (read-only). Tests
// that asserted on the previous public field still need to inspect the
// value; callers that previously mutated the field must move to the
// PolicyView constructor.
func (h *TickRiskHandler) StopLossPercent() float64 { return h.policy.stopLossPercent }

// TakeProfitPercent mirrors StopLossPercent for symmetric inspection.
func (h *TickRiskHandler) TakeProfitPercent() float64 { return h.policy.takeProfitPercent }

// SetATRMultipliers is retained for the legacy NewTickRiskHandler path.
// It mutates the SL ATR multiplier and (when called with a positive
// trailing multiplier) flips the trailing mode to ATR. New code should
// not call this — pass a PolicyView at construction instead. The setter
// stays in place because backtest runner-level tests rely on it; the
// live pipeline no longer calls it after this PR.
func (h *TickRiskHandler) SetATRMultipliers(stopLossATR, trailingATR float64) {
	h.policy.stopLossATRMultiplier = stopLossATR
	h.policy.trailingATRMultiplier = trailingATR
	if trailingATR > 0 {
		h.policy.trailingMode = trailingModeATR
	}
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
	percentDist := entryPrice * h.policy.stopLossPercent / 100.0
	atrDist := 0.0
	if h.policy.stopLossATRMultiplier > 0 && h.currentATR > 0 {
		atrDist = h.currentATR * h.policy.stopLossATRMultiplier
	}
	if atrDist > percentDist {
		return atrDist
	}
	return percentDist
}

// trailingDistance returns the trailing reversal distance for the
// configured TrailingMode. Disabled returns 0 so the caller skips the
// trailing path entirely; Percent uses the StopLoss percent verbatim;
// ATR returns max(percent fallback, currentATR × multiplier) so a stale
// or zero ATR cannot collapse the trailing distance to 0.
func (h *TickRiskHandler) trailingDistance(entryPrice float64) float64 {
	switch h.policy.trailingMode {
	case trailingModeDisabled:
		return 0
	case trailingModeATR:
		percentDist := entryPrice * h.policy.stopLossPercent / 100.0
		atrDist := 0.0
		if h.policy.trailingATRMultiplier > 0 && h.currentATR > 0 {
			atrDist = h.currentATR * h.policy.trailingATRMultiplier
		}
		if atrDist > percentDist {
			return atrDist
		}
		return percentDist
	default: // trailingModePercent
		return entryPrice * h.policy.stopLossPercent / 100.0
	}
}

func (h *TickRiskHandler) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
	// ATR は IndicatorEvent から抽出。IndicatorHandler が CandleEvent を
	// 受けて IndicatorEvent を emit するので、その直後に ATR が最新化される。
	// Trailing stop / ATR SL はこの最新値を使う。
	if indicatorEvent, ok := event.(entity.IndicatorEvent); ok {
		if indicatorEvent.Primary.ATR != nil {
			h.UpdateATR(*indicatorEvent.Primary.ATR)
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
		if h.policy.stopLossPercent > 0 && h.policy.takeProfitPercent > 0 {
			slDistance := h.stopLossDistance(pos.EntryPrice)
			tpDistance := pos.EntryPrice * h.policy.takeProfitPercent / 100.0
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
				orderEvent.Trigger = entity.DecisionTriggerTickSLTP
				orderEvent.ClosedPositionID = pos.PositionID
				emitted = append(emitted, orderEvent)
				delete(h.highWaterMarks, pos.PositionID)
				continue
			}
		}

		// Trailing stop: skip the entire branch when policy disables it
		// so the high-water-mark map stays empty for Disabled trailing —
		// the prior implementation wrote to the map even when trailing
		// was off, which leaked memory in long-running live processes.
		if h.policy.trailingMode == trailingModeDisabled {
			continue
		}
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
				orderEvent.Trigger = entity.DecisionTriggerTickTrailing
				orderEvent.ClosedPositionID = pos.PositionID
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
				orderEvent.Trigger = entity.DecisionTriggerTickTrailing
				orderEvent.ClosedPositionID = pos.PositionID
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

// UrgencyAwareExecutor is an optional add-on interface. When the underlying
// executor implements it, ExecutionHandler routes through OpenWithUrgency
// so the executor can pick a SOR strategy from the urgency hint. Executors
// that don't implement this stay on the legacy Open path verbatim.
type UrgencyAwareExecutor interface {
	OpenWithUrgency(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64, urgency entity.SignalUrgency) (entity.OrderEvent, error)
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

	var orderEvent entity.OrderEvent
	var err error
	if uae, ok := h.Executor.(UrgencyAwareExecutor); ok && signalEvent.Urgency != "" {
		orderEvent, err = uae.OpenWithUrgency(
			signalEvent.Signal.SymbolID,
			side,
			signalEvent.Price,
			amount,
			signalEvent.Signal.Reason,
			signalEvent.Timestamp,
			signalEvent.Urgency,
		)
	} else {
		orderEvent, err = h.Executor.Open(
			signalEvent.Signal.SymbolID,
			side,
			signalEvent.Price,
			amount,
			signalEvent.Signal.Reason,
			signalEvent.Timestamp,
		)
	}
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
// periods drives SMA / EMA / RSI / MACD / BB / ATR / VolumeSMA lookbacks; a
// zero-valued IndicatorConfig falls back to legacy defaults via WithDefaults
// so legacy call sites without a profile keep their pre-PR-B behaviour.
//
// bbSqueezeLookback is the window (in bars) used to detect a recent BB
// squeeze; 0 disables the detection (RecentSqueeze stays false), matching
// the cycle43 "0 = disabled" convention for the other integer stance
// parameters. Legacy callers can pass 5 to preserve pre-cycle44 behaviour.
//
// PR-C will profile-drive ADX / Stochastics / Donchian / CMF / OBVSlope /
// Ichimoku; until then those use their hardcoded periods below.
func calculateIndicatorSet(symbolID int64, candles []entity.Candle, periods entity.IndicatorConfig, bbSqueezeLookback int) entity.IndicatorSet {
	n := len(candles)
	if n == 0 {
		return entity.IndicatorSet{SymbolID: symbolID}
	}

	periods = periods.WithDefaults()

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
		SMAShort:  floatToPtr(indicator.SMA(closes, periods.SMAShort)),
		SMALong:   floatToPtr(indicator.SMA(closes, periods.SMALong)),
		EMAFast:   floatToPtr(indicator.EMA(closes, periods.EMAFast)),
		EMASlow:   floatToPtr(indicator.EMA(closes, periods.EMASlow)),
		RSI:       floatToPtr(indicator.RSI(closes, periods.RSIPeriod)),
		Timestamp: candles[n-1].Time,
	}

	macdLine, signalLine, histogram := indicator.MACD(closes, periods.MACDFast, periods.MACDSlow, periods.MACDSignal)
	result.MACDLine = floatToPtr(macdLine)
	result.SignalLine = floatToPtr(signalLine)
	result.Histogram = floatToPtr(histogram)

	bbUpper, bbMiddle, bbLower, bbBandwidth := indicator.BollingerBands(closes, periods.BBPeriod, periods.BBMultiplier)
	result.BBUpper = floatToPtr(bbUpper)
	result.BBMiddle = floatToPtr(bbMiddle)
	result.BBLower = floatToPtr(bbLower)
	result.BBBandwidth = floatToPtr(bbBandwidth)

	result.ATR = floatToPtr(indicator.ATR(highs, lows, closes, periods.ATRPeriod))

	// PR-6: ADX family. Mirror the live-pipeline calculator.
	adxVal, plusDI, minusDI := indicator.ADX(highs, lows, closes, periods.ADXPeriod)
	result.ADX = floatToPtr(adxVal)
	result.PlusDI = floatToPtr(plusDI)
	result.MinusDI = floatToPtr(minusDI)

	// PR-7: Stochastics + Stochastic RSI. Mirror the live-pipeline calculator.
	stochK, stochD := indicator.Stochastics(highs, lows, closes, periods.StochKPeriod, periods.StochSmoothK, periods.StochSmoothD)
	result.StochK = floatToPtr(stochK)
	result.StochD = floatToPtr(stochD)
	result.StochRSI = floatToPtr(indicator.StochasticRSI(closes, periods.StochRSIRSIPeriod, periods.StochRSIStochPeriod))

	// PR-8: Ichimoku. Mirror the live pipeline; nil when all five lines
	// are still in warmup.
	if snap := buildIchimokuSnapshotBT(indicator.Ichimoku(highs, lows, closes, periods.IchimokuTenkan, periods.IchimokuKijun, periods.IchimokuSenkouB)); snap != nil {
		result.Ichimoku = snap
	}

	// PR-11: Donchian Channel. Mirror the live pipeline; nil until
	// DonchianPeriod bars of history are available.
	donU, donL, donM := indicator.Donchian(highs, lows, periods.DonchianPeriod)
	result.DonchianUpper = floatToPtr(donU)
	result.DonchianLower = floatToPtr(donL)
	result.DonchianMiddle = floatToPtr(donM)

	// Volume indicators. VolumeSMA shares the BB period by default (legacy
	// behaviour was VolumeSMA20 alongside BB20).
	volumes := make([]float64, n)
	for i, c := range candles {
		volumes[i] = c.Volume
	}
	volSMA := indicator.VolumeSMA(volumes, periods.VolumeSMAPeriod)
	result.VolumeSMA = floatToPtr(volSMA)
	if !math.IsNaN(volSMA) && volSMA > 0 && n > 0 {
		vr := indicator.VolumeRatio(volumes[n-1], volSMA)
		result.VolumeRatio = floatToPtr(vr)
	}

	// PR-9: OBV + CMF (volume-based). Mirror the live-pipeline calculator.
	result.OBV = floatToPtr(indicator.OBV(closes, volumes))
	result.OBVSlope = floatToPtr(indicator.OBVSlope(closes, volumes, periods.OBVSlopePeriod))
	result.CMF = floatToPtr(indicator.CMF(highs, lows, closes, volumes, periods.CMFPeriod))

	// RecentSqueeze: check if any of the last `bbSqueezeLookback` candles
	// had BBBandwidth < threshold. cycle44: now honours the profile field
	// via the handler's bbSqueezeLookback. 0 keeps RecentSqueeze false
	// (gate disabled). Capped by n-(bbPeriod-1) so small warmup windows
	// do not read past the start of BB computation.
	if n >= periods.BBPeriod && bbSqueezeLookback > 0 {
		recentSqueeze := false
		lookback := bbSqueezeLookback
		if lookback > n-(periods.BBPeriod-1) {
			lookback = n - (periods.BBPeriod - 1)
		}
		for i := 0; i < lookback; i++ {
			offset := n - 1 - i
			windowCloses := closes[:offset+1]
			_, _, _, bw := indicator.BollingerBands(windowCloses, periods.BBPeriod, periods.BBMultiplier)
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
