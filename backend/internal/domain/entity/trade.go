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

// MyTrade の数値フィールドは楽天 API がシンボルによって number と string の
// どちらでも返してくる（例: LTC_JPY は price を string で返すケースが観測されている）。
// Symbol 型と同様に StringFloat64 で受け、Marshal 時は number に正規化する。
type MyTrade struct {
	ID               int64         `json:"id"`
	SymbolID         int64         `json:"symbolId"`
	OrderSide        OrderSide     `json:"orderSide"`
	Price            StringFloat64 `json:"price"`
	Amount           StringFloat64 `json:"amount"`
	Profit           StringFloat64 `json:"profit"`
	Fee              StringFloat64 `json:"fee"`
	PositionFee      StringFloat64 `json:"positionFee"`
	CloseTradeProfit StringFloat64 `json:"closeTradeProfit"`
	OrderID          int64         `json:"orderId"`
	PositionID       int64         `json:"positionId"`
	CreatedAt        int64         `json:"createdAt"`
}
