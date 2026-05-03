package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/exitplan"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

func openExitPlanTestDB(t *testing.T) *exitPlanTestDB {
	t.Helper()
	tmp := t.TempDir()
	db, err := NewDB(filepath.Join(tmp, "exitplan.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return &exitPlanTestDB{db: db, repo: NewExitPlanRepository(db)}
}

type exitPlanTestDB struct {
	db   interface{ Close() error }
	repo exitplan.Repository
}

func TestExitPlanRepo_CreateAndFind(t *testing.T) {
	h := openExitPlanTestDB(t)
	ctx := context.Background()

	plan := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := h.repo.Create(ctx, plan); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if plan.ID == 0 {
		t.Errorf("ID should be assigned after Create")
	}
	got, err := h.repo.FindByPositionID(ctx, 100)
	if err != nil {
		t.Fatalf("FindByPositionID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByPositionID: nil")
	}
	if got.PositionID != 100 || got.SymbolID != 7 || got.Side != entity.OrderSideBuy || got.EntryPrice != 10000 {
		t.Errorf("got = %+v", got)
	}
	if got.Policy.StopLoss.Percent != 1.5 || got.Policy.StopLoss.ATRMultiplier != 2.0 {
		t.Errorf("policy SL not roundtripped: %+v", got.Policy.StopLoss)
	}
	if got.Policy.TakeProfit.Percent != 3.0 {
		t.Errorf("policy TP not roundtripped: %+v", got.Policy.TakeProfit)
	}
	if got.Policy.Trailing.Mode != risk.TrailingModeATR || got.Policy.Trailing.ATRMultiplier != 2.5 {
		t.Errorf("policy trailing not roundtripped: %+v", got.Policy.Trailing)
	}
	if got.TrailingActivated || got.TrailingHWM != nil {
		t.Errorf("trailing should default not activated: %+v", got)
	}
	if got.ClosedAt != nil {
		t.Errorf("ClosedAt should default nil")
	}
}

func TestExitPlanRepo_FindByPositionID_notFound(t *testing.T) {
	h := openExitPlanTestDB(t)
	got, err := h.repo.FindByPositionID(context.Background(), 999)
	if err != nil {
		t.Fatalf("FindByPositionID: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing plan, got %+v", got)
	}
}

func TestExitPlanRepo_Create_uniquePositionID(t *testing.T) {
	h := openExitPlanTestDB(t)
	ctx := context.Background()
	p1 := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := h.repo.Create(ctx, p1); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	p2 := mustExitPlanForRepo(t, 100, 7, entity.OrderSideSell, 11000)
	if err := h.repo.Create(ctx, p2); err == nil {
		t.Errorf("second Create with same PositionID should fail")
	}
}

func TestExitPlanRepo_ListOpen_excludesClosed(t *testing.T) {
	h := openExitPlanTestDB(t)
	ctx := context.Background()
	open := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	closed := mustExitPlanForRepo(t, 101, 7, entity.OrderSideSell, 11000)
	otherSym := mustExitPlanForRepo(t, 102, 8, entity.OrderSideBuy, 5000)
	for _, p := range []*exitplan.ExitPlan{open, closed, otherSym} {
		if err := h.repo.Create(ctx, p); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := h.repo.Close(ctx, closed.ID, 1700000099999); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := h.repo.ListOpen(ctx, 7)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListOpen returned %d, want 1", len(got))
	}
	if got[0].PositionID != 100 {
		t.Errorf("expected open plan position 100, got %d", got[0].PositionID)
	}
}

func TestExitPlanRepo_UpdateTrailing(t *testing.T) {
	h := openExitPlanTestDB(t)
	ctx := context.Background()
	plan := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := h.repo.Create(ctx, plan); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := h.repo.UpdateTrailing(ctx, plan.ID, 10250, true, 1700000050000); err != nil {
		t.Fatalf("UpdateTrailing: %v", err)
	}
	got, _ := h.repo.FindByPositionID(ctx, 100)
	if !got.TrailingActivated {
		t.Errorf("TrailingActivated should be true")
	}
	if got.TrailingHWM == nil || *got.TrailingHWM != 10250 {
		t.Errorf("TrailingHWM = %+v, want 10250", got.TrailingHWM)
	}
	if got.UpdatedAt != 1700000050000 {
		t.Errorf("UpdatedAt = %v, want 1700000050000", got.UpdatedAt)
	}
}

func TestExitPlanRepo_UpdateTrailing_closedPlanFails(t *testing.T) {
	h := openExitPlanTestDB(t)
	ctx := context.Background()
	plan := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := h.repo.Create(ctx, plan); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.repo.Close(ctx, plan.ID, 1700000099999); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := h.repo.UpdateTrailing(ctx, plan.ID, 10300, true, 1700000100000); err == nil {
		t.Errorf("UpdateTrailing on closed plan should error")
	}
}

func TestExitPlanRepo_Close(t *testing.T) {
	h := openExitPlanTestDB(t)
	ctx := context.Background()
	plan := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := h.repo.Create(ctx, plan); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.repo.Close(ctx, plan.ID, 1700000099999); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, _ := h.repo.FindByPositionID(ctx, 100)
	if got.ClosedAt == nil || *got.ClosedAt != 1700000099999 {
		t.Errorf("ClosedAt = %+v, want 1700000099999", got.ClosedAt)
	}
}

func TestExitPlanRepo_Close_doubleCloseFails(t *testing.T) {
	h := openExitPlanTestDB(t)
	ctx := context.Background()
	plan := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := h.repo.Create(ctx, plan); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.repo.Close(ctx, plan.ID, 1700000099999); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := h.repo.Close(ctx, plan.ID, 1700000099999); err == nil {
		t.Errorf("second Close should error")
	}
}

func mustExitPlanForRepo(t *testing.T, posID, symID int64, side entity.OrderSide, entry float64) *exitplan.ExitPlan {
	t.Helper()
	plan, err := exitplan.New(exitplan.NewInput{
		PositionID: posID,
		SymbolID:   symID,
		Side:       side,
		EntryPrice: entry,
		Policy: risk.RiskPolicy{
			StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
			TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
			Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
		},
		CreatedAt: 1700000000000,
	})
	if err != nil {
		t.Fatalf("exitplan.New: %v", err)
	}
	return plan
}
