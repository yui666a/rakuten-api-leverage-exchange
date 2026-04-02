package entity

type Symbol struct {
	ID                   int64   `json:"id"`
	Authority            string  `json:"authority"`
	TradeType            string  `json:"tradeType"`
	CurrencyPair         string  `json:"currencyPair"`
	BaseCurrency         string  `json:"baseCurrency"`
	QuoteCurrency        string  `json:"quoteCurrency"`
	BaseScale            int     `json:"baseScale"`
	QuoteScale           int     `json:"quoteScale"`
	BaseStepAmount       float64 `json:"baseStepAmount"`
	MinOrderAmount       float64 `json:"minOrderAmount"`
	MaxOrderAmount       float64 `json:"maxOrderAmount"`
	MakerTradeFeePercent float64 `json:"makerTradeFeePercent"`
	TakerTradeFeePercent float64 `json:"takerTradeFeePercent"`
	CloseOnly            bool    `json:"closeOnly"`
	ViewOnly             bool    `json:"viewOnly"`
	Enabled              bool    `json:"enabled"`
}
