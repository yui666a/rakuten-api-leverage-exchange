package backtest

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
)

func newMultiPeriodTestRepo(t *testing.T) (*MultiPeriodResultRepository, *ResultRepository) {
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
	btRepo := NewResultRepository(db)
	return NewMultiPeriodResultRepository(db, btRepo), btRepo
}

func TestMultiPeriodRepository_SaveFindRehydratesPeriods(t *testing.T) {
	mpRepo, btRepo := newMultiPeriodTestRepo(t)
	ctx := context.Background()

	// Insert two per-period BacktestResults first.
	period1 := entity.BacktestResult{
		ID:        "bt-p1",
		CreatedAt: time.Now().Unix(),
		Config: entity.BacktestConfig{
			Symbol: "LTC_JPY", SymbolID: 10, PrimaryInterval: "PT15M",
			FromTimestamp: 1000, ToTimestamp: 2000,
		},
		Summary: entity.BacktestSummary{InitialBalance: 100000, FinalBalance: 110000, TotalReturn: 0.1},
	}
	period2 := entity.BacktestResult{
		ID:        "bt-p2",
		CreatedAt: time.Now().Unix(),
		Config: entity.BacktestConfig{
			Symbol: "LTC_JPY", SymbolID: 10, PrimaryInterval: "PT15M",
			FromTimestamp: 3000, ToTimestamp: 4000,
		},
		Summary: entity.BacktestSummary{InitialBalance: 100000, FinalBalance: 105000, TotalReturn: 0.05},
	}
	if err := btRepo.Save(ctx, period1); err != nil {
		t.Fatalf("save period1: %v", err)
	}
	if err := btRepo.Save(ctx, period2); err != nil {
		t.Fatalf("save period2: %v", err)
	}

	mp := entity.MultiPeriodResult{
		ID:          "mp-1",
		CreatedAt:   time.Now().Unix(),
		ProfileName: "production",
		PDCACycleID: "2026-05-01_cycle01",
		Hypothesis:  "1yr vs 2yr robustness check",
		Periods: []entity.LabeledBacktestResult{
			{Label: "1yr", Result: period1},
			{Label: "2yr", Result: period2},
		},
		Aggregate: entity.MultiPeriodAggregate{
			GeomMeanReturn:  0.074,
			ReturnStdDev:    0.025,
			WorstReturn:     0.05,
			BestReturn:      0.1,
			WorstDrawdown:   0.05,
			AllPositive:     true,
			RobustnessScore: 0.049,
		},
	}
	if err := mpRepo.Save(ctx, mp); err != nil {
		t.Fatalf("save mp: %v", err)
	}

	got, err := mpRepo.FindByID(ctx, "mp-1")
	if err != nil {
		t.Fatalf("find mp: %v", err)
	}
	if got == nil {
		t.Fatalf("expected result, got nil")
	}

	if got.ProfileName != "production" {
		t.Fatalf("ProfileName = %q", got.ProfileName)
	}
	if got.PDCACycleID != "2026-05-01_cycle01" {
		t.Fatalf("PDCACycleID = %q", got.PDCACycleID)
	}
	if got.Aggregate.GeomMeanReturn != 0.074 {
		t.Fatalf("Aggregate round-trip failed: %+v", got.Aggregate)
	}
	if len(got.Periods) != 2 {
		t.Fatalf("Periods len = %d, want 2", len(got.Periods))
	}
	// Period content rehydrated from backtest_results.
	if got.Periods[0].Label != "1yr" || got.Periods[0].Result.ID != "bt-p1" {
		t.Fatalf("period 0 mismatch: %+v", got.Periods[0])
	}
	if got.Periods[1].Result.Summary.TotalReturn != 0.05 {
		t.Fatalf("period 1 TotalReturn round-trip failed: %v", got.Periods[1].Result.Summary.TotalReturn)
	}
}

func TestMultiPeriodRepository_SaveAndRoundTripRuinAggregate(t *testing.T) {
	// Codex PR #111 BLOCKING: Save must not fail when the aggregate contains
	// NaN (the ruin signal). This regression test persists a ruined envelope
	// and asserts it comes back with the NaN preserved via the JSON null
	// round-trip on MultiPeriodAggregate.
	mpRepo, btRepo := newMultiPeriodTestRepo(t)
	ctx := context.Background()

	bt := entity.BacktestResult{
		ID:        "bt-ruin",
		CreatedAt: time.Now().Unix(),
		Config: entity.BacktestConfig{
			Symbol: "LTC_JPY", SymbolID: 10, PrimaryInterval: "PT15M",
			FromTimestamp: 1000, ToTimestamp: 2000,
		},
		Summary: entity.BacktestSummary{InitialBalance: 100000, FinalBalance: 0, TotalReturn: -1.0},
	}
	if err := btRepo.Save(ctx, bt); err != nil {
		t.Fatalf("save bt: %v", err)
	}

	ruin := entity.MultiPeriodResult{
		ID:        "mp-ruin",
		CreatedAt: time.Now().Unix(),
		Periods:   []entity.LabeledBacktestResult{{Label: "ruin-window", Result: bt}},
		Aggregate: entity.MultiPeriodAggregate{
			GeomMeanReturn:  math.NaN(),
			ReturnStdDev:    0.01,
			WorstReturn:     -1.0,
			BestReturn:      -1.0,
			WorstDrawdown:   1.0,
			AllPositive:     false,
			RobustnessScore: math.NaN(),
		},
	}
	if err := mpRepo.Save(ctx, ruin); err != nil {
		t.Fatalf("save ruin envelope should succeed, got: %v", err)
	}

	got, err := mpRepo.FindByID(ctx, "mp-ruin")
	if err != nil {
		t.Fatalf("find ruin: %v", err)
	}
	if got == nil {
		t.Fatalf("expected ruin envelope")
	}
	if !math.IsNaN(got.Aggregate.GeomMeanReturn) || !math.IsNaN(got.Aggregate.RobustnessScore) {
		t.Fatalf("NaN should survive persistence round-trip: %+v", got.Aggregate)
	}
	if got.Aggregate.AllPositive {
		t.Fatalf("ruin aggregate must not be AllPositive")
	}
}

func TestMultiPeriodRepository_FindByIDMissing(t *testing.T) {
	mpRepo, _ := newMultiPeriodTestRepo(t)
	got, err := mpRepo.FindByID(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestMultiPeriodRepository_ListFilters(t *testing.T) {
	mpRepo, btRepo := newMultiPeriodTestRepo(t)
	ctx := context.Background()

	// Seed: 3 multi-period runs across 2 profiles / 2 cycles.
	seed := []struct {
		ID       string
		Profile  string
		Cycle    string
		ResultID string
	}{
		{"mp-a", "production", "cycle01", "bt-a"},
		{"mp-b", "production", "cycle02", "bt-b"},
		{"mp-c", "experiment_x", "cycle01", "bt-c"},
	}
	for _, s := range seed {
		if err := btRepo.Save(ctx, entity.BacktestResult{
			ID:        s.ResultID,
			CreatedAt: time.Now().Unix(),
			Config: entity.BacktestConfig{
				Symbol: "LTC_JPY", SymbolID: 10, PrimaryInterval: "PT15M",
				FromTimestamp: 1000, ToTimestamp: 2000,
			},
			Summary: entity.BacktestSummary{InitialBalance: 100000, FinalBalance: 101000},
		}); err != nil {
			t.Fatalf("save bt %s: %v", s.ResultID, err)
		}
		if err := mpRepo.Save(ctx, entity.MultiPeriodResult{
			ID:          s.ID,
			CreatedAt:   time.Now().Unix(),
			ProfileName: s.Profile,
			PDCACycleID: s.Cycle,
			Periods: []entity.LabeledBacktestResult{
				{Label: "1yr", Result: entity.BacktestResult{ID: s.ResultID}},
			},
			Aggregate: entity.MultiPeriodAggregate{GeomMeanReturn: 0.01, AllPositive: true},
		}); err != nil {
			t.Fatalf("save mp %s: %v", s.ID, err)
		}
	}

	// Filter by profile.
	list, err := mpRepo.List(ctx, repository.MultiPeriodResultFilter{ProfileName: "production"})
	if err != nil {
		t.Fatalf("list by profile: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("profile filter: got %d, want 2", len(list))
	}

	// Filter by cycle.
	list, err = mpRepo.List(ctx, repository.MultiPeriodResultFilter{PDCACycleID: "cycle01"})
	if err != nil {
		t.Fatalf("list by cycle: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("cycle filter: got %d, want 2", len(list))
	}

	// No filter returns all.
	list, err = mpRepo.List(ctx, repository.MultiPeriodResultFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("no filter: got %d, want 3", len(list))
	}
}
