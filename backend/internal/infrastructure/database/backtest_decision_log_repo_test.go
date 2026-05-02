package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func openBacktestDecisionLogTestDB(t *testing.T) (*backtestDecisionLogRepoTestDB, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	cleanup := func() { db.Close() }
	return &backtestDecisionLogRepoTestDB{repo: NewBacktestDecisionLogRepository(db)}, cleanup
}

type backtestDecisionLogRepoTestDB struct {
	repo interface {
		Insert(ctx context.Context, rec entity.DecisionRecord, runID string) error
		ListByRun(ctx context.Context, runID string, limit int, cursor int64) ([]entity.DecisionRecord, int64, error)
		DeleteByRun(ctx context.Context, runID string) (int64, error)
		DeleteOlderThan(ctx context.Context, cutoff int64) (int64, error)
	}
}

func sampleBacktestDecisionRecord(barTs int64, createdAt int64) entity.DecisionRecord {
	return entity.DecisionRecord{
		BarCloseAt:      barTs,
		TriggerKind:     entity.DecisionTriggerBarClose,
		SymbolID:        7,
		CurrencyPair:    "LTC_JPY",
		PrimaryInterval: "PT15M",
		Stance:          "TREND_FOLLOW",
		LastPrice:       30210,
		SignalAction:    "HOLD",
		RiskOutcome:     entity.DecisionRiskSkipped,
		BookGateOutcome: entity.DecisionBookSkipped,
		OrderOutcome:    entity.DecisionOrderNoop,
		CreatedAt:       createdAt,
	}
}

func TestBacktestDecisionLogRepo_InsertAndListByRun(t *testing.T) {
	h, cleanup := openBacktestDecisionLogTestDB(t)
	defer cleanup()
	ctx := context.Background()

	runA := "run-aaa"
	runB := "run-bbb"
	now := time.Now().UnixMilli()

	for _, runID := range []string{runA, runA, runB} {
		if err := h.repo.Insert(ctx, sampleBacktestDecisionRecord(1745654700000, now), runID); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	rowsA, _, err := h.repo.ListByRun(ctx, runA, 100, 0)
	if err != nil {
		t.Fatalf("ListByRun A: %v", err)
	}
	if len(rowsA) != 2 {
		t.Errorf("runA rows = %d, want 2", len(rowsA))
	}
	rowsB, _, err := h.repo.ListByRun(ctx, runB, 100, 0)
	if err != nil {
		t.Fatalf("ListByRun B: %v", err)
	}
	if len(rowsB) != 1 {
		t.Errorf("runB rows = %d, want 1", len(rowsB))
	}
}

func TestBacktestDecisionLogRepo_DeleteByRun(t *testing.T) {
	h, cleanup := openBacktestDecisionLogTestDB(t)
	defer cleanup()
	ctx := context.Background()

	runA := "run-aaa"
	runB := "run-bbb"
	now := time.Now().UnixMilli()

	for i := 0; i < 5; i++ {
		if err := h.repo.Insert(ctx, sampleBacktestDecisionRecord(1, now), runA); err != nil {
			t.Fatalf("Insert A: %v", err)
		}
	}
	if err := h.repo.Insert(ctx, sampleBacktestDecisionRecord(1, now), runB); err != nil {
		t.Fatalf("Insert B: %v", err)
	}

	deleted, err := h.repo.DeleteByRun(ctx, runA)
	if err != nil {
		t.Fatalf("DeleteByRun: %v", err)
	}
	if deleted != 5 {
		t.Errorf("deleted = %d, want 5", deleted)
	}
	rowsA, _, _ := h.repo.ListByRun(ctx, runA, 10, 0)
	if len(rowsA) != 0 {
		t.Errorf("runA rows after delete = %d, want 0", len(rowsA))
	}
	rowsB, _, _ := h.repo.ListByRun(ctx, runB, 10, 0)
	if len(rowsB) != 1 {
		t.Errorf("runB rows after delete = %d, want 1 (untouched)", len(rowsB))
	}
}

func TestBacktestDecisionLogRepo_DeleteOlderThan(t *testing.T) {
	h, cleanup := openBacktestDecisionLogTestDB(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UnixMilli()
	threeDays := int64(3 * 24 * 60 * 60 * 1000)

	if err := h.repo.Insert(ctx, sampleBacktestDecisionRecord(now-2*threeDays, now-2*threeDays), "run-old"); err != nil {
		t.Fatalf("Insert old: %v", err)
	}
	if err := h.repo.Insert(ctx, sampleBacktestDecisionRecord(now, now), "run-fresh"); err != nil {
		t.Fatalf("Insert fresh: %v", err)
	}

	deleted, err := h.repo.DeleteOlderThan(ctx, now-threeDays)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	rowsFresh, _, _ := h.repo.ListByRun(ctx, "run-fresh", 10, 0)
	if len(rowsFresh) != 1 {
		t.Errorf("fresh rows = %d, want 1", len(rowsFresh))
	}
	rowsOld, _, _ := h.repo.ListByRun(ctx, "run-old", 10, 0)
	if len(rowsOld) != 0 {
		t.Errorf("old rows = %d, want 0", len(rowsOld))
	}
}

// TestBacktestDecisionLogRepo_PhaseOneColumnsRoundTrip: Phase 1 で追加した
// 6 カラムが INSERT → SELECT で往復し、Update でも上書きできることを確認。
func TestBacktestDecisionLogRepo_PhaseOneColumnsRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	repo := &backtestDecisionLogRepo{db: db}
	ctx := context.Background()

	rec := sampleBacktestDecisionRecord(1745654700000, time.Now().UnixMilli())
	rec.SignalDirection = "BEARISH"
	rec.SignalStrength = 0.55
	rec.DecisionIntent = "EXIT_CANDIDATE"
	rec.DecisionSide = "SELL"
	rec.DecisionReason = "rsi overbought"
	rec.ExitPolicyOutcome = "VETOED"

	id, err := repo.InsertAndID(ctx, rec, "run-phase1")
	if err != nil {
		t.Fatalf("InsertAndID: %v", err)
	}

	rows, _, err := repo.ListByRun(ctx, "run-phase1", 10, 0)
	if err != nil {
		t.Fatalf("ListByRun: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	got := rows[0]
	if got.SignalDirection != "BEARISH" || got.SignalStrength != 0.55 ||
		got.DecisionIntent != "EXIT_CANDIDATE" || got.DecisionSide != "SELL" ||
		got.DecisionReason != "rsi overbought" || got.ExitPolicyOutcome != "VETOED" {
		t.Errorf("Phase 1 round-trip mismatch: %+v", got)
	}

	// Update でも Phase 1 カラムが反映されること。
	got.ID = id
	got.SignalDirection = "BULLISH"
	got.DecisionIntent = "NEW_ENTRY"
	got.DecisionSide = "BUY"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	rows, _, _ = repo.ListByRun(ctx, "run-phase1", 10, 0)
	if rows[0].SignalDirection != "BULLISH" || rows[0].DecisionIntent != "NEW_ENTRY" || rows[0].DecisionSide != "BUY" {
		t.Errorf("Update did not persist Phase 1 fields, got %+v", rows[0])
	}
}
