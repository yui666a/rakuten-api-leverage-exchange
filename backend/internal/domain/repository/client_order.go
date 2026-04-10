package repository

import "context"

type ClientOrderRecord struct {
	ClientOrderID string
	Executed      bool
	OrderID       int64
	CreatedAt     int64
}

type ClientOrderRepository interface {
	Find(ctx context.Context, clientOrderID string) (*ClientOrderRecord, error)
	Save(ctx context.Context, record ClientOrderRecord) error
	DeleteExpired(ctx context.Context, beforeUnix int64) error
}
