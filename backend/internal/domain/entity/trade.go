package entity

type MarketTrade struct {
	ID          int64   `json:"id"`
	OrderSide   string  `json:"orderSide"`
	Price       float64 `json:"price"`
	Amount      float64 `json:"amount"`
	AssetAmount float64 `json:"assetAmount"`
	TradedAt    int64   `json:"tradedAt"`
}

type MarketTradesResponse struct {
	SymbolID  int64         `json:"symbolId"`
	Trades    []MarketTrade `json:"trades"`
	Timestamp int64         `json:"timestamp"`
}
