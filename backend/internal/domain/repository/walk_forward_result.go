package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// WalkForwardResultFilter is the query surface for List.
// Empty strings and zero values mean "no filter".
type WalkForwardResultFilter struct {
	Limit  int
	Offset int

	BaseProfile string
	PDCACycleID string
}

// WalkForwardResultRepository persists walk-forward envelopes. The runner
// returns WalkForwardResult embedded per-window BacktestResults so the
// repository stores the full envelope verbatim (no cross-table fan-out
// like multi_period_results). Request / aggregate are stored separately
// from the full envelope so list views can rank by RobustnessScore
// without parsing the (potentially large) result payload.
type WalkForwardResultRepository interface {
	// Save writes a new row. `request` is an opaque blob — the caller
	// chooses whether to round-trip it.
	Save(ctx context.Context, rec entity.WalkForwardPersisted) error
	List(ctx context.Context, filter WalkForwardResultFilter) ([]entity.WalkForwardPersisted, error)
	FindByID(ctx context.Context, id string) (*entity.WalkForwardPersisted, error)
}
