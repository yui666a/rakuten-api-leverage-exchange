package backtest

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// MultiPeriodInput is the fan-out request to MultiPeriodRunner.Run. The
// RunInputForPeriod callback lets the handler build a per-period RunInput
// without this usecase needing to know about CSVs, profiles, or risk config
// details. Each call should return a fully-formed RunInput for that period
// (Config.From/To, indicator data, runner instance wiring, etc.).
type MultiPeriodInput struct {
	ProfileName       string
	PDCACycleID       string
	Hypothesis        string
	ParentResultID    *string
	Periods           []entity.PeriodSpec
	RunInputForPeriod func(period entity.PeriodSpec) (*BacktestRunner, RunInput, error)
}

// MultiPeriodRunner coordinates N per-period backtest runs, executed in
// parallel up to a configurable limit, and folds them into a single
// MultiPeriodResult envelope. Per-period BacktestResult IDs are assigned by
// the underlying BacktestRunner; this runner only decides the envelope ID,
// the aggregate, and the ordering.
type MultiPeriodRunner struct {
	// MaxParallel bounds the concurrent per-period runs. <=0 falls back to
	// the BACKTEST_MAX_PARALLEL env default (4).
	MaxParallel int
	// Now is injected for deterministic tests; defaults to time.Now.
	Now func() time.Time
}

// NewMultiPeriodRunner constructs a runner with sensible defaults. Tests can
// override fields post-construction.
func NewMultiPeriodRunner() *MultiPeriodRunner {
	return &MultiPeriodRunner{
		MaxParallel: envMaxParallel(),
		Now:         time.Now,
	}
}

// Run executes the per-period inputs in parallel and returns the assembled
// envelope. The returned MultiPeriodResult has its ID generated, each
// LabeledBacktestResult carries the per-period output of BacktestRunner.Run,
// and Aggregate is computed via ComputeAggregate.
//
// The caller is responsible for persisting the per-period BacktestResult
// rows (so their IDs match what the caller expects) and then the envelope.
// This split keeps MultiPeriodRunner pure (no DB) and lets tests exercise
// the parallelism without touching SQLite.
func (r *MultiPeriodRunner) Run(ctx context.Context, in MultiPeriodInput) (*entity.MultiPeriodResult, error) {
	if len(in.Periods) == 0 {
		return nil, fmt.Errorf("multi-period: at least one period is required")
	}
	if in.RunInputForPeriod == nil {
		return nil, fmt.Errorf("multi-period: RunInputForPeriod callback is required")
	}
	if err := validatePeriodLabels(in.Periods); err != nil {
		return nil, err
	}

	maxP := r.MaxParallel
	if maxP <= 0 {
		maxP = 4
	}

	results := make([]entity.LabeledBacktestResult, len(in.Periods))
	var mu sync.Mutex // only for assigning slot index i (results[i]=...)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxP)

	for i := range in.Periods {
		i := i
		period := in.Periods[i]
		g.Go(func() error {
			runner, input, err := in.RunInputForPeriod(period)
			if err != nil {
				return fmt.Errorf("period %q: build run input: %w", period.Label, err)
			}
			if runner == nil {
				return fmt.Errorf("period %q: nil runner returned", period.Label)
			}
			result, err := runner.Run(gctx, input)
			if err != nil {
				return fmt.Errorf("period %q: run: %w", period.Label, err)
			}
			if result == nil {
				return fmt.Errorf("period %q: nil result", period.Label)
			}
			mu.Lock()
			results[i] = entity.LabeledBacktestResult{Label: period.Label, Result: *result}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	agg := ComputeAggregate(results)

	id, err := NewULID()
	if err != nil {
		return nil, fmt.Errorf("multi-period: generate id: %w", err)
	}

	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}

	return &entity.MultiPeriodResult{
		ID:             id,
		CreatedAt:      now.Unix(),
		ProfileName:    in.ProfileName,
		PDCACycleID:    in.PDCACycleID,
		Hypothesis:     in.Hypothesis,
		ParentResultID: in.ParentResultID,
		Periods:        results,
		Aggregate:      agg,
	}, nil
}

// validatePeriodLabels returns an error if labels are empty or duplicated.
// We require human-readable unique labels so callers can correlate rows
// back to their request without relying on order.
func validatePeriodLabels(periods []entity.PeriodSpec) error {
	seen := make(map[string]struct{}, len(periods))
	for _, p := range periods {
		if p.Label == "" {
			return fmt.Errorf("multi-period: every period must have a non-empty label")
		}
		if _, dup := seen[p.Label]; dup {
			return fmt.Errorf("multi-period: duplicate period label %q", p.Label)
		}
		seen[p.Label] = struct{}{}
	}
	return nil
}

func envMaxParallel() int {
	raw := os.Getenv("BACKTEST_MAX_PARALLEL")
	if raw == "" {
		return 4
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 4
	}
	return n
}
