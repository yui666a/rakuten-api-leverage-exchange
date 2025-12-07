package domain

import "context"

// MarketRepository defines the interface for market data operations
type MarketRepository interface {
	GetMarket(ctx context.Context, symbol string) (*Market, error)
	GetAllMarkets(ctx context.Context) ([]Market, error)
}

// OrderRepository defines the interface for order operations
type OrderRepository interface {
	CreateOrder(ctx context.Context, order *Order) error
	GetOrder(ctx context.Context, id string) (*Order, error)
	GetOrders(ctx context.Context) ([]Order, error)
	CancelOrder(ctx context.Context, id string) error
}

// AccountRepository defines the interface for account operations
type AccountRepository interface {
	GetAccount(ctx context.Context, id string) (*Account, error)
	UpdateBalance(ctx context.Context, id string, balance float64) error
}
