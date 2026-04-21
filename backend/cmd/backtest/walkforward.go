package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

// walkForwardCommand drives `backtest walk-forward`: same grid semantics as
// POST /backtest/walk-forward but usable from the shell without a running
// server. The CLI path is deliberately thin — all the heavy lifting
// (grid expansion, window schedule, runner) is shared with the HTTP handler.
func walkForwardCommand(args []string) error {
	fs := flag.NewFlagSet("walk-forward", flag.ContinueOnError)

	var (
		dataPath       = fs.String("data", "", "primary timeframe CSV path (required)")
		dataHTFPath    = fs.String("data-htf", "", "higher timeframe CSV path")
		fromDate       = fs.String("from", "", "window schedule start (YYYY-MM-DD) (required)")
		toDate         = fs.String("to", "", "window schedule end   (YYYY-MM-DD) (required)")
		inMonths       = fs.Int("in", 6, "in-sample months per window")
		oosMonths      = fs.Int("oos", 3, "out-of-sample months per window")
		stepMonths     = fs.Int("step", 3, "step size between window starts (months)")
		baseProfile    = fs.String("profile", "", "base strategy profile name (required)")
		objective      = fs.String("objective", "return", `objective for IS selection: "return" | "sharpe" | "profit_factor"`)
		gridFile       = fs.String("grid-file", "", "path to a JSON file shaped like [{path,values}, ...]")
		initialBalance = fs.Float64("initial-balance", 100000, "initial balance in JPY")
		spread         = fs.Float64("spread", 0.1, "spread percent")
		carryingCost   = fs.Float64("carrying-cost", 0.04, "daily carrying cost percent")
		slippage       = fs.Float64("slippage", 0, "slippage percent")
		tradeAmount    = fs.Float64("trade-amount", 0.01, "trade amount")
		outputJSON     = fs.String("output", "", "write result envelope to this path as JSON (optional)")
	)
	var gridInline paramFlags
	fs.Var(&gridInline, "grid", `parameter axis, repeatable: "path=v1,v2,v3". e.g. --grid "signal_rules.contrarian.stoch_entry_max=0,10,20"`)

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dataPath == "" {
		return fmt.Errorf("--data is required")
	}
	if *baseProfile == "" {
		return fmt.Errorf("--profile is required")
	}
	if *fromDate == "" || *toDate == "" {
		return fmt.Errorf("--from and --to are required")
	}
	switch *objective {
	case "", "return", "sharpe", "profit_factor":
	default:
		return fmt.Errorf("invalid --objective %q (want return|sharpe|profit_factor)", *objective)
	}

	profile, err := loadProfileIfSet(*baseProfile, profilesBaseDir)
	if err != nil {
		return err
	}
	if profile == nil {
		return fmt.Errorf("profile %q not found", *baseProfile)
	}

	overrides, err := resolveWalkForwardGrid(gridInline, *gridFile)
	if err != nil {
		return err
	}
	// Pre-validate every path so a typo fails the CLI up-front.
	for _, ov := range overrides {
		if _, err := bt.ApplyOverrides(entity.StrategyProfile{}, map[string]float64{ov.Path: 0}); err != nil {
			return fmt.Errorf("invalid --grid path %q: %w", ov.Path, err)
		}
	}
	grid, err := bt.ExpandGrid(overrides)
	if err != nil {
		return fmt.Errorf("expand grid: %w", err)
	}

	fromT, err := parseWFDate(*fromDate)
	if err != nil {
		return fmt.Errorf("--from: %w", err)
	}
	toT, err := parseWFDate(*toDate)
	if err != nil {
		return fmt.Errorf("--to: %w", err)
	}
	windows, err := bt.ComputeWindows(fromT, toT, *inMonths, *oosMonths, *stepMonths)
	if err != nil {
		return err
	}

	primary, err := csvinfra.LoadCandles(*dataPath)
	if err != nil {
		return fmt.Errorf("load primary csv: %w", err)
	}
	var higherCandles []entity.Candle
	if *dataHTFPath != "" {
		htf, err := csvinfra.LoadCandles(*dataHTFPath)
		if err != nil {
			return fmt.Errorf("load higher tf csv: %w", err)
		}
		higherCandles = htf.Candles
	}

	runner := bt.NewWalkForwardRunner()
	in := bt.WalkForwardInput{
		BaseProfile: *profile,
		Windows:     windows,
		Grid:        grid,
		Objective:   *objective,
		RunWindow: func(ctx context.Context, phase bt.WalkForwardPhase, pf entity.StrategyProfile, wFrom, wTo time.Time) (*entity.BacktestResult, error) {
			strat, err := strategyuc.NewConfigurableStrategy(&pf)
			if err != nil {
				return nil, err
			}
			cfg := entity.BacktestConfig{
				Symbol:           primary.Symbol,
				SymbolID:         primary.SymbolID,
				PrimaryInterval:  primary.Interval,
				HigherTFInterval: "PT1H",
				FromTimestamp:    wFrom.UnixMilli(),
				ToTimestamp:      wTo.UnixMilli(),
				InitialBalance:   *initialBalance,
				SpreadPercent:    *spread,
				DailyCarryCost:   *carryingCost,
				SlippagePercent:  *slippage,
			}
			if len(higherCandles) == 0 {
				cfg.HigherTFInterval = ""
			}
			risk := entity.RiskConfig{
				MaxPositionAmount:     nonZeroFloat(pf.Risk.MaxPositionAmount, 1_000_000_000.0),
				MaxDailyLoss:          nonZeroFloat(pf.Risk.MaxDailyLoss, 1_000_000_000.0),
				StopLossPercent:       pf.Risk.StopLossPercent,
				StopLossATRMultiplier: pf.Risk.StopLossATRMultiplier,
				TrailingATRMultiplier: pf.Risk.TrailingATRMultiplier,
				TakeProfitPercent:     pf.Risk.TakeProfitPercent,
				InitialCapital:        *initialBalance,
			}
			windowRunner := bt.NewBacktestRunner(bt.WithStrategy(strat))
			return windowRunner.Run(ctx, bt.RunInput{
				Config:         cfg,
				TradeAmount:    *tradeAmount,
				PrimaryCandles: primary.Candles,
				HigherCandles:  higherCandles,
				RiskConfig:     risk,
			})
		},
	}

	out, err := runner.Run(context.Background(), in)
	if err != nil {
		return err
	}

	printWalkForwardSummary(out)

	if *outputJSON != "" {
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		if err := os.WriteFile(*outputJSON, b, 0o644); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		fmt.Printf("envelope json: %s\n", *outputJSON)
	}
	return nil
}

// resolveWalkForwardGrid builds the []ParameterOverride from either a JSON
// file (preferred when many axes) or the repeated --grid "path=v1,v2,v3"
// inline flag. Both sources cannot be mixed — specifying both is a caller
// error because the priority would be ambiguous.
func resolveWalkForwardGrid(inline paramFlags, file string) ([]bt.ParameterOverride, error) {
	switch {
	case file != "" && len(inline) > 0:
		return nil, fmt.Errorf("use --grid OR --grid-file, not both")
	case file != "":
		return readGridFile(file)
	case len(inline) > 0:
		return parseGridInline(inline)
	default:
		// Empty grid → single baseline-only combo. ExpandGrid understands
		// this case, so return nil to surface exactly that behaviour.
		return nil, nil
	}
}

func readGridFile(path string) ([]bt.ParameterOverride, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read grid file: %w", err)
	}
	var out []bt.ParameterOverride
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse grid file: %w", err)
	}
	return out, nil
}

func parseGridInline(inline paramFlags) ([]bt.ParameterOverride, error) {
	out := make([]bt.ParameterOverride, 0, len(inline))
	for _, spec := range inline {
		eq := strings.IndexByte(spec, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("invalid --grid %q: want path=v1,v2,v3", spec)
		}
		path := strings.TrimSpace(spec[:eq])
		raw := strings.TrimSpace(spec[eq+1:])
		if path == "" || raw == "" {
			return nil, fmt.Errorf("invalid --grid %q: empty path or values", spec)
		}
		pieces := strings.Split(raw, ",")
		values := make([]float64, 0, len(pieces))
		for _, p := range pieces {
			p = strings.TrimSpace(p)
			if p == "" {
				return nil, fmt.Errorf("invalid --grid %q: empty value", spec)
			}
			v, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid --grid value %q: %w", p, err)
			}
			values = append(values, v)
		}
		out = append(out, bt.ParameterOverride{Path: path, Values: values})
	}
	return out, nil
}

// parseWFDate parses YYYY-MM-DD into a UTC time. The walk-forward CLI runs
// server-side on a Linux container so UTC is the least-surprising default;
// HTTP handler already treats from/to as UTC.
func parseWFDate(v string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date %q: %w", v, err)
	}
	return t, nil
}

// nonZeroFloat is the CLI-side counterpart of the handler's helper; kept
// here so the CLI doesn't import the HTTP handler package.
func nonZeroFloat(primary, fallback float64) float64 {
	if primary > 0 {
		return primary
	}
	return fallback
}

func printWalkForwardSummary(r *bt.WalkForwardResult) {
	fmt.Println("=== Walk-Forward Result ===")
	fmt.Printf("ID:           %s\n", r.ID)
	fmt.Printf("Profile:      %s\n", r.BaseProfile)
	fmt.Printf("Objective:    %s\n", r.Objective)
	fmt.Printf("Windows:      %d\n", len(r.Windows))
	for _, w := range r.Windows {
		fmt.Printf("  #%d IS[%s..%s] OOS[%s..%s]  best=%v  OOSReturn=%+.2f%%\n",
			w.Index,
			formatDateUnixMS(w.InSampleFrom), formatDateUnixMS(w.InSampleTo),
			formatDateUnixMS(w.OOSFrom), formatDateUnixMS(w.OOSTo),
			w.BestParameters,
			w.OOSResult.Summary.TotalReturn*100,
		)
	}
	fmt.Printf("Aggregate:    geomMean=%+.2f%%  worst=%+.2f%%  best=%+.2f%%  worstDD=%.2f%%  robustness=%.4f\n",
		r.AggregateOOS.GeomMeanReturn*100,
		r.AggregateOOS.WorstReturn*100,
		r.AggregateOOS.BestReturn*100,
		r.AggregateOOS.WorstDrawdown*100,
		r.AggregateOOS.RobustnessScore,
	)
}

func formatDateUnixMS(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}
