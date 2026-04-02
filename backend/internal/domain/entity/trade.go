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

type MyTrade struct {
	ID               int64     `json:"id"`
	SymbolID         int64     `json:"symbolId"`
	OrderSide        OrderSide `json:"orderSide"`
	Price            float64   `json:"price"`
	Amount           float64   `json:"amount"`
	Profit           float64   `json:"profit"`
	Fee              float64   `json:"fee"`
	PositionFee      float64   `json:"positionFee"`
	CloseTradeProfit float64   `json:"closeTradeProfit"`
	OrderID          int64     `json:"orderId"`
	PositionID       int64     `json:"positionId"`
	CreatedAt        int64     `json:"createdAt"`
}
