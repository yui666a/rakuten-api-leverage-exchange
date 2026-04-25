package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ExecutionQualityRepository persists periodic execution-quality snapshots
// so the API can serve cached values instead of recomputing on every request.
type ExecutionQualityRepository interface {
	// Save inserts one snapshot for symbolID at capturedAt (unix-millis).
	Save(ctx context.Context, symbolID int64, capturedAt int64, report entity.ExecutionQualityReport) error
	// Latest returns the most recent snapshot for symbolID, or (nil, nil)
	// when none exists yet.
	Latest(ctx context.Context, symbolID int64) (*entity.ExecutionQualityReport, error)
	// PurgeOlderThan deletes rows captured before cutoffMillis. Used by the
	// retention sweeper to bound disk usage.
	PurgeOlderThan(ctx context.Context, cutoffMillis int64) (int64, error)
}
