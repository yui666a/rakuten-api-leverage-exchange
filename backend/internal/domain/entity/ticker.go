package entity

type Ticker struct {
	SymbolID  int64   `json:"symbolId"`
	BestAsk   float64 `json:"bestAsk"`
	BestBid   float64 `json:"bestBid"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Last      float64 `json:"last"`
	Volume    float64 `json:"volume"`
	Timestamp int64   `json:"timestamp"`
}
