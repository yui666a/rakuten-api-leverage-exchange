package backtest

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"golang.org/x/sync/errgroup"
)

type ParamRange struct {
	Name string
	Min  float64
	Max  float64
	Step float64
}

type OptimizeConfig struct {
	MaxEvals int
	TopN     int
	Seed     int64
	Workers  int
}

type OptimizationResult struct {
	Params  map[string]float64
	Summary entity.BacktestSummary
}

type Optimizer struct {
	runner *BacktestRunner
}

func NewOptimizer(runner *BacktestRunner) *Optimizer {
	if runner == nil {
		runner = NewBacktestRunner()
	}
	return &Optimizer{runner: runner}
}

func (o *Optimizer) Optimize(ctx context.Context, base RunInput, ranges []ParamRange, cfg OptimizeConfig) ([]OptimizationResult, error) {
	if len(ranges) == 0 {
		return nil, fmt.Errorf("at least one param range is required")
	}
	if cfg.MaxEvals <= 0 {
		cfg.MaxEvals = 1000
	}
	if cfg.TopN <= 0 {
		cfg.TopN = 10
	}
	if cfg.Seed == 0 {
		cfg.Seed = time.Now().UnixNano()
	}
	if cfg.Workers <= 0 {
		cfg.Workers = runtime.GOMAXPROCS(0)
	}

	grid, err := buildGrid(ranges)
	if err != nil {
		return nil, err
	}
	if len(grid) == 0 {
		return nil, fmt.Errorf("no parameter combinations generated")
	}

	selected := grid
	if len(selected) > cfg.MaxEvals {
		selected = sampleCombos(grid, cfg.MaxEvals, cfg.Seed)
	}

	results := make([]OptimizationResult, len(selected))
	sem := make(chan struct{}, cfg.Workers)
	g, gctx := errgroup.WithContext(ctx)

	for i, combo := range selected {
		i := i
		combo := cloneParams(combo)
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			candidate := applyParams(base, combo)
			result, err := o.runner.Run(gctx, candidate)
			if err != nil {
				return err
			}
			results[i] = OptimizationResult{
				Params:  combo,
				Summary: result.Summary,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Summary.SharpeRatio == results[j].Summary.SharpeRatio {
			return results[i].Summary.MaxDrawdown < results[j].Summary.MaxDrawdown
		}
		return results[i].Summary.SharpeRatio > results[j].Summary.SharpeRatio
	})

	if len(results) > cfg.TopN {
		results = results[:cfg.TopN]
	}
	return results, nil
}

func buildGrid(ranges []ParamRange) ([]map[string]float64, error) {
	type axis struct {
		name   string
		values []float64
	}
	axes := make([]axis, 0, len(ranges))
	for _, r := range ranges {
		if r.Name == "" {
			return nil, fmt.Errorf("param name is required")
		}
		if r.Step <= 0 {
			return nil, fmt.Errorf("step must be positive: %s", r.Name)
		}
		if r.Max < r.Min {
			return nil, fmt.Errorf("max must be >= min: %s", r.Name)
		}

		values := make([]float64, 0, int((r.Max-r.Min)/r.Step)+2)
		for v := r.Min; v <= r.Max+1e-12; v += r.Step {
			values = append(values, round(v))
		}
		if len(values) == 0 {
			values = append(values, round(r.Min))
		}
		axes = append(axes, axis{name: r.Name, values: values})
	}

	combos := []map[string]float64{{}}
	for _, ax := range axes {
		next := make([]map[string]float64, 0, len(combos)*len(ax.values))
		for _, combo := range combos {
			for _, v := range ax.values {
				c := make(map[string]float64, len(combo)+1)
				for k, vv := range combo {
					c[k] = vv
				}
				c[ax.name] = v
				next = append(next, c)
			}
		}
		combos = next
	}
	return combos, nil
}

func sampleCombos(grid []map[string]float64, n int, seed int64) []map[string]float64 {
	if len(grid) <= n {
		return grid
	}
	rnd := rand.New(rand.NewSource(seed))
	perm := rnd.Perm(len(grid))
	out := make([]map[string]float64, 0, n)
	for _, idx := range perm[:n] {
		out = append(out, grid[idx])
	}
	return out
}

func applyParams(base RunInput, params map[string]float64) RunInput {
	out := base
	for k, v := range params {
		switch k {
		case "stop_loss_percent":
			out.RiskConfig.StopLossPercent = v
		case "take_profit_percent":
			out.RiskConfig.TakeProfitPercent = v
		case "initial_balance":
			out.Config.InitialBalance = v
			out.RiskConfig.InitialCapital = v
		case "spread_percent":
			out.Config.SpreadPercent = v
		case "carrying_cost":
			out.Config.DailyCarryCost = v
		case "trade_amount":
			out.TradeAmount = v
		}
	}
	return out
}

func cloneParams(src map[string]float64) map[string]float64 {
	dst := make(map[string]float64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func round(v float64) float64 {
	return math.Round(v*1e9) / 1e9
}

// --- Phase 2b: Local Neighborhood Refinement ---

type RefineConfig struct {
	TopN     int     // how many top coarse results to refine around
	StepDiv  float64 // divide original step by this for finer grid
	MaxEvals int     // max evaluations for refinement phase
	Workers  int
}

func (o *Optimizer) Refine(ctx context.Context, base RunInput, coarseResults []OptimizationResult, originalRanges []ParamRange, cfg RefineConfig) ([]OptimizationResult, error) {
	if len(coarseResults) == 0 {
		return nil, fmt.Errorf("coarse results are required for refinement")
	}
	if len(originalRanges) == 0 {
		return nil, fmt.Errorf("original ranges are required for refinement")
	}
	if cfg.TopN <= 0 {
		cfg.TopN = 5
	}
	if cfg.StepDiv <= 0 {
		cfg.StepDiv = 4.0
	}
	if cfg.MaxEvals <= 0 {
		cfg.MaxEvals = 5000
	}
	if cfg.Workers <= 0 {
		cfg.Workers = runtime.GOMAXPROCS(0)
	}

	topN := cfg.TopN
	if topN > len(coarseResults) {
		topN = len(coarseResults)
	}

	var allCombos []map[string]float64
	for i := 0; i < topN; i++ {
		neighborRanges := buildNeighborhoodRanges(coarseResults[i].Params, originalRanges, cfg.StepDiv)
		grid, err := buildGrid(neighborRanges)
		if err != nil {
			return nil, fmt.Errorf("build neighborhood grid for result %d: %w", i, err)
		}
		allCombos = append(allCombos, grid...)
	}

	allCombos = deduplicateCombos(allCombos)
	if len(allCombos) == 0 {
		return nil, fmt.Errorf("no refinement candidates generated")
	}

	selected := allCombos
	if len(selected) > cfg.MaxEvals {
		selected = sampleCombos(allCombos, cfg.MaxEvals, time.Now().UnixNano())
	}

	results := make([]OptimizationResult, len(selected))
	sem := make(chan struct{}, cfg.Workers)
	g, gctx := errgroup.WithContext(ctx)

	for i, combo := range selected {
		i := i
		combo := cloneParams(combo)
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			candidate := applyParams(base, combo)
			result, err := o.runner.Run(gctx, candidate)
			if err != nil {
				return err
			}
			results[i] = OptimizationResult{
				Params:  combo,
				Summary: result.Summary,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Summary.SharpeRatio == results[j].Summary.SharpeRatio {
			return results[i].Summary.MaxDrawdown < results[j].Summary.MaxDrawdown
		}
		return results[i].Summary.SharpeRatio > results[j].Summary.SharpeRatio
	})

	if cfg.TopN > 0 && len(results) > cfg.TopN {
		results = results[:cfg.TopN]
	}
	return results, nil
}

func buildNeighborhoodRanges(center map[string]float64, originalRanges []ParamRange, stepDiv float64) []ParamRange {
	out := make([]ParamRange, 0, len(originalRanges))
	for _, orig := range originalRanges {
		cv, ok := center[orig.Name]
		if !ok {
			out = append(out, orig)
			continue
		}
		fineStep := orig.Step / stepDiv
		lo := math.Max(orig.Min, cv-orig.Step)
		hi := math.Min(orig.Max, cv+orig.Step)
		out = append(out, ParamRange{
			Name: orig.Name,
			Min:  round(lo),
			Max:  round(hi),
			Step: round(fineStep),
		})
	}
	return out
}

func deduplicateCombos(combos []map[string]float64) []map[string]float64 {
	seen := make(map[string]struct{}, len(combos))
	out := make([]map[string]float64, 0, len(combos))
	for _, combo := range combos {
		key := comboKey(combo)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, combo)
	}
	return out
}

func comboKey(params map[string]float64) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	for _, k := range keys {
		b = append(b, []byte(fmt.Sprintf("%s=%.9f;", k, params[k]))...)
	}
	return string(b)
}
