package backtest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestWalkForwardRunner_PicksBestISAndScoresOOS(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC)
	windows, err := ComputeWindows(from, to, 3, 3, 3)
	if err != nil {
		t.Fatalf("windows: %v", err)
	}
	grid, err := ExpandGrid([]ParameterOverride{
		{Path: "strategy_risk.stop_loss_percent", Values: []float64{3, 5, 7}},
	})
	if err != nil {
		t.Fatalf("grid: %v", err)
	}

	base := entity.StrategyProfile{Name: "base"}
	var isCalls atomic.Int32
	var oosCalls atomic.Int32
	run := NewWalkForwardRunner()
	run.Now = func() time.Time { return time.Unix(1700000000, 0) }

	out, err := run.Run(context.Background(), WalkForwardInput{
		BaseProfile: base,
		Windows:     windows,
		Grid:        grid,
		Objective:   "return",
		RunWindow: func(ctx context.Context, phase WalkForwardPhase, p entity.StrategyProfile, wf, wt time.Time) (*entity.BacktestResult, error) {
			slp := p.Risk.StopLossPercent
			switch phase {
			case WalkForwardPhaseInSample:
				isCalls.Add(1)
			case WalkForwardPhaseOutOfSample:
				oosCalls.Add(1)
			}
			return &entity.BacktestResult{
				ID: fmt.Sprintf("bt-%s-%.0f-%d", phase, slp, wf.Unix()),
				Summary: entity.BacktestSummary{
					TotalReturn: slp / 100.0,
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == nil {
		t.Fatalf("nil result")
	}

	if got := isCalls.Load(); got != 6 {
		t.Fatalf("IS calls = %d, want 6", got)
	}
	if got := oosCalls.Load(); got != 2 {
		t.Fatalf("OOS calls = %d, want 2", got)
	}

	if len(out.Windows) != 2 {
		t.Fatalf("windows len = %d, want 2", len(out.Windows))
	}
	for i, wr := range out.Windows {
		if wr.BestParameters["strategy_risk.stop_loss_percent"] != 7 {
			t.Fatalf("window %d best != 7: %+v", i, wr.BestParameters)
		}
		if wr.OOSResult.Summary.TotalReturn != 0.07 {
			t.Fatalf("window %d OOS return = %v, want 0.07", i, wr.OOSResult.Summary.TotalReturn)
		}
		if len(wr.ISResults) != 3 {
			t.Fatalf("window %d ISResults len = %d, want 3", i, len(wr.ISResults))
		}
	}

	if diff := out.AggregateOOS.GeomMeanReturn - 0.07; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("aggregate geomMean = %v, want 0.07", out.AggregateOOS.GeomMeanReturn)
	}
	if out.AggregateOOS.ReturnStdDev != 0 {
		t.Fatalf("aggregate stdDev = %v, want 0", out.AggregateOOS.ReturnStdDev)
	}
	if !out.AggregateOOS.AllPositive {
		t.Fatalf("aggregate AllPositive should be true")
	}
	if out.CreatedAt != 1700000000 {
		t.Fatalf("CreatedAt injection failed: %d", out.CreatedAt)
	}
}

func TestWalkForwardRunner_RejectsEmptyWindows(t *testing.T) {
	run := NewWalkForwardRunner()
	_, err := run.Run(context.Background(), WalkForwardInput{
		Grid: []map[string]float64{{}},
		RunWindow: func(context.Context, WalkForwardPhase, entity.StrategyProfile, time.Time, time.Time) (*entity.BacktestResult, error) {
			return nil, nil
		},
	})
	if err == nil {
		t.Fatalf("expected error on empty windows")
	}
}

func TestWalkForwardRunner_RejectsEmptyGrid(t *testing.T) {
	run := NewWalkForwardRunner()
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
	ws, _ := ComputeWindows(from, to, 3, 3, 3)
	_, err := run.Run(context.Background(), WalkForwardInput{
		Windows: ws,
		Grid:    nil,
		RunWindow: func(context.Context, WalkForwardPhase, entity.StrategyProfile, time.Time, time.Time) (*entity.BacktestResult, error) {
			return nil, nil
		},
	})
	if err == nil {
		t.Fatalf("expected error on empty grid")
	}
}

func TestWalkForwardRunner_PropagatesRunWindowError(t *testing.T) {
	run := NewWalkForwardRunner()
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC)
	ws, _ := ComputeWindows(from, to, 3, 3, 3)
	grid := []map[string]float64{{}}
	_, err := run.Run(context.Background(), WalkForwardInput{
		Windows: ws,
		Grid:    grid,
		RunWindow: func(context.Context, WalkForwardPhase, entity.StrategyProfile, time.Time, time.Time) (*entity.BacktestResult, error) {
			return nil, fmt.Errorf("boom")
		},
	})
	if err == nil {
		t.Fatalf("expected error propagated from RunWindow")
	}
}
