package repository

import "context"

// TradingConfigState は永続化された取引設定。pipeline の起動時に
// 復元されるため、再起動を跨いで「最後に選んでいた銘柄/サイズ」が
// 失われない。
type TradingConfigState struct {
	SymbolID    int64   `json:"symbolId"`
	TradeAmount float64 `json:"tradeAmount"`
	UpdatedAt   int64   `json:"updatedAt"`
}

// TradingConfigRepository は取引設定の永続化インターフェース。
type TradingConfigRepository interface {
	Save(ctx context.Context, state TradingConfigState) error
	Load(ctx context.Context) (*TradingConfigState, error)
}
