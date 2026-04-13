package entity

// SignalAction は売買シグナルのアクション種別。
type SignalAction string

const (
	SignalActionBuy  SignalAction = "BUY"
	SignalActionSell SignalAction = "SELL"
	SignalActionHold SignalAction = "HOLD"
)

// Signal はStrategy Engineが生成する売買シグナル。
type Signal struct {
	SymbolID   int64        `json:"symbolId"`
	Action     SignalAction `json:"action"`
	Confidence float64      `json:"confidence"` // 0.0–1.0: indicator agreement score
	Reason     string       `json:"reason"`
	Timestamp  int64        `json:"timestamp"`
}
