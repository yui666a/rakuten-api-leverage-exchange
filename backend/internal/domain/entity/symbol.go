package entity

// Symbol は楽天 CFD の取引可能銘柄メタ情報。
// 楽天 API は手数料や発注単位を string で返すため、該当フィールドは StringFloat64 で受ける。
// MarshalJSON 時は数値で出力されるため、フロント側の TradableSymbol 型（number）と整合する。
type Symbol struct {
	ID                   int64         `json:"id"`
	Authority            string        `json:"authority"`
	TradeType            string        `json:"tradeType"`
	CurrencyPair         string        `json:"currencyPair"`
	BaseCurrency         string        `json:"baseCurrency"`
	QuoteCurrency        string        `json:"quoteCurrency"`
	BaseScale            int           `json:"baseScale"`
	QuoteScale           int           `json:"quoteScale"`
	BaseStepAmount       StringFloat64 `json:"baseStepAmount"`
	MinOrderAmount       StringFloat64 `json:"minOrderAmount"`
	MaxOrderAmount       StringFloat64 `json:"maxOrderAmount"`
	MakerTradeFeePercent StringFloat64 `json:"makerTradeFeePercent"`
	TakerTradeFeePercent StringFloat64 `json:"takerTradeFeePercent"`
	CloseOnly            bool          `json:"closeOnly"`
	ViewOnly             bool          `json:"viewOnly"`
	Enabled              bool          `json:"enabled"`
}
