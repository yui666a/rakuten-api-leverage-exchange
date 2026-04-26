package repository

import (
	"context"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type minimalRepo struct{}

func (*minimalRepo) Insert(_ context.Context, _ entity.DecisionRecord) error { return nil }
func (*minimalRepo) InsertAndID(_ context.Context, _ entity.DecisionRecord) (int64, error) {
	return 0, nil
}
func (*minimalRepo) Update(_ context.Context, _ entity.DecisionRecord) error { return nil }
func (*minimalRepo) List(_ context.Context, _ DecisionLogFilter) ([]entity.DecisionRecord, int64, error) {
	return nil, 0, nil
}

type minimalBacktestRepo struct{}

func (*minimalBacktestRepo) Insert(_ context.Context, _ entity.DecisionRecord, _ string) error {
	return nil
}
func (*minimalBacktestRepo) InsertAndID(_ context.Context, _ entity.DecisionRecord, _ string) (int64, error) {
	return 0, nil
}
func (*minimalBacktestRepo) Update(_ context.Context, _ entity.DecisionRecord) error { return nil }
func (*minimalBacktestRepo) ListByRun(_ context.Context, _ string, _ int, _ int64) ([]entity.DecisionRecord, int64, error) {
	return nil, 0, nil
}
func (*minimalBacktestRepo) DeleteByRun(_ context.Context, _ string) (int64, error) { return 0, nil }
func (*minimalBacktestRepo) DeleteOlderThan(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func TestDecisionLogFilter_ZeroValueIsAllSymbols(t *testing.T) {
	var f DecisionLogFilter
	if f.SymbolID != 0 || f.Limit != 0 {
		t.Errorf("zero value must be all-zero: %+v", f)
	}
}

func TestDecisionLogRepository_InterfaceShape(t *testing.T) {
	var _ DecisionLogRepository = (*minimalRepo)(nil)
	var _ BacktestDecisionLogRepository = (*minimalBacktestRepo)(nil)
}
