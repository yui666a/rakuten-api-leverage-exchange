package decisionlog

import (
	"context"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

type stubBacktestRepoForAdapter struct {
	rec   entity.DecisionRecord
	runID string
	seen  bool
}

func (s *stubBacktestRepoForAdapter) Insert(_ context.Context, rec entity.DecisionRecord, runID string) error {
	s.rec = rec
	s.runID = runID
	s.seen = true
	return nil
}
func (s *stubBacktestRepoForAdapter) InsertAndID(_ context.Context, rec entity.DecisionRecord, runID string) (int64, error) {
	s.rec = rec
	s.runID = runID
	s.seen = true
	return 1, nil
}
func (s *stubBacktestRepoForAdapter) Update(_ context.Context, rec entity.DecisionRecord) error {
	s.rec = rec
	return nil
}
func (s *stubBacktestRepoForAdapter) ListByRun(_ context.Context, _ string, _ int, _ int64) ([]entity.DecisionRecord, int64, error) {
	return nil, 0, nil
}
func (s *stubBacktestRepoForAdapter) DeleteByRun(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (s *stubBacktestRepoForAdapter) DeleteOlderThan(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func TestBacktestRepoAdapter_BindsRunIDOnEveryInsert(t *testing.T) {
	underlying := &stubBacktestRepoForAdapter{}
	adapter := NewBacktestRepoAdapter(underlying, "run-xyz")

	var _ repository.DecisionLogRepository = adapter

	rec := entity.DecisionRecord{BarCloseAt: 1_000, SymbolID: 7}
	if err := adapter.Insert(context.Background(), rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if !underlying.seen {
		t.Fatalf("underlying repo Insert was not called")
	}
	if underlying.runID != "run-xyz" {
		t.Errorf("runID = %q, want %q", underlying.runID, "run-xyz")
	}
	if underlying.rec.BarCloseAt != 1_000 {
		t.Errorf("record not forwarded: %+v", underlying.rec)
	}
}

func TestBacktestRepoAdapter_ListReturnsEmpty(t *testing.T) {
	adapter := NewBacktestRepoAdapter(&stubBacktestRepoForAdapter{}, "run-xyz")
	rows, next, err := adapter.List(context.Background(), repository.DecisionLogFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if rows != nil || next != 0 {
		t.Errorf("List must be a no-op for the adapter (recorder never reads)")
	}
}
