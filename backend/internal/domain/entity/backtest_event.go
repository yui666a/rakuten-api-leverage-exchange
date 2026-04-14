package entity

const (
	EventTypeCandle    = "candle"
	EventTypeIndicator = "indicator"
	EventTypeTick      = "tick"
	EventTypeSignal    = "signal"
	EventTypeApproved  = "approved_signal"
	EventTypeOrder     = "order"
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

// TickEvent represents synthetic in-bar ticks for SL/TP simulation.
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
}

func (e SignalEvent) EventType() string     { return EventTypeSignal }
func (e SignalEvent) EventTimestamp() int64 { return e.Timestamp }

type ApprovedSignalEvent struct {
	Signal    Signal
	Price     float64
	Timestamp int64
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
}

func (e OrderEvent) EventType() string     { return EventTypeOrder }
func (e OrderEvent) EventTimestamp() int64 { return e.Timestamp }
