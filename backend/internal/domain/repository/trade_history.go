package repository

import "context"

// TradeRecord は取引履歴の1レコード。
type TradeRecord struct {
	ID         int64   `json:"id"`
	SymbolID   int64   `json:"symbolId"`
	OrderID    int64   `json:"orderId"`
	Side       string  `json:"side"`
	Action     string  `json:"action"`
	Price      float64 `json:"price"`
	Amount     float64 `json:"amount"`
	Reason     string  `json:"reason"`
	IsStopLoss bool    `json:"isStopLoss"`
	CreatedAt  int64   `json:"createdAt"`
}

// TradeHistoryRepository は取引履歴の永続化インターフェース。
type TradeHistoryRepository interface {
	Save(ctx context.Context, record TradeRecord) error
	GetRecent(ctx context.Context, symbolID int64, limit int) ([]TradeRecord, error)
}
