package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// BacktestResultFilter defines query options for listing persisted results.
type BacktestResultFilter struct {
	Limit  int
	Offset int
}

// BacktestResultRepository persists backtest summaries and trade logs.
type BacktestResultRepository interface {
	Save(ctx context.Context, result entity.BacktestResult) error
	List(ctx context.Context, filter BacktestResultFilter) ([]entity.BacktestResult, error)
	FindByID(ctx context.Context, id string) (*entity.BacktestResult, error)
	DeleteOlderThan(ctx context.Context, timestamp int64) (int64, error)
}
