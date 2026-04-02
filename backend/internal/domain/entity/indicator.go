package entity

// IndicatorSet はある時点の全テクニカル指標をまとめた構造体。
type IndicatorSet struct {
	SymbolID   int64   `json:"symbolId"`
	SMA20      float64 `json:"sma20"`
	SMA50      float64 `json:"sma50"`
	EMA12      float64 `json:"ema12"`
	EMA26      float64 `json:"ema26"`
	RSI14      float64 `json:"rsi14"`
	MACDLine   float64 `json:"macdLine"`
	SignalLine float64 `json:"signalLine"`
	Histogram  float64 `json:"histogram"`
	Timestamp  int64   `json:"timestamp"`
}
