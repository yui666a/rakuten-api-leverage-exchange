package database

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

func newTestClientOrderRepo(t *testing.T) *ClientOrderRepo {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return NewClientOrderRepo(db)
}

func TestClientOrderRepo_InsertOrGet_Insert(t *testing.T) {
	repo := newTestClientOrderRepo(t)
	ctx := context.Background()

	now := time.Now().Unix()
	rec := repository.ClientOrderRecord{
		ClientOrderID: "co-1",
		Status:        entity.ClientOrderStatusPending,
		SymbolID:      7,
		Intent:        entity.ClientOrderIntentOpen,
		Side:          entity.OrderSideBuy,
		Amount:        0.001,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	stored, inserted, err := repo.InsertOrGet(ctx, rec)
	if err != nil {
		t.Fatalf("InsertOrGet: %v", err)
	}
	if !inserted {
		t.Fatal("expected inserted=true on first insert")
	}
	if stored == nil || stored.ClientOrderID != "co-1" {
		t.Fatalf("unexpected stored record: %+v", stored)
	}
	if stored.Status != entity.ClientOrderStatusPending {
		t.Fatalf("expected pending, got %s", stored.Status)
	}
	if stored.SymbolID != 7 || stored.Side != entity.OrderSideBuy || stored.Amount != 0.001 {
		t.Fatalf("metadata mismatch: %+v", stored)
	}
	if stored.Executed {
		t.Fatal("pending should map to executed=false")
	}
}

func TestClientOrderRepo_InsertOrGet_ReturnsExistingOnConflict(t *testing.T) {
	repo := newTestClientOrderRepo(t)
	ctx := context.Background()

	now := time.Now().Unix()
	first := repository.ClientOrderRecord{
		ClientOrderID: "co-2",
		Status:        entity.ClientOrderStatusPending,
		SymbolID:      7,
		Intent:        entity.ClientOrderIntentOpen,
		Side:          entity.OrderSideBuy,
		Amount:        0.001,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, _, err := repo.InsertOrGet(ctx, first); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// 状態を confirmed に進めておく。
	orderID := int64(12345)
	if err := repo.UpdateStatus(ctx, "co-2", entity.ClientOrderStatusConfirmed, now+1, repository.ClientOrderUpdate{
		OrderID: &orderID,
	}); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	// 同じ clientOrderID で再度 InsertOrGet → 既存行 (confirmed) が返る。
	second := first
	second.SymbolID = 999 // 上書きされていないことを確認するためのダミー
	existing, inserted, err := repo.InsertOrGet(ctx, second)
	if err != nil {
		t.Fatalf("second InsertOrGet: %v", err)
	}
	if inserted {
		t.Fatal("expected inserted=false on conflict")
	}
	if existing == nil {
		t.Fatal("existing should not be nil")
	}
	if existing.Status != entity.ClientOrderStatusConfirmed {
		t.Fatalf("expected confirmed, got %s", existing.Status)
	}
	if existing.OrderID != orderID {
		t.Fatalf("expected orderID %d, got %d", orderID, existing.OrderID)
	}
	if existing.SymbolID != 7 {
		t.Fatalf("existing should preserve original symbolID 7, got %d", existing.SymbolID)
	}
	if !existing.Executed {
		t.Fatal("confirmed should map to executed=true")
	}
}

func TestClientOrderRepo_InsertOrGet_ConcurrentSingleInsert(t *testing.T) {
	repo := newTestClientOrderRepo(t)
	ctx := context.Background()
	const goroutines = 16

	now := time.Now().Unix()
	base := repository.ClientOrderRecord{
		ClientOrderID: "co-race",
		Status:        entity.ClientOrderStatusPending,
		SymbolID:      7,
		Intent:        entity.ClientOrderIntentOpen,
		Side:          entity.OrderSideBuy,
		Amount:        0.001,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	var wg sync.WaitGroup
	var insertedCount int32
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, inserted, err := repo.InsertOrGet(ctx, base)
			if err != nil {
				t.Errorf("InsertOrGet: %v", err)
				return
			}
			if inserted {
				atomic.AddInt32(&insertedCount, 1)
			}
		}()
	}
	wg.Wait()

	if insertedCount != 1 {
		t.Fatalf("expected exactly 1 insert across %d goroutines, got %d", goroutines, insertedCount)
	}
}

func TestClientOrderRepo_UpdateStatus_FieldsAndExecutedFlag(t *testing.T) {
	repo := newTestClientOrderRepo(t)
	ctx := context.Background()

	now := time.Now().Unix()
	rec := repository.ClientOrderRecord{
		ClientOrderID: "co-3",
		Status:        entity.ClientOrderStatusPending,
		SymbolID:      7,
		Intent:        entity.ClientOrderIntentOpen,
		Side:          entity.OrderSideSell,
		Amount:        0.5,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, _, err := repo.InsertOrGet(ctx, rec); err != nil {
		t.Fatalf("InsertOrGet: %v", err)
	}

	raw := `{"id":42}`
	errMsg := "parse failed"
	if err := repo.UpdateStatus(ctx, "co-3", entity.ClientOrderStatusSubmitted, now+10, repository.ClientOrderUpdate{
		RawResponse:  &raw,
		ErrorMessage: &errMsg,
	}); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := repo.Find(ctx, "co-3")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got == nil {
		t.Fatal("record should exist")
	}
	if got.Status != entity.ClientOrderStatusSubmitted {
		t.Fatalf("expected submitted, got %s", got.Status)
	}
	if got.RawResponse != raw || got.ErrorMessage != errMsg {
		t.Fatalf("fields not updated: %+v", got)
	}
	if got.UpdatedAt != now+10 {
		t.Fatalf("expected updated_at %d, got %d", now+10, got.UpdatedAt)
	}
	if got.Executed {
		t.Fatal("submitted should map to executed=false")
	}

	// confirmed に遷移すると executed=true になる。
	orderID := int64(7777)
	if err := repo.UpdateStatus(ctx, "co-3", entity.ClientOrderStatusReconciledConfirmed, now+20, repository.ClientOrderUpdate{
		OrderID: &orderID,
	}); err != nil {
		t.Fatalf("second UpdateStatus: %v", err)
	}
	got2, err := repo.Find(ctx, "co-3")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !got2.Executed {
		t.Fatal("reconciled-confirmed should map to executed=true")
	}
	if got2.OrderID != orderID {
		t.Fatalf("expected orderID %d, got %d", orderID, got2.OrderID)
	}
}

func TestClientOrderRepo_UpdateStatus_NoRow(t *testing.T) {
	repo := newTestClientOrderRepo(t)
	ctx := context.Background()
	err := repo.UpdateStatus(ctx, "missing", entity.ClientOrderStatusFailed, time.Now().Unix(), repository.ClientOrderUpdate{})
	if err == nil {
		t.Fatal("expected error for missing row")
	}
}

func TestClientOrderRepo_ListByStatus(t *testing.T) {
	repo := newTestClientOrderRepo(t)
	ctx := context.Background()
	now := time.Now().Unix()

	insert := func(id string, status entity.ClientOrderStatus, updatedAt int64) {
		t.Helper()
		rec := repository.ClientOrderRecord{
			ClientOrderID: id,
			Status:        status,
			SymbolID:      7,
			Intent:        entity.ClientOrderIntentOpen,
			Side:          entity.OrderSideBuy,
			Amount:        0.001,
			CreatedAt:     updatedAt,
			UpdatedAt:     updatedAt,
		}
		if _, _, err := repo.InsertOrGet(ctx, rec); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	insert("a", entity.ClientOrderStatusSubmitted, now+1)
	insert("b", entity.ClientOrderStatusPending, now+2)
	insert("c", entity.ClientOrderStatusCompleted, now+3)
	insert("d", entity.ClientOrderStatusSubmitted, now+4)

	out, err := repo.ListByStatus(ctx,
		[]entity.ClientOrderStatus{entity.ClientOrderStatusPending, entity.ClientOrderStatusSubmitted}, 10)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 records, got %d (%+v)", len(out), out)
	}
	// updated_at 昇順
	wantOrder := []string{"a", "b", "d"}
	for i, w := range wantOrder {
		if out[i].ClientOrderID != w {
			t.Fatalf("order mismatch at %d: want %s, got %s", i, w, out[i].ClientOrderID)
		}
	}
}

func TestClientOrderRepo_Save_BackwardCompatible(t *testing.T) {
	// 既存呼び出し元 (handler の Find→Save パターン) が壊れないことを確認する。
	repo := newTestClientOrderRepo(t)
	ctx := context.Background()
	now := time.Now().Unix()

	rec := repository.ClientOrderRecord{
		ClientOrderID: "co-legacy",
		Executed:      true,
		OrderID:       42,
		CreatedAt:     now,
	}
	if err := repo.Save(ctx, rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Find(ctx, "co-legacy")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got == nil || !got.Executed || got.OrderID != 42 {
		t.Fatalf("legacy save round-trip failed: %+v", got)
	}
	if got.Status != entity.ClientOrderStatusCompleted {
		t.Fatalf("legacy executed=true should map to completed, got %s", got.Status)
	}
}
