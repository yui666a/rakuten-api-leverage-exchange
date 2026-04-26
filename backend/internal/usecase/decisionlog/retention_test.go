package decisionlog

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type stubBacktestRepo struct {
	mu      sync.Mutex
	cutoffs []int64
	err     error
}

func (s *stubBacktestRepo) Insert(_ context.Context, _ entity.DecisionRecord, _ string) error {
	return nil
}
func (s *stubBacktestRepo) InsertAndID(_ context.Context, _ entity.DecisionRecord, _ string) (int64, error) {
	return 1, nil
}
func (s *stubBacktestRepo) Update(_ context.Context, _ entity.DecisionRecord) error {
	return nil
}
func (s *stubBacktestRepo) ListByRun(_ context.Context, _ string, _ int, _ int64) ([]entity.DecisionRecord, int64, error) {
	return nil, 0, nil
}
func (s *stubBacktestRepo) DeleteByRun(_ context.Context, _ string) (int64, error) { return 0, nil }
func (s *stubBacktestRepo) DeleteOlderThan(_ context.Context, cutoff int64) (int64, error) {
	s.mu.Lock()
	s.cutoffs = append(s.cutoffs, cutoff)
	s.mu.Unlock()
	return 1, s.err
}

func TestRetention_RunsImmediatelyAndOnTicker(t *testing.T) {
	repo := &stubBacktestRepo{}
	fixedNow := int64(10_000_000)
	cleanup := NewRetentionCleanup(repo, RetentionConfig{
		MaxAge:   3 * 24 * time.Hour,
		Interval: 20 * time.Millisecond,
		NowFn:    func() time.Time { return time.UnixMilli(fixedNow) },
	})

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cleanup.Run(ctx)
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		repo.mu.Lock()
		n := len(repo.cutoffs)
		repo.mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.cutoffs) < 2 {
		t.Fatalf("expected >=2 sweeps, got %d", len(repo.cutoffs))
	}
	expected := fixedNow - int64(3*24*time.Hour/time.Millisecond)
	for _, c := range repo.cutoffs {
		if c != expected {
			t.Errorf("cutoff = %d, want %d", c, expected)
		}
	}
}

type countingErrorRepo struct {
	stub  *stubBacktestRepo
	count *atomic.Int32
}

func (c *countingErrorRepo) Insert(ctx context.Context, rec entity.DecisionRecord, runID string) error {
	return c.stub.Insert(ctx, rec, runID)
}
func (c *countingErrorRepo) InsertAndID(ctx context.Context, rec entity.DecisionRecord, runID string) (int64, error) {
	return c.stub.InsertAndID(ctx, rec, runID)
}
func (c *countingErrorRepo) Update(ctx context.Context, rec entity.DecisionRecord) error {
	return c.stub.Update(ctx, rec)
}
func (c *countingErrorRepo) ListByRun(ctx context.Context, runID string, limit int, cursor int64) ([]entity.DecisionRecord, int64, error) {
	return c.stub.ListByRun(ctx, runID, limit, cursor)
}
func (c *countingErrorRepo) DeleteByRun(ctx context.Context, runID string) (int64, error) {
	return c.stub.DeleteByRun(ctx, runID)
}
func (c *countingErrorRepo) DeleteOlderThan(ctx context.Context, cutoff int64) (int64, error) {
	c.count.Add(1)
	return c.stub.DeleteOlderThan(ctx, cutoff)
}

func TestRetention_DeleteErrorDoesNotKillLoop(t *testing.T) {
	repo := &stubBacktestRepo{err: errors.New("db down")}
	var sweeps atomic.Int32
	wrapped := &countingErrorRepo{stub: repo, count: &sweeps}

	cleanup := NewRetentionCleanup(wrapped, RetentionConfig{
		MaxAge:   3 * 24 * time.Hour,
		Interval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cleanup.Run(ctx)
	}()

	deadline := time.Now().Add(200 * time.Millisecond)
	for sweeps.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	if sweeps.Load() < 3 {
		t.Errorf("loop must continue after errors, sweeps = %d", sweeps.Load())
	}
}
