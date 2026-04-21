package backtest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
)

func newTestDB(t *testing.T) *WalkForwardResultRepository {
	t.Helper()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "wf.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewWalkForwardResultRepository(db)
}

func TestWalkForwardResultRepository_SaveFindByIDRoundTrip(t *testing.T) {
	repo := newTestDB(t)
	ctx := context.Background()

	parent := "parent-id"
	rec := entity.WalkForwardPersisted{
		ID:               "wf-001",
		CreatedAt:        1700000000,
		BaseProfile:      "production",
		Objective:        "return",
		PDCACycleID:      "cycle22",
		Hypothesis:       "grid over stoch_entry_max",
		ParentResultID:   &parent,
		RequestJSON:      `{"from":"2023-01-01","to":"2024-01-01"}`,
		ResultJSON:       `{"id":"wf-001","windows":[]}`,
		AggregateOOSJSON: `{"robustnessScore":0.42}`,
	}
	if err := repo.Save(ctx, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.FindByID(ctx, "wf-001")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil {
		t.Fatal("find returned nil")
	}
	if got.ID != rec.ID || got.BaseProfile != rec.BaseProfile ||
		got.RequestJSON != rec.RequestJSON || got.ResultJSON != rec.ResultJSON ||
		got.AggregateOOSJSON != rec.AggregateOOSJSON ||
		got.Objective != rec.Objective || got.PDCACycleID != rec.PDCACycleID ||
		got.Hypothesis != rec.Hypothesis {
		t.Fatalf("round-trip mismatch: got %+v", got)
	}
	if got.ParentResultID == nil || *got.ParentResultID != parent {
		t.Fatalf("parent mismatch: got %v", got.ParentResultID)
	}
}

func TestWalkForwardResultRepository_FindByIDMissingReturnsNil(t *testing.T) {
	repo := newTestDB(t)
	got, err := repo.FindByID(context.Background(), "nope")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing id, got %+v", got)
	}
}

func TestWalkForwardResultRepository_ListFiltersAndOrders(t *testing.T) {
	repo := newTestDB(t)
	ctx := context.Background()

	records := []entity.WalkForwardPersisted{
		{ID: "a", CreatedAt: 100, BaseProfile: "production", PDCACycleID: "cycle01",
			RequestJSON: "{}", ResultJSON: "{}", AggregateOOSJSON: "{}"},
		{ID: "b", CreatedAt: 200, BaseProfile: "experimental", PDCACycleID: "cycle01",
			RequestJSON: "{}", ResultJSON: "{}", AggregateOOSJSON: "{}"},
		{ID: "c", CreatedAt: 300, BaseProfile: "production", PDCACycleID: "cycle02",
			RequestJSON: "{}", ResultJSON: "{}", AggregateOOSJSON: "{}"},
	}
	for _, r := range records {
		if err := repo.Save(ctx, r); err != nil {
			t.Fatalf("save %s: %v", r.ID, err)
		}
	}

	all, err := repo.List(ctx, repository.WalkForwardResultFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(all))
	}
	// ORDER BY created_at DESC -> c, b, a.
	if all[0].ID != "c" || all[1].ID != "b" || all[2].ID != "a" {
		t.Fatalf("wrong order: %s,%s,%s", all[0].ID, all[1].ID, all[2].ID)
	}

	prod, err := repo.List(ctx, repository.WalkForwardResultFilter{BaseProfile: "production"})
	if err != nil {
		t.Fatalf("list prod: %v", err)
	}
	if len(prod) != 2 {
		t.Fatalf("expected 2 production rows, got %d", len(prod))
	}

	cyc1, err := repo.List(ctx, repository.WalkForwardResultFilter{PDCACycleID: "cycle01"})
	if err != nil {
		t.Fatalf("list cycle01: %v", err)
	}
	if len(cyc1) != 2 {
		t.Fatalf("expected 2 cycle01 rows, got %d", len(cyc1))
	}

	// Combined filter
	both, err := repo.List(ctx, repository.WalkForwardResultFilter{BaseProfile: "production", PDCACycleID: "cycle01"})
	if err != nil {
		t.Fatalf("list both: %v", err)
	}
	if len(both) != 1 || both[0].ID != "a" {
		t.Fatalf("expected [a], got %+v", both)
	}

	// List must not expose result_json (empty because the SELECT omits it)
	if all[0].ResultJSON != "" {
		t.Fatalf("list should not return result_json; got %q", all[0].ResultJSON)
	}
}

func TestWalkForwardResultRepository_SaveRejectsEmptyID(t *testing.T) {
	repo := newTestDB(t)
	err := repo.Save(context.Background(), entity.WalkForwardPersisted{
		ResultJSON: "{}", AggregateOOSJSON: "{}",
	})
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}
