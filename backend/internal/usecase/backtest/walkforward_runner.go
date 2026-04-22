package backtest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// WalkForwardInput drives WalkForwardRunner.Run. As with MultiPeriodRunner,
// a callback lets the handler build a per-window RunInput without this
// usecase needing to know about CSVs, risk config assembly, or profile
// loading. The callback receives the resolved (profile, is/oos timestamps)
// and returns a ready-to-run backtest pair.
type WalkForwardInput struct {
	BaseProfile entity.StrategyProfile
	Windows     []WalkForwardWindow
	// Grid is the legacy numeric-only pre-expanded grid. New callers that
	// also use string axes (e.g. htf_filter.mode) should populate
	// Combinations instead. When both are set, Combinations wins.
	Grid         []map[string]float64
	Combinations []GridCombination
	Objective    string // "return" | "sharpe" | "profit_factor"
	PDCACycleID  string
	Hypothesis   string
	ParentResultID *string

	// RunWindow is called per (window, param-combination) for the IS phase
	// and per window (with the winning combination) for the OOS phase.
	// It must return a populated BacktestResult.
	RunWindow func(ctx context.Context, phase WalkForwardPhase, profile entity.StrategyProfile, from, to time.Time) (*entity.BacktestResult, error)
}

// WalkForwardPhase distinguishes IS (parameter selection) from OOS
// (unbiased scoring) calls to RunWindow. Callers use it to tag the
// resulting BacktestResult for DB storage / display.
type WalkForwardPhase string

const (
	WalkForwardPhaseInSample     WalkForwardPhase = "in_sample"
	WalkForwardPhaseOutOfSample WalkForwardPhase = "out_of_sample"
)

// WalkForwardWindowResult is the per-window output: the best grid combo
// chosen on IS, every grid combo's IS summary, and the unbiased OOS result.
type WalkForwardWindowResult struct {
	Index                int                   `json:"index"`
	InSampleFrom         int64                 `json:"inSampleFrom"`
	InSampleTo           int64                 `json:"inSampleTo"`
	OOSFrom              int64                 `json:"oosFrom"`
	OOSTo                int64                 `json:"oosTo"`
	BestParameters       map[string]float64    `json:"bestParameters"`
	BestStringParameters map[string]string     `json:"bestStringParameters,omitempty"`
	ISResults            []WalkForwardISResult `json:"isResults"`
	OOSResult            entity.BacktestResult `json:"oosResult"`
}

// WalkForwardISResult records a single IS run per grid combination.
type WalkForwardISResult struct {
	Parameters       map[string]float64     `json:"parameters"`
	StringParameters map[string]string      `json:"stringParameters,omitempty"`
	Summary          entity.BacktestSummary `json:"summary"`
	Score            float64                `json:"score"`
	ResultID         string                 `json:"resultId,omitempty"`
}

// WalkForwardResult is the envelope returned by Run. Individual per-window
// results live in Windows; AggregateOOS is the Phase-B RobustnessScore
// scope applied to the OOS returns of every window.
type WalkForwardResult struct {
	ID             string                      `json:"id"`
	CreatedAt      int64                       `json:"createdAt"`
	BaseProfile    string                      `json:"baseProfile"`
	Objective      string                      `json:"objective"`
	PDCACycleID    string                      `json:"pdcaCycleId,omitempty"`
	Hypothesis     string                      `json:"hypothesis,omitempty"`
	ParentResultID *string                     `json:"parentResultId,omitempty"`
	Windows        []WalkForwardWindowResult   `json:"windows"`
	AggregateOOS   entity.MultiPeriodAggregate `json:"aggregateOOS"`
}

// WalkForwardRunner is stateless; construct one per request.
type WalkForwardRunner struct {
	// MaxParallel bounds the number of concurrent IS backtests within a
	// single window. <=0 falls back to BACKTEST_MAX_PARALLEL's usual value
	// (4) via envMaxParallel() from multi_period_runner.go.
	MaxParallel int
	Now         func() time.Time
}

// NewWalkForwardRunner returns a runner with sensible defaults.
func NewWalkForwardRunner() *WalkForwardRunner {
	return &WalkForwardRunner{
		MaxParallel: envMaxParallel(),
		Now:         time.Now,
	}
}

// Run executes the walk-forward schedule. For each window: run every grid
// combo on the IS slice, pick the best by Objective, then run that combo
// once on the OOS slice to produce the unbiased score. The OOS scores
// across all windows are then folded into AggregateOOS via ComputeAggregate.
//
// Errors on any single IS or OOS call are returned immediately; partial
// results are discarded so callers never see a half-complete envelope.
func (r *WalkForwardRunner) Run(ctx context.Context, in WalkForwardInput) (*WalkForwardResult, error) {
	if len(in.Windows) == 0 {
		return nil, fmt.Errorf("walk-forward: at least one window is required")
	}
	// Prefer Combinations (supports mixed numeric+string axes) and fall
	// back to Grid for existing callers. Both empty = invalid.
	combos := in.Combinations
	if len(combos) == 0 {
		if len(in.Grid) == 0 {
			return nil, fmt.Errorf("walk-forward: grid must contain at least one combination (use [{}] for baseline-only)")
		}
		combos = make([]GridCombination, 0, len(in.Grid))
		for _, g := range in.Grid {
			combos = append(combos, GridCombination{Numeric: g, String: map[string]string{}})
		}
	}
	if in.RunWindow == nil {
		return nil, fmt.Errorf("walk-forward: RunWindow callback is required")
	}
	maxP := r.MaxParallel
	if maxP <= 0 {
		maxP = 4
	}

	windowResults := make([]WalkForwardWindowResult, len(in.Windows))

	for wi, w := range in.Windows {
		isResults := make([]WalkForwardISResult, len(combos))
		var mu sync.Mutex

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(maxP)
		for gi, combo := range combos {
			gi := gi
			combo := combo
			g.Go(func() error {
				profile, err := ApplyCombination(in.BaseProfile, combo)
				if err != nil {
					return fmt.Errorf("window %d combo %d: %w", wi, gi, err)
				}
				res, err := in.RunWindow(gctx, WalkForwardPhaseInSample, profile, w.InSampleFrom, w.InSampleTo)
				if err != nil {
					return fmt.Errorf("window %d IS combo %d: %w", wi, gi, err)
				}
				if res == nil {
					return fmt.Errorf("window %d IS combo %d: nil result", wi, gi)
				}
				score := SelectByObjective(res.Summary, in.Objective)
				mu.Lock()
				isResults[gi] = WalkForwardISResult{
					Parameters:       combo.Numeric,
					StringParameters: combo.String,
					Summary:          res.Summary,
					Score:            score,
					ResultID:         res.ID,
				}
				mu.Unlock()
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return nil, err
		}

		// Pick best by score (ties -> first, deterministic).
		bestIdx := 0
		for i := 1; i < len(isResults); i++ {
			if isResults[i].Score > isResults[bestIdx].Score {
				bestIdx = i
			}
		}
		bestCombo := combos[bestIdx]

		// OOS run with the winning combo.
		bestProfile, err := ApplyCombination(in.BaseProfile, bestCombo)
		if err != nil {
			return nil, fmt.Errorf("window %d apply best combo: %w", wi, err)
		}
		oosResult, err := in.RunWindow(ctx, WalkForwardPhaseOutOfSample, bestProfile, w.OOSFrom, w.OOSTo)
		if err != nil {
			return nil, fmt.Errorf("window %d OOS: %w", wi, err)
		}
		if oosResult == nil {
			return nil, fmt.Errorf("window %d OOS: nil result", wi)
		}

		windowResults[wi] = WalkForwardWindowResult{
			Index:                w.Index,
			InSampleFrom:         w.InSampleFrom.UnixMilli(),
			InSampleTo:           w.InSampleTo.UnixMilli(),
			OOSFrom:              w.OOSFrom.UnixMilli(),
			OOSTo:                w.OOSTo.UnixMilli(),
			BestParameters:       bestCombo.Numeric,
			BestStringParameters: bestCombo.String,
			ISResults:            isResults,
			OOSResult:            *oosResult,
		}
	}

	// Aggregate OOS across windows using PR-2's RobustnessScore helper.
	labelled := make([]entity.LabeledBacktestResult, 0, len(windowResults))
	for _, wr := range windowResults {
		labelled = append(labelled, entity.LabeledBacktestResult{
			Label:  fmt.Sprintf("w%d", wr.Index),
			Result: wr.OOSResult,
		})
	}
	aggregate := ComputeAggregate(labelled)

	id, err := NewULID()
	if err != nil {
		return nil, fmt.Errorf("walk-forward: generate id: %w", err)
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}

	return &WalkForwardResult{
		ID:             id,
		CreatedAt:      now.Unix(),
		BaseProfile:    in.BaseProfile.Name,
		Objective:      in.Objective,
		PDCACycleID:    in.PDCACycleID,
		Hypothesis:     in.Hypothesis,
		ParentResultID: in.ParentResultID,
		Windows:        windowResults,
		AggregateOOS:   aggregate,
	}, nil
}
