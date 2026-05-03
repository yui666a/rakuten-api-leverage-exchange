package entity

// SignalAction は売買シグナルのアクション種別。
type SignalAction string

const (
	SignalActionBuy  SignalAction = "BUY"
	SignalActionSell SignalAction = "SELL"
	SignalActionHold SignalAction = "HOLD"
)

// SignalUrgency conveys the strategy's preference for *how* the order
// should be executed once it has been approved. The Execution layer reads
// this to pick a concrete SOR strategy:
//
//   - SignalUrgencyUrgent  : "lift the offer now" — favour MARKET, even at cost.
//   - SignalUrgencyNormal  : no preference; honour the configured default.
//   - SignalUrgencyPassive : "wait for the price" — favour post-only LIMIT.
//
// An empty value means "unspecified" and is treated identically to Normal,
// preserving bit-identical behaviour for existing strategies that do not
// populate this field.
type SignalUrgency string

const (
	SignalUrgencyUnspecified SignalUrgency = ""
	SignalUrgencyUrgent      SignalUrgency = "urgent"
	SignalUrgencyNormal      SignalUrgency = "normal"
	SignalUrgencyPassive     SignalUrgency = "passive"
)

// Signal はStrategy Engineが生成する売買シグナル。
//
// Deprecated (Phase 1, 2026-05-02): Signal は旧ルートの 1st-class entity
// だったが、Phase 1 で MarketSignal (Direction + Strength) と ActionDecision
// (Intent + Side) の二層に分割された。Signal は現在 RiskHandler 内で
// synthSignal として組み立てられ ApprovedSignalEvent / RejectedSignalEvent
// の payload として残っているだけ。Phase 6+ で完全に置換予定。
// 詳細: docs/design/2026-04-29-signal-decision-policy-separation-design.md
type Signal struct {
	SymbolID   int64         `json:"symbolId"`
	Action     SignalAction  `json:"action"`
	Confidence float64       `json:"confidence"` // 0.0–1.0: indicator agreement score
	Reason     string        `json:"reason"`
	// Urgency is optional. Strategies that don't fill it leave it as
	// SignalUrgencyUnspecified, which the Execution layer treats as
	// "honour the configured SOR default" — i.e. fully backwards compatible.
	Urgency   SignalUrgency `json:"urgency,omitempty"`
	Timestamp int64         `json:"timestamp"`
}
