package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func setupExecutionQualityRepo(t *testing.T) *ExecutionQualityRepo {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewExecutionQualityRepo(db)
}

func TestExecutionQualityRepo_SaveAndLatest(t *testing.T) {
	repo := setupExecutionQualityRepo(t)
	ctx := context.Background()

	avg := 12.5
	report := entity.ExecutionQualityReport{
		WindowSec: 86400,
		From:      1000,
		To:        87400,
		Trades: entity.ExecutionQualityTrades{
			Count: 5, MakerCount: 3, TakerCount: 2, UnknownCount: 0,
			MakerRatio: 0.6, TotalFeeJPY: -1.23,
			AvgSlippageBps: &avg,
			ByOrderBehavior: map[string]entity.ExecutionQualityBehaviorBucket{
				"OPEN": {Count: 3, MakerCount: 2, MakerRatio: 0.667, FeeJPY: -0.5},
			},
		},
		CircuitBreaker: entity.ExecutionQualityCircuitBreaker{
			Halted: true, HaltReason: "circuit_breaker:price_jump",
		},
	}
	if err := repo.Save(ctx, 7, 5_000_000, report); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.Latest(ctx, 7)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil report")
	}
	if got.Trades.Count != 5 || got.Trades.MakerCount != 3 {
		t.Fatalf("counts mismatch: %+v", got.Trades)
	}
	if got.Trades.AvgSlippageBps == nil || *got.Trades.AvgSlippageBps != 12.5 {
		t.Fatalf("avg slippage mismatch: %v", got.Trades.AvgSlippageBps)
	}
	if !got.CircuitBreaker.Halted || got.CircuitBreaker.HaltReason == "" {
		t.Fatalf("halt info mismatch: %+v", got.CircuitBreaker)
	}
	if got.Trades.ByOrderBehavior["OPEN"].MakerCount != 2 {
		t.Fatalf("bucket round-trip failed: %+v", got.Trades.ByOrderBehavior)
	}
}

func TestExecutionQualityRepo_LatestReturnsNilWhenEmpty(t *testing.T) {
	repo := setupExecutionQualityRepo(t)
	got, err := repo.Latest(context.Background(), 7)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty table, got %+v", got)
	}
}

func TestExecutionQualityRepo_LatestPicksMostRecent(t *testing.T) {
	repo := setupExecutionQualityRepo(t)
	ctx := context.Background()
	a := entity.ExecutionQualityReport{WindowSec: 86400, Trades: entity.ExecutionQualityTrades{Count: 1}}
	b := entity.ExecutionQualityReport{WindowSec: 86400, Trades: entity.ExecutionQualityTrades{Count: 2}}
	if err := repo.Save(ctx, 7, 1000, a); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if err := repo.Save(ctx, 7, 2000, b); err != nil {
		t.Fatalf("save b: %v", err)
	}
	got, _ := repo.Latest(ctx, 7)
	if got.Trades.Count != 2 {
		t.Fatalf("expected newest count=2, got %d", got.Trades.Count)
	}
}

func TestExecutionQualityRepo_PurgeOlderThan(t *testing.T) {
	repo := setupExecutionQualityRepo(t)
	ctx := context.Background()
	_ = repo.Save(ctx, 7, 1000, entity.ExecutionQualityReport{WindowSec: 86400})
	_ = repo.Save(ctx, 7, 5000, entity.ExecutionQualityReport{WindowSec: 86400})
	deleted, err := repo.PurgeOlderThan(ctx, 3000)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted, got %d", deleted)
	}
}
