package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// DecisionLogFilter narrows a List query. Zero values mean "no filter":
//   - SymbolID == 0    -> all symbols
//   - From == 0        -> no lower bound
//   - To == 0          -> no upper bound
//   - Cursor == 0      -> latest page
//   - Limit <= 0       -> repository default
type DecisionLogFilter struct {
	SymbolID int64
	From     int64 // unix ms inclusive
	To       int64 // unix ms inclusive
	Cursor   int64 // returns rows with id < Cursor
	Limit    int
}

// DecisionLogRepository persists live-pipeline decisions. Implementations
// must be safe for concurrent reads but a single recorder writes serially.
type DecisionLogRepository interface {
	Insert(ctx context.Context, record entity.DecisionRecord) error
	// List returns rows newest-first along with the next cursor (the id of
	// the oldest row in the page, suitable as Cursor for the next call).
	// nextCursor == 0 means "no more rows".
	List(ctx context.Context, filter DecisionLogFilter) (records []entity.DecisionRecord, nextCursor int64, err error)
}

// BacktestDecisionLogRepository scopes records to a backtest run id and
// supports retention sweeping. Insert ties each record to runID; ListByRun
// returns the run's rows newest-first; Delete* enables both immediate and
// scheduled cleanup.
type BacktestDecisionLogRepository interface {
	Insert(ctx context.Context, record entity.DecisionRecord, runID string) error
	ListByRun(ctx context.Context, runID string, limit int, cursor int64) (records []entity.DecisionRecord, nextCursor int64, err error)
	DeleteByRun(ctx context.Context, runID string) (deleted int64, err error)
	DeleteOlderThan(ctx context.Context, cutoff int64) (deleted int64, err error)
}
