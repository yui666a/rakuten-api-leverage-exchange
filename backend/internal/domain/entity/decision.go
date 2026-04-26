package entity

// DecisionRecord captures a single pipeline decision (BUY/SELL/HOLD plus its
// reasons and the indicator snapshot that produced it). One bar emits at
// least one row (BAR_CLOSE) and may emit additional rows for tick-driven
// SL/TP/Trailing closes (sequence_in_bar > 0).
type DecisionRecord struct {
	ID              int64
	BarCloseAt      int64 // unix ms
	SequenceInBar   int   // 0 for BAR_CLOSE, then 1, 2, ... for in-bar tick events
	TriggerKind     string

	SymbolID        int64
	CurrencyPair    string
	PrimaryInterval string

	Stance    string
	LastPrice float64

	SignalAction     string // "BUY" | "SELL" | "HOLD"
	SignalConfidence float64
	SignalReason     string

	RiskOutcome string
	RiskReason  string

	BookGateOutcome string
	BookGateReason  string

	OrderOutcome   string
	OrderID        int64
	ExecutedAmount float64
	ExecutedPrice  float64
	OrderError     string

	ClosedPositionID int64
	OpenedPositionID int64

	IndicatorsJSON         string // already-marshalled IndicatorSet
	HigherTFIndicatorsJSON string

	CreatedAt int64
}

// Trigger kinds.
const (
	DecisionTriggerBarClose     = "BAR_CLOSE"
	DecisionTriggerTickSLTP     = "TICK_SLTP"
	DecisionTriggerTickTrailing = "TICK_TRAILING"
)

// Risk gate outcomes.
const (
	DecisionRiskApproved = "APPROVED"
	DecisionRiskRejected = "REJECTED"
	DecisionRiskSkipped  = "SKIPPED"
)

// BookGate outcomes.
const (
	DecisionBookAllowed = "ALLOWED"
	DecisionBookVetoed  = "VETOED"
	DecisionBookSkipped = "SKIPPED"
)

// Order execution outcomes.
const (
	DecisionOrderFilled = "FILLED"
	DecisionOrderFailed = "FAILED"
	DecisionOrderNoop   = "NOOP"
)

// Rejected signal stages (used by RejectedSignalEvent.Stage).
const (
	RejectedStageRisk     = "risk"
	RejectedStageBookGate = "book_gate"
)
