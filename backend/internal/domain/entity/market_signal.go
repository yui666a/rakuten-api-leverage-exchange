package entity

// SignalDirection は市況の方向性を表す。Signal レイヤは BUY/SELL のような
// 注文サイドではなく、市場解釈としての BULLISH/BEARISH/NEUTRAL を返す。
// 注文サイドへの変換は Decision レイヤの責務。
type SignalDirection string

const (
	DirectionBullish SignalDirection = "BULLISH"
	DirectionBearish SignalDirection = "BEARISH"
	DirectionNeutral SignalDirection = "NEUTRAL"
)

// MarketSignal は Strategy が指標から導いた状況解釈。Direction は方向、
// Strength は確信度 (0.0–1.0)。Source は由来戦略 (例: "contrarian:rsi")、
// Reason は人間可読な根拠で recorder が decision_log.signal_reason に書き戻す。
type MarketSignal struct {
	SymbolID   int64           `json:"symbolId"`
	Direction  SignalDirection `json:"direction"`
	Strength   float64         `json:"strength"`
	Source     string          `json:"source"`
	Reason     string          `json:"reason"`
	Indicators IndicatorSet    `json:"indicators"`
	Timestamp  int64           `json:"timestamp"`
}

// MarketSignalEvent は EventBus に流れるイベント。Decision レイヤが購読する。
// CurrentATR は SignalEvent と同じく warmup 中は 0。
type MarketSignalEvent struct {
	Signal     MarketSignal
	Price      float64
	CurrentATR float64
	Timestamp  int64
}

func (e MarketSignalEvent) EventType() string     { return EventTypeMarketSignal }
func (e MarketSignalEvent) EventTimestamp() int64 { return e.Timestamp }
