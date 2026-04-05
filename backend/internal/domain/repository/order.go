package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// OrderClient は注文操作のインターフェース。
type OrderClient interface {
	CreateOrder(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error)
	CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error)
	GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error)
	GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error)
	GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error)
}
