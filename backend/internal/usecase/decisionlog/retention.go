package decisionlog

import (
	"context"
	"log/slog"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// RetentionConfig controls the backtest decision-log cleanup loop.
//
//   - MaxAge: rows older than now-MaxAge get deleted.
//   - Interval: how often the loop runs.
//   - NowFn: clock injection for tests; nil falls back to time.Now.
type RetentionConfig struct {
	MaxAge   time.Duration
	Interval time.Duration
	NowFn    func() time.Time
}

// RetentionCleanup periodically deletes backtest decision-log rows older
// than MaxAge. It runs an initial sweep at start, then sweeps every
// Interval until the context is cancelled. Errors are logged at warn level
// and do not abort the loop.
type RetentionCleanup struct {
	repo   repository.BacktestDecisionLogRepository
	cfg    RetentionConfig
	logger *slog.Logger
}

func NewRetentionCleanup(repo repository.BacktestDecisionLogRepository, cfg RetentionConfig) *RetentionCleanup {
	if cfg.NowFn == nil {
		cfg.NowFn = time.Now
	}
	return &RetentionCleanup{repo: repo, cfg: cfg, logger: slog.Default()}
}

// Run blocks until ctx is cancelled.
func (c *RetentionCleanup) Run(ctx context.Context) {
	c.sweep(ctx)
	if c.cfg.Interval <= 0 {
		return
	}
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sweep(ctx)
		}
	}
}

func (c *RetentionCleanup) sweep(ctx context.Context) {
	cutoff := c.cfg.NowFn().Add(-c.cfg.MaxAge).UnixMilli()
	deleted, err := c.repo.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		c.logger.Warn("decisionlog retention: sweep failed", "cutoff", cutoff, "error", err)
		return
	}
	if deleted > 0 {
		c.logger.Info("decisionlog retention: pruned rows", "deleted", deleted, "cutoff", cutoff)
	}
}
