package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// BacktestResultFilter defines query options for listing persisted results.
//
// String filters (ProfileName, PDCACycleID) are inclusive exact-match filters;
// an empty string means "no filter". Pointer filters (ParentResultID, HasParent)
// treat nil as "no filter".
//
// If both ParentResultID and HasParent are supplied, ParentResultID takes
// precedence and HasParent is ignored (see design doc §5.3).
type BacktestResultFilter struct {
	Limit  int
	Offset int

	// ProfileName filters by exact ProfileName match when non-empty.
	ProfileName string
	// PDCACycleID filters by exact PDCACycleID match when non-empty.
	PDCACycleID string
	// ParentResultID filters rows where parent_result_id equals *ParentResultID
	// when non-nil. An empty-string pointer value is a legitimate filter value
	// (not treated as "no filter").
	ParentResultID *string
	// HasParent filters rows by parent_result_id IS [NOT] NULL when non-nil.
	// Ignored if ParentResultID is also non-nil.
	HasParent *bool
}

// BacktestResultRepository persists backtest summaries and trade logs.
type BacktestResultRepository interface {
	Save(ctx context.Context, result entity.BacktestResult) error
	List(ctx context.Context, filter BacktestResultFilter) ([]entity.BacktestResult, error)
	FindByID(ctx context.Context, id string) (*entity.BacktestResult, error)
	DeleteOlderThan(ctx context.Context, timestamp int64) (int64, error)
}
