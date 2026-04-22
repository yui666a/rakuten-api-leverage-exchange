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
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
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

	overrides, stringOverrides, err := resolveWalkForwardGrid(gridInline, *gridFile)
	if err != nil {
		return err
	}
	// Pre-validate every path so a typo fails the CLI up-front.
	for _, ov := range overrides {
		if _, err := bt.ApplyOverrides(entity.StrategyProfile{}, map[string]float64{ov.Path: 0}); err != nil {
			return fmt.Errorf("invalid --grid path %q: %w", ov.Path, err)
		}
	}
	for _, ov := range stringOverrides {
		if _, err := bt.ApplyStringOverrides(entity.StrategyProfile{}, map[string]string{ov.Path: ""}); err != nil {
			return fmt.Errorf("invalid --grid path %q: %w", ov.Path, err)
		}
	}
	combos, err := bt.ExpandCombinedGrid(overrides, stringOverrides)
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
		BaseProfile:  *profile,
		Windows:      windows,
		Combinations: combos,
		Objective:    *objective,
		RunWindow: func(ctx context.Context, phase bt.WalkForwardPhase, pf entity.StrategyProfile, wFrom, wTo time.Time) (*entity.BacktestResult, error) {
			// Build a fresh Strategy per (window, combo) so a
			// regime-aware ProfileRouter starts each WFO window with
			// a clean detector hysteresis state — see the matching
			// comment in the HTTP handler for rationale.
			strat, err := strategyuc.BuildStrategyFromProfile(strategyprofile.NewLoader(profilesBaseDir), &pf)
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

// resolveWalkForwardGrid builds the numeric and string override slices
// from either a JSON file (preferred when many axes) or the repeated
// --grid "path=v1,v2,v3" inline flag. Both sources cannot be mixed —
// specifying both is a caller error because the priority would be
// ambiguous.
//
// The inline flag auto-detects numeric vs. string axes: if every value
// parses as float64 the axis is numeric; otherwise every value is
// treated as a string (no mixed-type axes). This matches the file
// format, which uses the shape {path, values} and infers type from
// whether values are JSON numbers or JSON strings.
func resolveWalkForwardGrid(inline paramFlags, file string) ([]bt.ParameterOverride, []bt.ParameterStringOverride, error) {
	switch {
	case file != "" && len(inline) > 0:
		return nil, nil, fmt.Errorf("use --grid OR --grid-file, not both")
	case file != "":
		return readGridFile(file)
	case len(inline) > 0:
		return parseGridInline(inline)
	default:
		// Empty grid → single baseline-only combo. ExpandCombinedGrid
		// understands this case, so return nil to surface that behaviour.
		return nil, nil, nil
	}
}

// rawGridAxis is the file format each entry can take. Values may be
// either JSON numbers (populating Values) or JSON strings (populating
// StringValues); a mix within one axis is rejected at unmarshal time.
type rawGridAxis struct {
	Path   string            `json:"path"`
	Values []json.RawMessage `json:"values"`
}

func readGridFile(path string) ([]bt.ParameterOverride, []bt.ParameterStringOverride, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read grid file: %w", err)
	}
	var raw []rawGridAxis
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse grid file: %w", err)
	}
	var numeric []bt.ParameterOverride
	var strings []bt.ParameterStringOverride
	for _, axis := range raw {
		num, str, err := classifyAxisValues(axis.Path, axis.Values)
		if err != nil {
			return nil, nil, err
		}
		if num != nil {
			numeric = append(numeric, bt.ParameterOverride{Path: axis.Path, Values: num})
			continue
		}
		strings = append(strings, bt.ParameterStringOverride{Path: axis.Path, Values: str})
	}
	return numeric, strings, nil
}

// classifyAxisValues inspects the JSON-raw values of one axis. An axis
// is numeric when every value is a JSON number, string when every value
// is a JSON string; anything else (bool, object, mixed) is a caller
// error.
func classifyAxisValues(path string, values []json.RawMessage) ([]float64, []string, error) {
	if len(values) == 0 {
		return nil, nil, fmt.Errorf("grid axis %q has no values", path)
	}
	allNumeric := true
	allString := true
	for _, v := range values {
		trimmed := strings.TrimSpace(string(v))
		if trimmed == "" {
			return nil, nil, fmt.Errorf("grid axis %q has empty value", path)
		}
		if trimmed[0] != '"' {
			allString = false
		}
		var probe float64
		if err := json.Unmarshal(v, &probe); err != nil {
			allNumeric = false
		}
	}
	if allNumeric {
		nums := make([]float64, 0, len(values))
		for _, v := range values {
			var n float64
			if err := json.Unmarshal(v, &n); err != nil {
				return nil, nil, fmt.Errorf("grid axis %q: parse number: %w", path, err)
			}
			nums = append(nums, n)
		}
		return nums, nil, nil
	}
	if allString {
		strs := make([]string, 0, len(values))
		for _, v := range values {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return nil, nil, fmt.Errorf("grid axis %q: parse string: %w", path, err)
			}
			strs = append(strs, s)
		}
		return nil, strs, nil
	}
	return nil, nil, fmt.Errorf("grid axis %q mixes numeric and string values (pick one)", path)
}

func parseGridInline(inline paramFlags) ([]bt.ParameterOverride, []bt.ParameterStringOverride, error) {
	var numeric []bt.ParameterOverride
	var strs []bt.ParameterStringOverride
	for _, spec := range inline {
		eq := strings.IndexByte(spec, '=')
		if eq <= 0 {
			return nil, nil, fmt.Errorf("invalid --grid %q: want path=v1,v2,v3", spec)
		}
		path := strings.TrimSpace(spec[:eq])
		raw := strings.TrimSpace(spec[eq+1:])
		if path == "" || raw == "" {
			return nil, nil, fmt.Errorf("invalid --grid %q: empty path or values", spec)
		}
		pieces := strings.Split(raw, ",")
		values := make([]float64, 0, len(pieces))
		stringValues := make([]string, 0, len(pieces))
		allNumeric := true
		for _, p := range pieces {
			p = strings.TrimSpace(p)
			if p == "" {
				return nil, nil, fmt.Errorf("invalid --grid %q: empty value", spec)
			}
			if v, err := strconv.ParseFloat(p, 64); err == nil {
				values = append(values, v)
			} else {
				allNumeric = false
			}
			stringValues = append(stringValues, p)
		}
		if allNumeric {
			numeric = append(numeric, bt.ParameterOverride{Path: path, Values: values})
		} else {
			strs = append(strs, bt.ParameterStringOverride{Path: path, Values: stringValues})
		}
	}
	return numeric, strs, nil
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
