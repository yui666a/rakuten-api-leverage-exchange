package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// MultiPeriodResultFilter defines query options for listing multi-period
// runs. Empty strings mean "no filter".
type MultiPeriodResultFilter struct {
	Limit  int
	Offset int

	ProfileName string
	PDCACycleID string
}

// MultiPeriodResultRepository persists the envelope of a multi-period
// backtest run. The individual BacktestResult rows referenced by
// MultiPeriodResult.Periods live in the existing backtest_results table and
// are saved via BacktestResultRepository.Save; this repository only stores
// the aggregate and the ID cross-references.
type MultiPeriodResultRepository interface {
	Save(ctx context.Context, result entity.MultiPeriodResult) error
	List(ctx context.Context, filter MultiPeriodResultFilter) ([]entity.MultiPeriodResult, error)
	FindByID(ctx context.Context, id string) (*entity.MultiPeriodResult, error)
}
