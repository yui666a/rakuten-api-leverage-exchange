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

	// Phase 1 (Signal/Decision/ExecutionPolicy 三層分離) で追加。
	// PR1 時点では recorder が値を埋めず、全行で空文字 / 0 のまま。
	// PR2 で recorder が新ロジックの結果を書き始める。
	SignalDirection   string  // SignalDirection の string 形 ("BULLISH" 等)
	SignalStrength    float64 // 由来 MarketSignal.Strength
	DecisionIntent    string  // DecisionIntent の string 形
	DecisionSide      string  // OrderSide の string 形 ("BUY" | "SELL" | "")
	DecisionReason    string
	ExitPolicyOutcome string // PR4 で BookGate 経由の出口判断記録に使う

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
	// DecisionTriggerDecisionExit identifies an exit produced by the
	// Decision layer (IntentExitCandidate consumed by RiskHandler when the
	// strategy profile opted in via ExitOnSignal). Distinct from the tick
	// SL/TP/Trailing exits so the recorder and analytics can attribute
	// signal-driven closes to the strategy rather than the risk envelope.
	DecisionTriggerDecisionExit = "DECISION_EXIT"
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
