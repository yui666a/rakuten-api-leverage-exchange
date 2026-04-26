package entity

// IndicatorSet はある時点の全テクニカル指標をまとめた構造体。
// データ不足で計算できない指標はnilになる。
//
// フィールド名は「期間中立」: SMAShort/SMALong は期間値を含まず、
// 実際の lookback は profile.indicators.* で決まる。PT15M で sma_short=20,
// sma_long=50 を使えば従来挙動と等価。PT5M なら sma_short=60, sma_long=150
// 等にスケールできる。
type IndicatorSet struct {
	SymbolID      int64    `json:"symbolId"`
	SMAShort      *float64 `json:"smaShort"`
	SMALong       *float64 `json:"smaLong"`
	EMAFast       *float64 `json:"emaFast"`
	EMASlow       *float64 `json:"emaSlow"`
	RSI           *float64 `json:"rsi"`
	MACDLine      *float64 `json:"macdLine"`
	SignalLine    *float64 `json:"signalLine"`
	Histogram     *float64 `json:"histogram"`
	BBUpper       *float64 `json:"bbUpper"`
	BBMiddle      *float64 `json:"bbMiddle"`
	BBLower       *float64 `json:"bbLower"`
	BBBandwidth   *float64 `json:"bbBandwidth"`
	ATR           *float64 `json:"atr"`
	VolumeSMA     *float64 `json:"volumeSma"`     // 出来高 SMA (期間は profile.indicators.volume_sma_period)
	VolumeRatio   *float64 `json:"volumeRatio"`   // 最新出来高 / VolumeSMA
	RecentSqueeze *bool    `json:"recentSqueeze"` // BBBandwidth < 0.02 が直近 bb_squeeze_lookback 本以内に発生

	// PR-6: ADX (Average Directional Index) family. ADX = 0-100 trend strength,
	// PlusDI / MinusDI = Wilder-smoothed directional pressure. nil when
	// insufficient data (need 2*period+1 bars). 期間は profile.indicators.adx_period。
	ADX     *float64 `json:"adx"`
	PlusDI  *float64 `json:"plusDi"`
	MinusDI *float64 `json:"minusDi"`

	// PR-7: Stochastics family.
	// StochK = slow %K (raw stoch over k_period bars, smoothed by smooth_k).
	// StochD = %D (SMA of slow %K with smooth_d).
	// StochRSI = stochastic RSI over an rsi_period-bar RSI window.
	// nil when insufficient data.
	StochK   *float64 `json:"stochK"`
	StochD   *float64 `json:"stochD"`
	StochRSI *float64 `json:"stochRsi"`

	// PR-8: Ichimoku cloud snapshot. nil when the warmup is insufficient.
	// Individual fields inside IchimokuSnapshot are omitted from JSON when
	// they could not be computed (yields cleaner payloads during warmup).
	Ichimoku *IchimokuSnapshot `json:"ichimoku,omitempty"`

	// PR-11: Donchian Channel (N-bar high/low). nil when insufficient
	// history is available. Upper/Lower bound the most recent N bars
	// inclusive of the current bar so `lastPrice > DonchianUpper` is a
	// direct upside breakout probe for the configurable strategy's breakout
	// stance. 期間は profile.indicators.donchian_period。
	DonchianUpper  *float64 `json:"donchianUpper"`
	DonchianLower  *float64 `json:"donchianLower"`
	DonchianMiddle *float64 `json:"donchianMiddle"`

	// PR-9: Volume-based oscillators. OBV is a cumulative scalar whose
	// absolute value is meaningless; OBVSlope is (OBV_now − OBV_{−N}) and
	// carries the gate signal (positive = net buying volume over the last
	// N bars). CMF is bounded in [-1, 1]. Both nil during warmup. 期間は
	// profile.indicators.obv_slope_period / cmf_period。
	OBV      *float64 `json:"obv"`
	OBVSlope *float64 `json:"obvSlope"`
	CMF      *float64 `json:"cmf"`

	// Orderbook-derived signals (PR-J). All nil unless a BookSource is wired
	// into the IndicatorHandler and a recent enough snapshot exists.
	//
	// Microprice = (BestBid * askVol + BestAsk * bidVol) / (askVol + bidVol)
	// — leans toward the heavier side of the book; richer than plain mid.
	Microprice *float64 `json:"microprice,omitempty"`

	// OFIShort / OFILong = sum over the rolling window (10s / 60s by default)
	// of (Δbid_topN_depth − Δask_topN_depth), normalised by topN_depth so the
	// number is dimensionless ([-1, +1]ish). Positive = bid pressure
	// (taker-buy intent), negative = ask pressure.
	OFIShort *float64 `json:"ofiShort,omitempty"`
	OFILong  *float64 `json:"ofiLong,omitempty"`

	Timestamp int64 `json:"timestamp"`
}

// IchimokuSnapshot is the per-bar Ichimoku Kinkō Hyō reading exposed to the
// Strategy layer and Frontend. All fields are pointers so the JSON payload
// distinguishes "not yet defined during warmup" from "zero". 期間は
// profile.indicators.ichimoku_tenkan/kijun/senkou_b で設定 (PR-C で配線)。
type IchimokuSnapshot struct {
	Tenkan  *float64 `json:"tenkan,omitempty"`  // conversion line
	Kijun   *float64 `json:"kijun,omitempty"`   // base line
	SenkouA *float64 `json:"senkouA,omitempty"` // leading span A (Tenkan+Kijun)/2, plotted +kijun
	SenkouB *float64 `json:"senkouB,omitempty"` // leading span B (high/low mid over senkou_b), plotted +kijun
	Chikou  *float64 `json:"chikou,omitempty"`  // lagging span — latest close
}
