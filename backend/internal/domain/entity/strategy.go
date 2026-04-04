package entity

// MarketStance はLLMが判断する相場の戦略方針。
type MarketStance string

const (
	MarketStanceTrendFollow MarketStance = "TREND_FOLLOW"
	MarketStanceContrarian  MarketStance = "CONTRARIAN"
	MarketStanceHold        MarketStance = "HOLD"
)

// StrategyAdvice はLLM Serviceが返す戦略アドバイス。
type StrategyAdvice struct {
	Stance    MarketStance `json:"stance"`
	Reasoning string       `json:"reasoning"`
	UpdatedAt int64        `json:"updatedAt"`
}

// MarketContext はLLMに渡す相場コンテキスト情報。
type MarketContext struct {
	SymbolID   int64        `json:"symbolId"`
	LastPrice  float64      `json:"lastPrice"`
	Indicators IndicatorSet `json:"indicators"`
}
