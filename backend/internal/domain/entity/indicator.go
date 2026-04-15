package entity

// IndicatorSet はある時点の全テクニカル指標をまとめた構造体。
// データ不足で計算できない指標はnilになる。
type IndicatorSet struct {
	SymbolID       int64    `json:"symbolId"`
	SMA20          *float64 `json:"sma20"`
	SMA50          *float64 `json:"sma50"`
	EMA12          *float64 `json:"ema12"`
	EMA26          *float64 `json:"ema26"`
	RSI14          *float64 `json:"rsi14"`
	MACDLine       *float64 `json:"macdLine"`
	SignalLine     *float64 `json:"signalLine"`
	Histogram      *float64 `json:"histogram"`
	BBUpper        *float64 `json:"bbUpper"`
	BBMiddle       *float64 `json:"bbMiddle"`
	BBLower        *float64 `json:"bbLower"`
	BBBandwidth    *float64 `json:"bbBandwidth"`
	ATR14          *float64 `json:"atr14"`
	VolumeSMA20    *float64 `json:"volumeSma20"`   // 出来高20期間SMA
	VolumeRatio    *float64 `json:"volumeRatio"`   // 最新出来高 / VolumeSMA20
	RecentSqueeze  *bool    `json:"recentSqueeze"` // 直近5本以内に BBBandwidth < 0.02
	Timestamp      int64    `json:"timestamp"`
}
