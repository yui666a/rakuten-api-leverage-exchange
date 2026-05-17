package entity

const (
	EventTypeCandle            = "candle"
	EventTypeIndicator         = "indicator"
	EventTypeTick              = "tick"
	EventTypeSignal            = "signal"
	EventTypeMarketSignal      = "market_signal"
	EventTypeDecision          = "decision"
	EventTypeApproved          = "approved_signal"
	EventTypeRejected          = "rejected_signal"
	EventTypeOrder             = "order"
	EventTypePositionConfirmed = "position_confirmed"
)

// Event is a minimal contract used by the backtest event bus.
type Event interface {
	EventType() string
	EventTimestamp() int64
}

// CandleEvent treats Candle.Time as close timestamp.
type CandleEvent struct {
	SymbolID  int64
	Interval  string
	Candle    Candle
	Timestamp int64
}

func (e CandleEvent) EventType() string     { return EventTypeCandle }
func (e CandleEvent) EventTimestamp() int64 { return e.Timestamp }

// IndicatorEvent binds Strategy input contract in a single payload.
type IndicatorEvent struct {
	SymbolID  int64
	Interval  string
	Primary   IndicatorSet
	HigherTF  *IndicatorSet
	LastPrice float64
	Timestamp int64
}

func (e IndicatorEvent) EventType() string     { return EventTypeIndicator }
func (e IndicatorEvent) EventTimestamp() int64 { return e.Timestamp }

// TickEvent represents in-bar ticks for SL/TP evaluation. Both backtest
// (synthetic) and live (real WS) sources emit this event.
//
// BarHigh/BarLow contract (consumed by backtest.TickRiskHandler via
// SelectSLTPExit: TP triggers when BarHigh >= takeProfitPrice for longs,
// SL when BarLow <= stopLossPrice — and mirrored for shorts):
//
//   - **Backtest source** (backtest.handler emits synthetic ticks per bar):
//     BarHigh/BarLow carry the **confirmed full-bar OHLC** of the candle
//     this tick belongs to. The tick effectively replays a closed bar so
//     SL/TP can be evaluated against the worst-case intra-bar extremes.
//
//   - **Live source** (infrastructure/live.LiveSource.HandleTick):
//     BarHigh/BarLow carry the **in-progress current-bar OHLC** snapshot
//     of CandleBuilder.currentCandle *after this tick is applied*. The
//     range therefore grows monotonically within a bar and resets to
//     High=Low=Last on the first tick of a new bar.
//
// The two semantics are asymmetric (backtest = confirmed final extremes /
// live = monotonic in-progress extremes) but both satisfy the worst-case
// SL/TP contract: any price the position has actually been exposed to
// while the bar was active is included in the range, so the handler can
// fire when it must. The live form is *strictly* safer than the backtest
// form because the range is always a subset of the eventual confirmed
// bar (no future ticks are pre-revealed).
//
// Live MUST NOT use 24h ticker.High/Low here — that caused Issue #266
// (2026-05-12 and 2026-05-17 instant-TP incidents).
type TickEvent struct {
	SymbolID   int64
	Interval   string
	Price      float64
	Timestamp  int64
	TickType   string
	ParentTime int64
	BarLow     float64
	BarHigh    float64
}

func (e TickEvent) EventType() string     { return EventTypeTick }
func (e TickEvent) EventTimestamp() int64 { return e.Timestamp }

type SignalEvent struct {
	Signal    Signal
	Price     float64
	Timestamp int64
	// CurrentATR carries the latest ATR value (price units) forward so
	// downstream sizing can scale by realised volatility without re-reading
	// the indicator stream. 0 means "unknown / warmup" and triggers the
	// sizer's ATR fallback.
	CurrentATR float64
}

func (e SignalEvent) EventType() string     { return EventTypeSignal }
func (e SignalEvent) EventTimestamp() int64 { return e.Timestamp }

type ApprovedSignalEvent struct {
	Signal    Signal
	Price     float64
	Timestamp int64
	// Amount is the sized lot produced by the risk handler. Downstream
	// executors use this value verbatim so that backtest and live code
	// share one sizing decision. 0 means "no-trade" (rejected by sizer).
	Amount float64
	// Urgency is mirrored from Signal.Urgency by the risk handler so the
	// executor can route on a single field without reaching back into the
	// raw signal. Empty value preserves legacy behaviour.
	Urgency SignalUrgency
}

func (e ApprovedSignalEvent) EventType() string     { return EventTypeApproved }
func (e ApprovedSignalEvent) EventTimestamp() int64 { return e.Timestamp }

type OrderEvent struct {
	OrderID   int64
	SymbolID  int64
	Side      string
	Action    string
	Price     float64
	Amount    float64
	Reason    string
	Timestamp int64
	// Trigger identifies what produced this order. Zero value means
	// "legacy / unknown" so existing call-sites that haven't been updated
	// still compile and dispatch normally. Recorder uses this to decide
	// whether the row belongs to the bar's BAR_CLOSE record or is a
	// separate tick-driven row.
	Trigger string
	// OpenedPositionID is set when this order opened a new position.
	OpenedPositionID int64
	// ClosedPositionID is set when this order closed an existing position
	// (set on both stand-alone closes and the close-leg of a reversal).
	ClosedPositionID int64
}

func (e OrderEvent) EventType() string     { return EventTypeOrder }
func (e OrderEvent) EventTimestamp() int64 { return e.Timestamp }

// PositionConfirmedEvent is emitted when the executor observes that the
// venue has confirmed a fill — i.e. a new Position has appeared in
// Positions() with EntryPrice > 0. This is the authoritative trigger for
// downstream handlers (Risk, Exit, ExitPlan shadow) that need the real
// fill price, not the submit-time signalPrice.
//
// Per docs/design/2026-05-12-position-confirmed-only.md, the OrderEvent
// path remains for logging / dashboard purposes, but anything that
// depends on EntryPrice for correctness (TP / SL anchoring) must consume
// this event instead.
//
// One submitted order may produce multiple PositionConfirmedEvent payloads
// when the venue splits the fill across several PositionIDs.
type PositionConfirmedEvent struct {
	PositionID     int64
	OrderID        int64
	SymbolID       int64
	Side           OrderSide
	EntryPrice     float64
	Amount         float64
	EntryTimestamp int64
	Timestamp      int64
}

func (e PositionConfirmedEvent) EventType() string     { return EventTypePositionConfirmed }
func (e PositionConfirmedEvent) EventTimestamp() int64 { return e.Timestamp }
