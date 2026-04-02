package entity

type Candle struct {
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
	Time   int64   `json:"time"`
}

type CandlestickResponse struct {
	SymbolID     int64    `json:"symbolId"`
	Candlesticks []Candle `json:"candlesticks"`
	Timestamp    int64    `json:"timestamp"`
}
