package decisionlog

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// backtestRepoAdapter binds a runID to a BacktestDecisionLogRepository so the
// generic Recorder can write into it without knowing about run scoping.
// List is a no-op because the recorder never reads back from its repo.
type backtestRepoAdapter struct {
	repo  repository.BacktestDecisionLogRepository
	runID string
}

// NewBacktestRepoAdapter returns a repository.DecisionLogRepository that
// forwards every Insert to repo.Insert(ctx, rec, runID). Use this to plug
// the live Recorder into a backtest run without touching the recorder code.
func NewBacktestRepoAdapter(repo repository.BacktestDecisionLogRepository, runID string) repository.DecisionLogRepository {
	return &backtestRepoAdapter{repo: repo, runID: runID}
}

func (a *backtestRepoAdapter) Insert(ctx context.Context, rec entity.DecisionRecord) error {
	return a.repo.Insert(ctx, rec, a.runID)
}

func (a *backtestRepoAdapter) List(_ context.Context, _ repository.DecisionLogFilter) ([]entity.DecisionRecord, int64, error) {
	return nil, 0, nil
}
