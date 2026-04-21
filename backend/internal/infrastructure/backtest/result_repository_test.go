package backtest

import (
	"context"
	"errors"
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

// TestResultRepository_BreakdownRoundTrip confirms that the per-exit-reason
// and per-signal-source breakdown maps are serialised to breakdown_json on
// Save and restored on Find/List.
//
// This test is the primary guard against cross-wire bugs between
// SummaryReporter → entity.BacktestSummary → DB column. If the breakdown
// maps are silently dropped at the persistence boundary, Frontend / API
// consumers see empty breakdowns regardless of what the reporter produced.
func TestResultRepository_BreakdownRoundTrip(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	result := entity.BacktestResult{
		ID:        "bt-breakdown-1",
		CreatedAt: time.Now().Unix(),
		Config: entity.BacktestConfig{
			Symbol:          "LTC_JPY",
			SymbolID:        10,
			PrimaryInterval: "PT15M",
			FromTimestamp:   1000,
			ToTimestamp:     2000,
		},
		Summary: entity.BacktestSummary{
			InitialBalance: 100000,
			FinalBalance:   105000,
			TotalTrades:    3,
			ByExitReason: map[string]entity.SummaryBreakdown{
				"take_profit": {Trades: 2, WinTrades: 2, WinRate: 100, TotalPnL: 70, AvgPnL: 35, ProfitFactor: 0},
				"stop_loss":   {Trades: 1, LossTrades: 1, TotalPnL: -20, AvgPnL: -20},
			},
			BySignalSource: map[string]entity.SummaryBreakdown{
				"trend_follow": {Trades: 2, WinTrades: 1, LossTrades: 1, WinRate: 50, TotalPnL: 30, AvgPnL: 15, ProfitFactor: 2.5},
				"contrarian":   {Trades: 1, WinTrades: 1, WinRate: 100, TotalPnL: 20, AvgPnL: 20},
			},
		},
	}
	if err := repo.Save(ctx, result); err != nil {
		t.Fatalf("save: %v", err)
	}

	found, err := repo.FindByID(ctx, result.ID)
	if err != nil || found == nil {
		t.Fatalf("find: err=%v found=%v", err, found)
	}
	if len(found.Summary.ByExitReason) != 2 {
		t.Fatalf("ByExitReason len=%d, want 2", len(found.Summary.ByExitReason))
	}
	if found.Summary.ByExitReason["take_profit"].Trades != 2 {
		t.Fatalf("take_profit bucket round-trip failed: %+v", found.Summary.ByExitReason["take_profit"])
	}
	if found.Summary.ByExitReason["take_profit"].TotalPnL != 70 {
		t.Fatalf("take_profit TotalPnL round-trip failed: %v", found.Summary.ByExitReason["take_profit"].TotalPnL)
	}
	if len(found.Summary.BySignalSource) != 2 {
		t.Fatalf("BySignalSource len=%d, want 2", len(found.Summary.BySignalSource))
	}
	if found.Summary.BySignalSource["trend_follow"].ProfitFactor != 2.5 {
		t.Fatalf("trend_follow PF round-trip failed: %v", found.Summary.BySignalSource["trend_follow"].ProfitFactor)
	}
}

// TestResultRepository_LegacyRowWithoutBreakdown confirms backward
// compatibility: rows persisted before PR-1 (breakdown_json = NULL) must
// still load successfully with empty breakdown maps on the Summary.
func TestResultRepository_LegacyRowWithoutBreakdown(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	result := entity.BacktestResult{
		ID:        "bt-legacy-1",
		CreatedAt: time.Now().Unix(),
		Config: entity.BacktestConfig{
			Symbol:          "LTC_JPY",
			SymbolID:        10,
			PrimaryInterval: "PT15M",
			FromTimestamp:   1000,
			ToTimestamp:     2000,
		},
		Summary: entity.BacktestSummary{
			InitialBalance: 100000,
			FinalBalance:   100000,
			// No ByExitReason / BySignalSource — simulates a legacy row.
		},
	}
	if err := repo.Save(ctx, result); err != nil {
		t.Fatalf("save: %v", err)
	}

	found, err := repo.FindByID(ctx, result.ID)
	if err != nil || found == nil {
		t.Fatalf("find: err=%v found=%v", err, found)
	}
	if len(found.Summary.ByExitReason) != 0 {
		t.Fatalf("expected empty ByExitReason for legacy row, got %v", found.Summary.ByExitReason)
	}
	if len(found.Summary.BySignalSource) != 0 {
		t.Fatalf("expected empty BySignalSource for legacy row, got %v", found.Summary.BySignalSource)
	}
}

// newTestRepo opens a fresh in-memory-backed SQLite DB in a temp directory,
// runs migrations, and returns a ResultRepository ready for use.
func newTestRepo(t *testing.T) *ResultRepository {
	t.Helper()
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewResultRepository(db)
}

// basicResult returns a minimally-populated BacktestResult for persistence tests.
func basicResult(id string, createdAt int64) entity.BacktestResult {
	return entity.BacktestResult{
		ID:        id,
		CreatedAt: createdAt,
		Config: entity.BacktestConfig{
			Symbol:          "BTC_JPY",
			SymbolID:        7,
			PrimaryInterval: "PT15M",
			FromTimestamp:   1000,
			ToTimestamp:     2000,
		},
		Summary: entity.BacktestSummary{
			InitialBalance: 100000,
			FinalBalance:   110000,
			WinRate:        50,
		},
	}
}

func TestResultRepository_SaveRejectsSelfReference(t *testing.T) {
	repo := newTestRepo(t)
	id := "bt-self-ref"
	self := id
	r := basicResult(id, time.Now().Unix())
	r.ParentResultID = &self

	err := repo.Save(context.Background(), r)
	if err == nil {
		t.Fatal("expected error for self-reference, got nil")
	}
	if !errors.Is(err, repository.ErrParentResultSelfReference) {
		t.Fatalf("expected ErrParentResultSelfReference, got %v", err)
	}

	// Rollback integrity: the rejected row must not have been persisted.
	found, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("find after self-ref rejection: %v", err)
	}
	if found != nil {
		t.Fatalf("expected no row persisted after self-ref rejection, got %+v", found)
	}
}

func TestResultRepository_SaveRejectsMissingParent(t *testing.T) {
	repo := newTestRepo(t)
	missing := "does-not-exist"
	id := "bt-orphan"
	r := basicResult(id, time.Now().Unix())
	r.ParentResultID = &missing

	err := repo.Save(context.Background(), r)
	if err == nil {
		t.Fatal("expected error for missing parent, got nil")
	}
	if !errors.Is(err, repository.ErrParentResultNotFound) {
		t.Fatalf("expected ErrParentResultNotFound, got %v", err)
	}

	// Rollback integrity: the rejected row must not have been persisted.
	found, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("find after missing-parent rejection: %v", err)
	}
	if found != nil {
		t.Fatalf("expected no row persisted after missing-parent rejection, got %+v", found)
	}
}

func TestResultRepository_SaveValidParent(t *testing.T) {
	repo := newTestRepo(t)
	now := time.Now().Unix()

	parent := basicResult("bt-parent", now)
	if err := repo.Save(context.Background(), parent); err != nil {
		t.Fatalf("save parent: %v", err)
	}

	pid := parent.ID
	child := basicResult("bt-child", now+1)
	child.ParentResultID = &pid
	if err := repo.Save(context.Background(), child); err != nil {
		t.Fatalf("save valid child: %v", err)
	}

	found, err := repo.FindByID(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("find child: %v", err)
	}
	if found == nil || found.ParentResultID == nil || *found.ParentResultID != parent.ID {
		t.Fatalf("expected child.ParentResultID=%q, got %+v", parent.ID, found)
	}
}

func TestResultRepository_Filter(t *testing.T) {
	repo := newTestRepo(t)
	now := time.Now().Unix()
	ctx := context.Background()

	// r1: profile=prodA, cycle=c1, no parent.
	r1 := basicResult("bt-f1", now)
	r1.ProfileName = "prodA"
	r1.PDCACycleID = "c1"

	// r2: profile=prodB, cycle=c1, no parent.
	r2 := basicResult("bt-f2", now+1)
	r2.ProfileName = "prodB"
	r2.PDCACycleID = "c1"

	// r3: profile=prodA, cycle=c2, parent=r1.
	r3 := basicResult("bt-f3", now+2)
	r3.ProfileName = "prodA"
	r3.PDCACycleID = "c2"
	pid1 := r1.ID
	r3.ParentResultID = &pid1

	// r4: profile=prodB, cycle=c2, parent=r2.
	r4 := basicResult("bt-f4", now+3)
	r4.ProfileName = "prodB"
	r4.PDCACycleID = "c2"
	pid2 := r2.ID
	r4.ParentResultID = &pid2

	for _, r := range []entity.BacktestResult{r1, r2, r3, r4} {
		if err := repo.Save(ctx, r); err != nil {
			t.Fatalf("save %s: %v", r.ID, err)
		}
	}

	collectIDs := func(results []entity.BacktestResult) map[string]struct{} {
		m := make(map[string]struct{}, len(results))
		for _, r := range results {
			m[r.ID] = struct{}{}
		}
		return m
	}

	t.Run("ProfileName", func(t *testing.T) {
		got, err := repo.List(ctx, repository.BacktestResultFilter{
			Limit:       10,
			ProfileName: "prodA",
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		ids := collectIDs(got)
		if len(ids) != 2 {
			t.Fatalf("expected 2 rows, got %d: %v", len(ids), ids)
		}
		if _, ok := ids[r1.ID]; !ok {
			t.Fatalf("r1 missing from ProfileName=prodA result: %v", ids)
		}
		if _, ok := ids[r3.ID]; !ok {
			t.Fatalf("r3 missing from ProfileName=prodA result: %v", ids)
		}
	})

	t.Run("PDCACycleID", func(t *testing.T) {
		got, err := repo.List(ctx, repository.BacktestResultFilter{
			Limit:       10,
			PDCACycleID: "c2",
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		ids := collectIDs(got)
		if len(ids) != 2 {
			t.Fatalf("expected 2 rows, got %d: %v", len(ids), ids)
		}
		if _, ok := ids[r3.ID]; !ok {
			t.Fatalf("r3 missing from PDCACycleID=c2 result: %v", ids)
		}
		if _, ok := ids[r4.ID]; !ok {
			t.Fatalf("r4 missing from PDCACycleID=c2 result: %v", ids)
		}
	})

	t.Run("HasParentTrue", func(t *testing.T) {
		yes := true
		got, err := repo.List(ctx, repository.BacktestResultFilter{
			Limit:     10,
			HasParent: &yes,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		ids := collectIDs(got)
		if len(ids) != 2 {
			t.Fatalf("expected 2 rows (children only), got %d: %v", len(ids), ids)
		}
		if _, ok := ids[r3.ID]; !ok {
			t.Fatalf("r3 missing from HasParent=true: %v", ids)
		}
		if _, ok := ids[r4.ID]; !ok {
			t.Fatalf("r4 missing from HasParent=true: %v", ids)
		}
	})

	t.Run("HasParentFalse", func(t *testing.T) {
		no := false
		got, err := repo.List(ctx, repository.BacktestResultFilter{
			Limit:     10,
			HasParent: &no,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		ids := collectIDs(got)
		if len(ids) != 2 {
			t.Fatalf("expected 2 rows (roots only), got %d: %v", len(ids), ids)
		}
		if _, ok := ids[r1.ID]; !ok {
			t.Fatalf("r1 missing from HasParent=false: %v", ids)
		}
		if _, ok := ids[r2.ID]; !ok {
			t.Fatalf("r2 missing from HasParent=false: %v", ids)
		}
	})

	t.Run("ParentResultIDExactMatch", func(t *testing.T) {
		pid := r1.ID
		got, err := repo.List(ctx, repository.BacktestResultFilter{
			Limit:          10,
			ParentResultID: &pid,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		ids := collectIDs(got)
		if len(ids) != 1 {
			t.Fatalf("expected 1 row, got %d: %v", len(ids), ids)
		}
		if _, ok := ids[r3.ID]; !ok {
			t.Fatalf("r3 missing from ParentResultID=r1 result: %v", ids)
		}
	})

	t.Run("ProfileNameAndPDCACycleIDCombined", func(t *testing.T) {
		// AND semantics: only rows matching BOTH ProfileName=prodA and
		// PDCACycleID=c1 should be returned. r3 matches profile but not cycle;
		// r2 matches cycle but not profile; only r1 matches both.
		got, err := repo.List(ctx, repository.BacktestResultFilter{
			Limit:       10,
			ProfileName: "prodA",
			PDCACycleID: "c1",
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		ids := collectIDs(got)
		if len(ids) != 1 {
			t.Fatalf("expected 1 row (AND match), got %d: %v", len(ids), ids)
		}
		if _, ok := ids[r1.ID]; !ok {
			t.Fatalf("r1 missing from ProfileName=prodA AND PDCACycleID=c1 result: %v", ids)
		}
	})

	t.Run("ParentResultIDWinsOverHasParent", func(t *testing.T) {
		// Spec §5.3: ParentResultID takes precedence over HasParent.
		// If HasParent=false (which would exclude rows with a parent) were
		// honored, we'd get zero rows; ParentResultID=r2 must win and return r4.
		pid := r2.ID
		no := false
		got, err := repo.List(ctx, repository.BacktestResultFilter{
			Limit:          10,
			ParentResultID: &pid,
			HasParent:      &no,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		ids := collectIDs(got)
		if len(ids) != 1 {
			t.Fatalf("expected 1 row (ParentResultID wins), got %d: %v", len(ids), ids)
		}
		if _, ok := ids[r4.ID]; !ok {
			t.Fatalf("r4 missing from ParentResultID=r2 (HasParent should be ignored): %v", ids)
		}
	})
}
