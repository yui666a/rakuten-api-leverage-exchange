package entity

type OrderbookEntry struct {
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"`
}

type Orderbook struct {
	SymbolID  int64            `json:"symbolId"`
	Asks      []OrderbookEntry `json:"asks"`
	Bids      []OrderbookEntry `json:"bids"`
	BestAsk   float64          `json:"bestAsk"`
	BestBid   float64          `json:"bestBid"`
	MidPrice  float64          `json:"midPrice"`
	Spread    float64          `json:"spread"`
	Timestamp int64            `json:"timestamp"`
}
