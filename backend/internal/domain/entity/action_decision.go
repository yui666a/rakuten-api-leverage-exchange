package entity

// DecisionIntent は Decision レイヤが下した行動意図。Side と組み合わせて
// ExecutionPolicy (Risk / BookGate / Executor) が解釈する。
type DecisionIntent string

const (
	IntentNewEntry        DecisionIntent = "NEW_ENTRY"
	IntentExitCandidate   DecisionIntent = "EXIT_CANDIDATE"
	IntentHold            DecisionIntent = "HOLD"
	IntentCooldownBlocked DecisionIntent = "COOLDOWN_BLOCKED"
)

// ActionDecision は Decision レイヤの判定結果。
// Intent=HOLD / COOLDOWN_BLOCKED の時 Side は空文字。
// Strength と Source は由来 MarketSignal から継承し、サイジング / ログに使う。
type ActionDecision struct {
	SymbolID  int64          `json:"symbolId"`
	Intent    DecisionIntent `json:"intent"`
	Side      OrderSide      `json:"side,omitempty"`
	Reason    string         `json:"reason"`
	Source    string         `json:"source"`
	Strength  float64        `json:"strength"`
	Timestamp int64          `json:"timestamp"`
}

// IsActionable は Intent が実発注を伴う種類か (NEW_ENTRY / EXIT_CANDIDATE)。
// HOLD / COOLDOWN_BLOCKED は false。後段ハンドラの分岐用。
func (d ActionDecision) IsActionable() bool {
	return d.Intent == IntentNewEntry || d.Intent == IntentExitCandidate
}

// ActionDecisionEvent は EventBus に流れるイベント。RiskHandler が購読する。
type ActionDecisionEvent struct {
	Decision   ActionDecision
	Price      float64
	CurrentATR float64
	Timestamp  int64
}

func (e ActionDecisionEvent) EventType() string     { return EventTypeDecision }
func (e ActionDecisionEvent) EventTimestamp() int64 { return e.Timestamp }
