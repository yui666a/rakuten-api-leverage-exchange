package backtest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
)

func TestResultRepository_SaveListFindDelete(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	defer db.Close()
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewResultRepository(db)
	now := time.Now().Unix()

	result := entity.BacktestResult{
		ID:        "bt-test-1",
		CreatedAt: now,
		Config: entity.BacktestConfig{
			Symbol:           "BTC_JPY",
			SymbolID:         7,
			PrimaryInterval:  "PT15M",
			HigherTFInterval: "PT1H",
			FromTimestamp:    1000,
			ToTimestamp:      2000,
		},
		Summary: entity.BacktestSummary{
			InitialBalance:     100000,
			FinalBalance:       110000,
			TotalReturn:        0.1,
			TotalTrades:        1,
			WinTrades:          1,
			LossTrades:         0,
			WinRate:            100,
			ProfitFactor:       2,
			MaxDrawdown:        0.05,
			MaxDrawdownBalance: 95000,
			SharpeRatio:        1.2,
			AvgHoldSeconds:     3600,
			TotalCarryingCost:  100,
			TotalSpreadCost:    50,
		},
		Trades: []entity.BacktestTradeRecord{
			{
				TradeID:      1,
				SymbolID:     7,
				EntryTime:    1100,
				ExitTime:     1500,
				Side:         "BUY",
				EntryPrice:   100,
				ExitPrice:    110,
				Amount:       0.01,
				PnL:          10,
				PnLPercent:   10,
				CarryingCost: 1,
				SpreadCost:   0.5,
				ReasonEntry:  "entry",
				ReasonExit:   "exit",
			},
		},
	}
	if err := repo.Save(context.Background(), result); err != nil {
		t.Fatalf("save: %v", err)
	}

	list, err := repo.List(context.Background(), repository.BacktestResultFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 result, got %d", len(list))
	}
	if list[0].ID != result.ID {
		t.Fatalf("unexpected list id: %s", list[0].ID)
	}

	found, err := repo.FindByID(context.Background(), result.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil {
		t.Fatal("expected found result")
	}
	if len(found.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(found.Trades))
	}

	deleted, err := repo.DeleteOlderThan(context.Background(), now+1)
	if err != nil {
		t.Fatalf("delete older than: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted row, got %d", deleted)
	}
}

func TestResultRepository_PDCAFieldsRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	defer db.Close()
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewResultRepository(db)
	now := time.Now().Unix()

	parentID := "bt-parent-01"
	parentResult := entity.BacktestResult{
		ID:        parentID,
		CreatedAt: now,
		Config: entity.BacktestConfig{
			Symbol:          "BTC_JPY",
			SymbolID:        7,
			PrimaryInterval: "PT15M",
			FromTimestamp:   1000,
			ToTimestamp:     2000,
		},
		Summary: entity.BacktestSummary{
			InitialBalance:  100000,
			FinalBalance:    110000,
			WinRate:         100,
			BiweeklyWinRate: 72.5,
		},
		ProfileName: "production_v1",
		PDCACycleID: "2026-04-16_cycle01",
		Hypothesis:  "baseline before tuning",
		// ParentResultID: nil (root)
	}
	if err := repo.Save(context.Background(), parentResult); err != nil {
		t.Fatalf("save parent: %v", err)
	}

	childID := "bt-child-01"
	pid := parentID
	childResult := entity.BacktestResult{
		ID:        childID,
		CreatedAt: now + 1,
		Config: entity.BacktestConfig{
			Symbol:          "BTC_JPY",
			SymbolID:        7,
			PrimaryInterval: "PT15M",
			FromTimestamp:   3000,
			ToTimestamp:     4000,
		},
		Summary: entity.BacktestSummary{
			InitialBalance:  100000,
			FinalBalance:    120000,
			WinRate:         80,
			BiweeklyWinRate: 81.25,
		},
		ProfileName:    "experiment_v1",
		PDCACycleID:    "2026-04-16_cycle02",
		Hypothesis:     "ATR stop helps",
		ParentResultID: &pid,
	}
	if err := repo.Save(context.Background(), childResult); err != nil {
		t.Fatalf("save child: %v", err)
	}

	// List returns both rows with PDCA fields intact.
	list, err := repo.List(context.Background(), repository.BacktestResultFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 results, got %d", len(list))
	}
	byID := map[string]entity.BacktestResult{}
	for _, r := range list {
		byID[r.ID] = r
	}
	gotParent, ok := byID[parentID]
	if !ok {
		t.Fatalf("parent result missing from list")
	}
	if gotParent.ProfileName != "production_v1" ||
		gotParent.PDCACycleID != "2026-04-16_cycle01" ||
		gotParent.Hypothesis != "baseline before tuning" {
		t.Fatalf("parent PDCA metadata mismatch: %+v", gotParent)
	}
	if gotParent.ParentResultID != nil {
		t.Fatalf("parent ParentResultID expected nil, got %v", *gotParent.ParentResultID)
	}
	if gotParent.Summary.BiweeklyWinRate != 72.5 {
		t.Fatalf("parent biweekly win rate mismatch: %v", gotParent.Summary.BiweeklyWinRate)
	}

	gotChild, ok := byID[childID]
	if !ok {
		t.Fatalf("child result missing from list")
	}
	if gotChild.ParentResultID == nil {
		t.Fatalf("child ParentResultID expected non-nil")
	}
	if *gotChild.ParentResultID != parentID {
		t.Fatalf("child ParentResultID expected %q, got %q", parentID, *gotChild.ParentResultID)
	}
	if gotChild.Summary.BiweeklyWinRate != 81.25 {
		t.Fatalf("child biweekly win rate mismatch: %v", gotChild.Summary.BiweeklyWinRate)
	}

	// FindByID returns the PDCA fields.
	found, err := repo.FindByID(context.Background(), childID)
	if err != nil {
		t.Fatalf("find child: %v", err)
	}
	if found == nil || found.ParentResultID == nil || *found.ParentResultID != parentID {
		t.Fatalf("find child PDCA mismatch: %+v", found)
	}

	// FK ON DELETE SET NULL: deleting the parent should null out the child's ParentResultID.
	// DeleteOlderThan deletes through the same sql.DB where PRAGMA foreign_keys=ON was set
	// by database.NewDB, so the trigger fires here.
	if _, err := db.ExecContext(context.Background(), "DELETE FROM backtest_results WHERE id = ?", parentID); err != nil {
		t.Fatalf("delete parent: %v", err)
	}
	afterDelete, err := repo.FindByID(context.Background(), childID)
	if err != nil {
		t.Fatalf("find child after parent delete: %v", err)
	}
	if afterDelete == nil {
		t.Fatalf("child should still exist after parent delete")
	}
	if afterDelete.ParentResultID != nil {
		t.Fatalf("expected child ParentResultID to be nil after parent delete, got %q", *afterDelete.ParentResultID)
	}
}
