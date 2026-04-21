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

	// PR-6: ADX (Average Directional Index) family. ADX14 = 0-100 trend
	// strength, PlusDI14 / MinusDI14 = Wilder-smoothed directional pressure.
	// nil when insufficient data (need 2*period+1 bars).
	ADX14     *float64 `json:"adx14"`
	PlusDI14  *float64 `json:"plusDi14"`
	MinusDI14 *float64 `json:"minusDi14"`

	// PR-7: Stochastics family.
	// StochK14_3 = slow %K (raw stoch over 14 bars, smoothed 3).
	// StochD14_3 = %D (SMA3 of slow %K).
	// StochRSI14 = stochastic RSI over a 14-bar RSI window.
	// nil when insufficient data.
	StochK14_3 *float64 `json:"stochK14_3"`
	StochD14_3 *float64 `json:"stochD14_3"`
	StochRSI14 *float64 `json:"stochRsi14"`

	// PR-8: Ichimoku cloud snapshot. nil when the warmup is insufficient.
	// Individual fields inside IchimokuSnapshot are omitted from JSON when
	// they could not be computed (yields cleaner payloads during warmup).
	Ichimoku *IchimokuSnapshot `json:"ichimoku,omitempty"`

	Timestamp int64 `json:"timestamp"`
}

// IchimokuSnapshot is the per-bar Ichimoku Kinkō Hyō reading exposed to the
// Strategy layer and Frontend. All fields are pointers so the JSON payload
// distinguishes "not yet defined during warmup" from "zero".
type IchimokuSnapshot struct {
	Tenkan  *float64 `json:"tenkan,omitempty"`  // conversion line (9)
	Kijun   *float64 `json:"kijun,omitempty"`   // base line (26)
	SenkouA *float64 `json:"senkouA,omitempty"` // leading span A (Tenkan+Kijun)/2, plotted +26
	SenkouB *float64 `json:"senkouB,omitempty"` // leading span B (high/low mid over 52), plotted +26
	Chikou  *float64 `json:"chikou,omitempty"`  // lagging span — latest close
}
