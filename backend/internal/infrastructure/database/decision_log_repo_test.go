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

// TestDecisionLogRepo_PhaseOneColumnsRoundTrip: Phase 1 で追加した 6 カラムが
// INSERT → SELECT でそのまま往復することを確認 (値あり / ゼロ値の両方)。
func TestDecisionLogRepo_PhaseOneColumnsRoundTrip(t *testing.T) {
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

	// Case 1: 値ありで往復。
	withValues := newDecisionRecord(7, 1745654700000, 0)
	withValues.SignalDirection = "BULLISH"
	withValues.SignalStrength = 0.72
	withValues.DecisionIntent = "NEW_ENTRY"
	withValues.DecisionSide = "BUY"
	withValues.DecisionReason = "rsi oversold; trend up"
	withValues.ExitPolicyOutcome = "ALLOWED"

	if err := repo.Insert(ctx, withValues); err != nil {
		t.Fatalf("Insert (values): %v", err)
	}

	// Case 2: ゼロ値のまま (PR1 中の通常パス)。
	zero := newDecisionRecord(7, 1745654700000+900_000, 0)
	if err := repo.Insert(ctx, zero); err != nil {
		t.Fatalf("Insert (zero): %v", err)
	}

	rows, _, err := repo.List(ctx, repository.DecisionLogFilter{SymbolID: 7, Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	// rows は新しい順なので、index 0 が zero, index 1 が withValues。
	gotZero := rows[0]
	if gotZero.SignalDirection != "" || gotZero.SignalStrength != 0 ||
		gotZero.DecisionIntent != "" || gotZero.DecisionSide != "" ||
		gotZero.DecisionReason != "" || gotZero.ExitPolicyOutcome != "" {
		t.Errorf("zero-value row should have empty Phase 1 fields, got %+v", gotZero)
	}

	got := rows[1]
	if got.SignalDirection != "BULLISH" {
		t.Errorf("SignalDirection = %q, want BULLISH", got.SignalDirection)
	}
	if got.SignalStrength != 0.72 {
		t.Errorf("SignalStrength = %v, want 0.72", got.SignalStrength)
	}
	if got.DecisionIntent != "NEW_ENTRY" {
		t.Errorf("DecisionIntent = %q, want NEW_ENTRY", got.DecisionIntent)
	}
	if got.DecisionSide != "BUY" {
		t.Errorf("DecisionSide = %q, want BUY", got.DecisionSide)
	}
	if got.DecisionReason != "rsi oversold; trend up" {
		t.Errorf("DecisionReason = %q, want rsi oversold; trend up", got.DecisionReason)
	}
	if got.ExitPolicyOutcome != "ALLOWED" {
		t.Errorf("ExitPolicyOutcome = %q, want ALLOWED", got.ExitPolicyOutcome)
	}
}

// TestDecisionLogRepo_UpdatePreservesPhaseOneColumns: Update で Phase 1 カラムが
// 上書きされることを確認 (PR2 で recorder が後から値を埋める想定)。
func TestDecisionLogRepo_UpdatePreservesPhaseOneColumns(t *testing.T) {
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

	rec := newDecisionRecord(7, 1745654700000, 0)
	id, err := repo.InsertAndID(ctx, rec)
	if err != nil {
		t.Fatalf("InsertAndID: %v", err)
	}

	rec.ID = id
	rec.SignalDirection = "BEARISH"
	rec.DecisionIntent = "EXIT_CANDIDATE"
	rec.DecisionSide = "SELL"
	rec.DecisionReason = "trailing stop trigger"
	if err := repo.Update(ctx, rec); err != nil {
		t.Fatalf("Update: %v", err)
	}

	rows, _, err := repo.List(ctx, repository.DecisionLogFilter{SymbolID: 7, Limit: 1})
	if err != nil {
		t.Fatalf("List after update: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].SignalDirection != "BEARISH" || rows[0].DecisionIntent != "EXIT_CANDIDATE" ||
		rows[0].DecisionSide != "SELL" || rows[0].DecisionReason != "trailing stop trigger" {
		t.Errorf("Update did not persist Phase 1 fields, got %+v", rows[0])
	}
}
