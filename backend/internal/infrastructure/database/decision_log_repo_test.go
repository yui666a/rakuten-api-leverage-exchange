package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

func newDecisionRecord(symbolID int64, barTs int64, seq int) entity.DecisionRecord {
	return entity.DecisionRecord{
		BarCloseAt:       barTs,
		SequenceInBar:    seq,
		TriggerKind:      entity.DecisionTriggerBarClose,
		SymbolID:         symbolID,
		CurrencyPair:     "LTC_JPY",
		PrimaryInterval:  "PT15M",
		Stance:           "TREND_FOLLOW",
		LastPrice:        30210,
		SignalAction:     "HOLD",
		SignalConfidence: 0,
		SignalReason:     "trend follow: ADX below threshold",
		RiskOutcome:      entity.DecisionRiskSkipped,
		BookGateOutcome:  entity.DecisionBookSkipped,
		OrderOutcome:     entity.DecisionOrderNoop,
		IndicatorsJSON:   `{"rsi":48.2}`,
		CreatedAt:        time.Now().UnixMilli(),
	}
}

func TestDecisionLogRepo_InsertAndList(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	repo := NewDecisionLogRepository(db)
	ctx := context.Background()

	base := int64(1745654700000)
	for i := 0; i < 3; i++ {
		rec := newDecisionRecord(7, base+int64(i)*900_000, 0)
		if err := repo.Insert(ctx, rec); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}

	rows, next, err := repo.List(ctx, repository.DecisionLogFilter{SymbolID: 7, Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("List len = %d, want 3", len(rows))
	}
	if rows[0].BarCloseAt < rows[1].BarCloseAt {
		t.Errorf("rows must be newest first, got %d before %d", rows[0].BarCloseAt, rows[1].BarCloseAt)
	}
	if next != 0 {
		t.Errorf("nextCursor must be 0 when fewer rows than limit, got %d", next)
	}
}

func TestDecisionLogRepo_CursorPaging(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	repo := NewDecisionLogRepository(db)
	ctx := context.Background()

	base := int64(1745654700000)
	for i := 0; i < 5; i++ {
		rec := newDecisionRecord(7, base+int64(i)*900_000, 0)
		if err := repo.Insert(ctx, rec); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}

	page1, next1, err := repo.List(ctx, repository.DecisionLogFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if len(page1) != 2 || next1 == 0 {
		t.Fatalf("page1 len=%d next=%d (want 2 / non-zero)", len(page1), next1)
	}

	page2, _, err := repo.List(ctx, repository.DecisionLogFilter{Limit: 10, Cursor: next1})
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2) != 3 {
		t.Errorf("page2 len = %d, want 3", len(page2))
	}
	for _, r := range page2 {
		if r.ID >= next1 {
			t.Errorf("page2 row id %d must be < cursor %d", r.ID, next1)
		}
	}
}

func TestDecisionLogRepo_FilterByTimeRange(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	repo := NewDecisionLogRepository(db)
	ctx := context.Background()

	base := int64(1745654700000)
	for i := 0; i < 5; i++ {
		rec := newDecisionRecord(7, base+int64(i)*900_000, 0)
		if err := repo.Insert(ctx, rec); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}

	rows, _, err := repo.List(ctx, repository.DecisionLogFilter{
		From:  base + 900_000,
		To:    base + 3*900_000,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("len = %d, want 3 (inclusive on both ends)", len(rows))
	}
}
