package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/config"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

// profilesBaseDir is the on-disk directory under the backend module where
// StrategyProfile JSON files live. It matches the contract documented in
// docs/superpowers/specs/2026-04-16-pdca-strategy-optimizer-design.md §8.3.
// The path is relative, so callers are expected to invoke this CLI with
// cwd = backend/ (see spec example).
const profilesBaseDir = "profiles"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "run":
		if err := runCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "backtest run failed: %v\n", err)
			os.Exit(1)
		}
	case "optimize":
		if err := optimizeCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "backtest optimize failed: %v\n", err)
			os.Exit(1)
		}
	case "refine":
		if err := refineCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "backtest refine failed: %v\n", err)
			os.Exit(1)
		}
	case "download":
		if err := downloadCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "backtest download failed: %v\n", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("Usage:")
	fmt.Println("  go run ./cmd/backtest run --data <primary.csv> [--data-htf <htf.csv>] [flags]")
	fmt.Println("  go run ./cmd/backtest optimize --data <primary.csv> --param \"stop_loss_percent=1:10:1\" [flags]")
	fmt.Println("  go run ./cmd/backtest refine --data <primary.csv> --param \"stop_loss_percent=1:10:1\" [--top 5] [--step-div 4] [flags]")
	fmt.Println("  go run ./cmd/backtest download ...")
}

// runFlags is the parsed set of CLI flags for the `run` subcommand. Kept in
// a struct so buildRunConfig can be unit-tested without executing an actual
// backtest.
type runFlags struct {
	DataPath       string
	DataHTFPath    string
	FromDate       string
	ToDate         string
	InitialBalance float64
	Spread         float64
	CarryingCost   float64
	Slippage       float64
	TradeAmount    float64
	StopLoss       float64
	TakeProfit     float64
	OutputDir      string
	Profile        string
	// Set contains the lowercase names of flags that were explicitly provided
	// by the user (via flag.Visit). Used to honour the override-precedence
	// rule documented in --help: profile → user-supplied flag.
	Set map[string]bool
}

// registerRunFlags wires the `run`-subcommand flag set. Extracted so tests
// can drive it with a fresh FlagSet without invoking Parse on os.Args.
func registerRunFlags(fs *flag.FlagSet, f *runFlags) {
	fs.StringVar(&f.DataPath, "data", "", "primary timeframe CSV path")
	fs.StringVar(&f.DataHTFPath, "data-htf", "", "higher timeframe CSV path")
	fs.StringVar(&f.FromDate, "from", "", "start date (YYYY-MM-DD)")
	fs.StringVar(&f.ToDate, "to", "", "end date (YYYY-MM-DD)")
	fs.Float64Var(&f.InitialBalance, "initial-balance", 100000, "initial balance in JPY")
	fs.Float64Var(&f.Spread, "spread", 0.1, "spread percent")
	fs.Float64Var(&f.CarryingCost, "carrying-cost", 0.04, "daily carrying cost percent")
	fs.Float64Var(&f.Slippage, "slippage", 0, "slippage percent")
	fs.Float64Var(&f.TradeAmount, "trade-amount", 0.01, "trade amount")
	fs.Float64Var(&f.StopLoss, "stop-loss", 5, "stop loss percent")
	fs.Float64Var(&f.TakeProfit, "take-profit", 10, "take profit percent")
	fs.StringVar(&f.OutputDir, "output", "", "output directory for trades/result")
	fs.StringVar(&f.Profile, "profile", "", "strategy profile name (resolves to profiles/<name>.json). "+
		"Individual flags (e.g. --stop-loss) override the profile's values when explicitly supplied.")
}

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var f runFlags
	registerRunFlags(fs, &f)
	if err := fs.Parse(args); err != nil {
		return err
	}
	f.Set = visitedFlagNames(fs)
	if f.DataPath == "" {
		return fmt.Errorf("--data is required")
	}

	profile, err := loadProfileIfSet(f.Profile)
	if err != nil {
		return err
	}

	input, err := buildRunInput(f, profile)
	if err != nil {
		return err
	}

	var runnerOpts []bt.RunnerOption
	if profile != nil {
		strat, err := strategyuc.NewConfigurableStrategy(profile)
		if err != nil {
			return fmt.Errorf("construct strategy from profile %q: %w", f.Profile, err)
		}
		runnerOpts = append(runnerOpts, bt.WithStrategy(strat))
	}

	runner := bt.NewBacktestRunner(runnerOpts...)
	result, err := runner.Run(context.Background(), input)
	if err != nil {
		return err
	}

	printSummary(result.Summary)
	if f.OutputDir != "" {
		if err := os.MkdirAll(f.OutputDir, 0o755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
		tradesPath := filepath.Join(f.OutputDir, "trades.csv")
		if err := writeTradesCSV(tradesPath, result.Trades); err != nil {
			return fmt.Errorf("write trades csv: %w", err)
		}
		fmt.Printf("trades csv: %s\n", tradesPath)
	}

	return nil
}

// visitedFlagNames returns the set of flag names that the user explicitly set
// on the command line. Used to distinguish "flag left at default" from
// "flag set to the same value that happens to be the default" — the latter
// must still override the profile per the spec.
func visitedFlagNames(fs *flag.FlagSet) map[string]bool {
	set := make(map[string]bool)
	fs.Visit(func(ff *flag.Flag) {
		set[ff.Name] = true
	})
	return set
}

// loadProfileIfSet loads a StrategyProfile from profiles/<name>.json if
// `name` is non-empty. It returns (nil, nil) when the caller did not request
// a profile, which the caller uses as the sentinel for "keep default
// strategy".
func loadProfileIfSet(name string) (*entity.StrategyProfile, error) {
	if name == "" {
		return nil, nil
	}
	// ResolveProfilePath rejects traversal / unsafe names before any I/O so
	// the caller sees a clean error for bad input without a disk roundtrip.
	if _, err := strategyprofile.ResolveProfilePath(profilesBaseDir, name); err != nil {
		return nil, fmt.Errorf("resolve profile %q: %w", name, err)
	}
	loader := strategyprofile.NewLoader(profilesBaseDir)
	profile, err := loader.Load(name)
	if err != nil {
		return nil, fmt.Errorf("load profile %q: %w", name, err)
	}
	return profile, nil
}

type paramFlags []string

func (p *paramFlags) String() string { return strings.Join(*p, ",") }
func (p *paramFlags) Set(v string) error {
	*p = append(*p, v)
	return nil
}

func optimizeCommand(args []string) error {
	fs := flag.NewFlagSet("optimize", flag.ContinueOnError)
	var f runFlags
	registerRunFlags(fs, &f)
	sortBy := fs.String("sort-by", "sharpe_ratio", "ranking metric (sharpe_ratio only)")
	top := fs.Int("top", 10, "top N results to print")
	maxEvals := fs.Int("max-evals", 10000, "max evaluated combinations")
	workers := fs.Int("workers", 8, "number of parallel workers")
	seed := fs.Int64("seed", 0, "random seed for sampling")
	var params paramFlags
	fs.Var(&params, "param", `parameter range: "name=min:max:step"`)

	if err := fs.Parse(args); err != nil {
		return err
	}
	f.Set = visitedFlagNames(fs)
	if f.DataPath == "" {
		return fmt.Errorf("--data is required")
	}
	if len(params) == 0 {
		return fmt.Errorf("at least one --param is required")
	}
	if *sortBy != "sharpe_ratio" {
		return fmt.Errorf("--sort-by currently supports only sharpe_ratio")
	}

	profile, err := loadProfileIfSet(f.Profile)
	if err != nil {
		return err
	}

	baseInput, err := buildRunInput(f, profile)
	if err != nil {
		return err
	}

	ranges := make([]bt.ParamRange, 0, len(params))
	for _, spec := range params {
		r, err := parseParamRange(spec)
		if err != nil {
			return err
		}
		ranges = append(ranges, r)
	}

	// Build the runner once; if a profile is set, its ConfigurableStrategy
	// drives every evaluated parameter combination so the optimizer
	// explores the space around the profile rather than the default
	// strategy.
	var runnerOpts []bt.RunnerOption
	if profile != nil {
		strat, err := strategyuc.NewConfigurableStrategy(profile)
		if err != nil {
			return fmt.Errorf("construct strategy from profile %q: %w", f.Profile, err)
		}
		runnerOpts = append(runnerOpts, bt.WithStrategy(strat))
	}
	optimizer := bt.NewOptimizer(bt.NewBacktestRunner(runnerOpts...))
	results, err := optimizer.Optimize(context.Background(), baseInput, ranges, bt.OptimizeConfig{
		MaxEvals: *maxEvals,
		TopN:     *top,
		Seed:     *seed,
		Workers:  *workers,
	})
	if err != nil {
		return err
	}

	fmt.Printf("evaluated top %d results\n", len(results))
	for i, r := range results {
		fmt.Printf(
			"%d) sharpe=%.4f maxDD=%.2f%% return=%+.2f%% trades=%d params=%v\n",
			i+1,
			r.Summary.SharpeRatio,
			r.Summary.MaxDrawdown*100,
			r.Summary.TotalReturn*100,
			r.Summary.TotalTrades,
			r.Params,
		)
	}
	return nil
}

func refineCommand(args []string) error {
	fs := flag.NewFlagSet("refine", flag.ContinueOnError)
	var f runFlags
	registerRunFlags(fs, &f)
	coarseTop := fs.Int("top", 10, "top N results from coarse search")
	coarseMaxEvals := fs.Int("max-evals", 10000, "max evaluated combinations for coarse search")
	coarseWorkers := fs.Int("workers", 8, "number of parallel workers")
	coarseSeed := fs.Int64("seed", 0, "random seed for sampling")
	refineTopN := fs.Int("refine-top", 5, "top N coarse results to refine around")
	refineStepDiv := fs.Float64("step-div", 4, "divide original step by this for finer grid")
	refineMaxEvals := fs.Int("refine-max-evals", 5000, "max evaluations for refinement phase")
	var params paramFlags
	fs.Var(&params, "param", `parameter range: "name=min:max:step"`)

	if err := fs.Parse(args); err != nil {
		return err
	}
	f.Set = visitedFlagNames(fs)
	if f.DataPath == "" {
		return fmt.Errorf("--data is required")
	}
	if len(params) == 0 {
		return fmt.Errorf("at least one --param is required")
	}

	profile, err := loadProfileIfSet(f.Profile)
	if err != nil {
		return err
	}

	baseInput, err := buildRunInput(f, profile)
	if err != nil {
		return err
	}

	ranges := make([]bt.ParamRange, 0, len(params))
	for _, spec := range params {
		r, err := parseParamRange(spec)
		if err != nil {
			return err
		}
		ranges = append(ranges, r)
	}

	var runnerOpts []bt.RunnerOption
	if profile != nil {
		strat, err := strategyuc.NewConfigurableStrategy(profile)
		if err != nil {
			return fmt.Errorf("construct strategy from profile %q: %w", f.Profile, err)
		}
		runnerOpts = append(runnerOpts, bt.WithStrategy(strat))
	}
	optimizer := bt.NewOptimizer(bt.NewBacktestRunner(runnerOpts...))

	fmt.Println("=== Phase 2a: Coarse Search ===")
	coarseResults, err := optimizer.Optimize(context.Background(), baseInput, ranges, bt.OptimizeConfig{
		MaxEvals: *coarseMaxEvals,
		TopN:     *coarseTop,
		Seed:     *coarseSeed,
		Workers:  *coarseWorkers,
	})
	if err != nil {
		return fmt.Errorf("coarse search: %w", err)
	}

	fmt.Printf("coarse search: top %d results\n", len(coarseResults))
	for i, r := range coarseResults {
		fmt.Printf(
			"  %d) sharpe=%.4f maxDD=%.2f%% return=%+.2f%% trades=%d params=%v\n",
			i+1,
			r.Summary.SharpeRatio,
			r.Summary.MaxDrawdown*100,
			r.Summary.TotalReturn*100,
			r.Summary.TotalTrades,
			r.Params,
		)
	}

	fmt.Println("\n=== Phase 2b: Refinement (Local Neighborhood Search) ===")
	refinedResults, err := optimizer.Refine(context.Background(), baseInput, coarseResults, ranges, bt.RefineConfig{
		TopN:     *refineTopN,
		StepDiv:  *refineStepDiv,
		MaxEvals: *refineMaxEvals,
		Workers:  *coarseWorkers,
	})
	if err != nil {
		return fmt.Errorf("refinement: %w", err)
	}

	topPrint := len(refinedResults)
	if topPrint > *coarseTop {
		topPrint = *coarseTop
	}
	fmt.Printf("refined: top %d results\n", topPrint)
	for i := 0; i < topPrint; i++ {
		r := refinedResults[i]
		fmt.Printf(
			"  %d) sharpe=%.4f maxDD=%.2f%% return=%+.2f%% trades=%d params=%v\n",
			i+1,
			r.Summary.SharpeRatio,
			r.Summary.MaxDrawdown*100,
			r.Summary.TotalReturn*100,
			r.Summary.TotalTrades,
			r.Params,
		)
	}

	return nil
}

func downloadCommand(args []string) error {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	var (
		symbol   = fs.String("symbol", "BTC_JPY", "currency pair (e.g. BTC_JPY)")
		interval = fs.String("interval", "PT15M", "candle interval")
		fromDate = fs.String("from", "", "start date (YYYY-MM-DD)")
		update   = fs.Bool("update", false, "incremental update from latest csv timestamp")
		output   = fs.String("output", "", "output csv path (default: data/candles_{symbol}_{interval}.csv)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *symbol == "" {
		return fmt.Errorf("--symbol is required")
	}

	outPath := *output
	if outPath == "" {
		outPath = filepath.Join("data", fmt.Sprintf("candles_%s_%s.csv", *symbol, *interval))
	}

	_ = godotenv.Load()
	cfg := config.Load()
	client := rakuten.NewRESTClient(cfg.Rakuten.BaseURL, cfg.Rakuten.APIKey, cfg.Rakuten.APISecret)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	symbolID, err := resolveSymbolID(ctx, client, *symbol)
	if err != nil {
		return err
	}

	var fromTs int64
	if *update {
		if _, statErr := os.Stat(outPath); statErr == nil {
			latest, latestErr := csvinfra.LatestTimestamp(outPath)
			if latestErr != nil {
				return fmt.Errorf("read latest timestamp: %w", latestErr)
			}
			fromTs = latest + 1
		}
	}
	if fromTs == 0 {
		fromTs, err = parseDateStart(*fromDate)
		if err != nil {
			return err
		}
		if fromTs == 0 {
			return fmt.Errorf("--from is required for initial download")
		}
	}
	toTs := time.Now().UnixMilli()

	existing := csvinfra.CandleFile{
		Symbol:   *symbol,
		SymbolID: symbolID,
		Interval: *interval,
	}
	if _, statErr := os.Stat(outPath); statErr == nil {
		loaded, loadErr := csvinfra.LoadCandles(outPath)
		if loadErr != nil {
			return loadErr
		}
		existing = *loaded
	}

	collected, err := fetchCandlesRange(ctx, client, symbolID, *interval, fromTs, toTs)
	if err != nil {
		return err
	}
	merged := mergeCandles(existing.Candles, collected)
	existing.Candles = merged
	existing.Symbol = *symbol
	existing.SymbolID = symbolID
	existing.Interval = *interval

	if err := csvinfra.SaveCandles(outPath, existing); err != nil {
		return err
	}
	slog.Info("download complete", "symbol", *symbol, "interval", *interval, "rows", len(merged), "output", outPath)
	return nil
}

func resolveSymbolID(ctx context.Context, client *rakuten.RESTClient, symbol string) (int64, error) {
	symbols, err := client.GetSymbols(ctx)
	if err != nil {
		return 0, fmt.Errorf("get symbols: %w", err)
	}
	for _, s := range symbols {
		if s.CurrencyPair == symbol {
			return s.ID, nil
		}
	}
	return 0, fmt.Errorf("symbol not found: %s", symbol)
}

func fetchCandlesRange(ctx context.Context, client *rakuten.RESTClient, symbolID int64, interval string, fromTs, toTs int64) ([]entity.Candle, error) {
	if fromTs <= 0 || toTs <= 0 || fromTs > toTs {
		return nil, fmt.Errorf("invalid range")
	}
	collected := make([]entity.Candle, 0, 2048)
	cursor := fromTs
	for cursor <= toTs {
		df := cursor
		dt := toTs
		resp, err := client.GetCandlestick(ctx, symbolID, interval, &df, &dt)
		if err != nil {
			return nil, fmt.Errorf("get candlestick: %w", err)
		}
		if resp == nil || len(resp.Candlesticks) == 0 {
			break
		}

		maxTs := cursor
		for _, c := range resp.Candlesticks {
			if c.Time < fromTs || c.Time > toTs {
				continue
			}
			collected = append(collected, c)
			if c.Time > maxTs {
				maxTs = c.Time
			}
		}
		if maxTs < cursor {
			break
		}
		cursor = maxTs + 1
	}
	return collected, nil
}

func mergeCandles(existing, incoming []entity.Candle) []entity.Candle {
	byTime := make(map[int64]entity.Candle, len(existing)+len(incoming))
	for _, c := range existing {
		byTime[c.Time] = c
	}
	for _, c := range incoming {
		byTime[c.Time] = c
	}
	out := make([]entity.Candle, 0, len(byTime))
	for _, c := range byTime {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b entity.Candle) int {
		if a.Time < b.Time {
			return -1
		}
		if a.Time > b.Time {
			return 1
		}
		return 0
	})
	return out
}

// buildRunInput assembles a bt.RunInput from parsed flags and an optional
// StrategyProfile. When a profile is supplied, its risk values (stop-loss,
// take-profit, max position, max daily loss) become the base and any flag
// the user *explicitly* set on the command line overrides that base. This
// implements the precedence rule in spec §8.2: profile first, then overlays
// from explicit flags.
func buildRunInput(f runFlags, profile *entity.StrategyProfile) (bt.RunInput, error) {
	primary, err := csvinfra.LoadCandles(f.DataPath)
	if err != nil {
		return bt.RunInput{}, fmt.Errorf("load primary csv: %w", err)
	}
	var higherCandles []entity.Candle
	if f.DataHTFPath != "" {
		htf, err := csvinfra.LoadCandles(f.DataHTFPath)
		if err != nil {
			return bt.RunInput{}, fmt.Errorf("load higher tf csv: %w", err)
		}
		higherCandles = htf.Candles
	}

	fromTs, err := parseDateStart(f.FromDate)
	if err != nil {
		return bt.RunInput{}, err
	}
	toTs, err := parseDateEnd(f.ToDate)
	if err != nil {
		return bt.RunInput{}, err
	}
	if fromTs == 0 && len(primary.Candles) > 0 {
		fromTs = primary.Candles[0].Time
	}
	if toTs == 0 && len(primary.Candles) > 0 {
		toTs = primary.Candles[len(primary.Candles)-1].Time
	}

	// Base values come from the flags (which already carry the Go defaults).
	stopLoss := f.StopLoss
	takeProfit := f.TakeProfit
	maxPositionAmount := 1_000_000_000.0
	maxDailyLoss := 1_000_000_000.0

	// Overlay profile risk values where the user did NOT explicitly set the
	// corresponding flag.
	if profile != nil {
		if !f.Set["stop-loss"] && profile.Risk.StopLossPercent > 0 {
			stopLoss = profile.Risk.StopLossPercent
		}
		if !f.Set["take-profit"] && profile.Risk.TakeProfitPercent > 0 {
			takeProfit = profile.Risk.TakeProfitPercent
		}
		if profile.Risk.MaxPositionAmount > 0 {
			maxPositionAmount = profile.Risk.MaxPositionAmount
		}
		if profile.Risk.MaxDailyLoss > 0 {
			maxDailyLoss = profile.Risk.MaxDailyLoss
		}
	}

	cfg := entity.BacktestConfig{
		Symbol:           primary.Symbol,
		SymbolID:         primary.SymbolID,
		PrimaryInterval:  primary.Interval,
		HigherTFInterval: "PT1H",
		FromTimestamp:    fromTs,
		ToTimestamp:      toTs,
		InitialBalance:   f.InitialBalance,
		SpreadPercent:    f.Spread,
		DailyCarryCost:   f.CarryingCost,
		SlippagePercent:  f.Slippage,
	}
	if len(higherCandles) == 0 {
		cfg.HigherTFInterval = ""
	}

	return bt.RunInput{
		Config:         cfg,
		TradeAmount:    f.TradeAmount,
		PrimaryCandles: primary.Candles,
		HigherCandles:  higherCandles,
		RiskConfig: entity.RiskConfig{
			MaxPositionAmount:    maxPositionAmount,
			MaxDailyLoss:         maxDailyLoss,
			StopLossPercent:      stopLoss,
			TakeProfitPercent:    takeProfit,
			InitialCapital:       f.InitialBalance,
			MaxConsecutiveLosses: 0,
			CooldownMinutes:      0,
		},
	}, nil
}

func parseParamRange(spec string) (bt.ParamRange, error) {
	parts := strings.SplitN(spec, "=", 2)
	if len(parts) != 2 {
		return bt.ParamRange{}, fmt.Errorf("invalid --param format: %s", spec)
	}
	name := strings.TrimSpace(parts[0])
	rangeParts := strings.Split(parts[1], ":")
	if len(rangeParts) != 3 {
		return bt.ParamRange{}, fmt.Errorf("invalid --param range format: %s", spec)
	}
	min, err := strconv.ParseFloat(strings.TrimSpace(rangeParts[0]), 64)
	if err != nil {
		return bt.ParamRange{}, fmt.Errorf("invalid min in --param %s: %w", spec, err)
	}
	max, err := strconv.ParseFloat(strings.TrimSpace(rangeParts[1]), 64)
	if err != nil {
		return bt.ParamRange{}, fmt.Errorf("invalid max in --param %s: %w", spec, err)
	}
	step, err := strconv.ParseFloat(strings.TrimSpace(rangeParts[2]), 64)
	if err != nil {
		return bt.ParamRange{}, fmt.Errorf("invalid step in --param %s: %w", spec, err)
	}
	return bt.ParamRange{Name: name, Min: min, Max: max, Step: step}, nil
}

func parseDateStart(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	loc, _ := time.LoadLocation("Asia/Tokyo")
	t, err := time.ParseInLocation("2006-01-02", v, loc)
	if err != nil {
		return 0, fmt.Errorf("invalid --from date: %w", err)
	}
	return t.UnixMilli(), nil
}

func parseDateEnd(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	loc, _ := time.LoadLocation("Asia/Tokyo")
	t, err := time.ParseInLocation("2006-01-02", v, loc)
	if err != nil {
		return 0, fmt.Errorf("invalid --to date: %w", err)
	}
	t = t.Add(24*time.Hour - time.Millisecond)
	return t.UnixMilli(), nil
}

func printSummary(summary entity.BacktestSummary) {
	fmt.Println("=== Backtest Result ===")
	fmt.Printf("Period:           %s ~ %s\n", formatDate(summary.PeriodFrom), formatDate(summary.PeriodTo))
	fmt.Printf("Initial Balance:  ¥%s\n", comma(int64(summary.InitialBalance)))
	fmt.Printf("Final Balance:    ¥%s\n", comma(int64(summary.FinalBalance)))
	fmt.Printf("Total Return:     %+0.2f%%\n", summary.TotalReturn*100)
	fmt.Printf("Total Trades:     %d\n", summary.TotalTrades)
	fmt.Printf("Win Rate:         %.1f%% (%dW / %dL)\n", summary.WinRate, summary.WinTrades, summary.LossTrades)
	fmt.Printf("Profit Factor:    %.2f\n", summary.ProfitFactor)
	fmt.Printf("Max Drawdown:     -%.2f%% (¥%s)\n", summary.MaxDrawdown*100, comma(int64(summary.MaxDrawdownBalance)))
	fmt.Printf("Sharpe Ratio:     %.2f\n", summary.SharpeRatio)
	fmt.Printf("Avg Hold Time:    %s\n", formatDurationSeconds(summary.AvgHoldSeconds))
	fmt.Printf("Carrying Cost:    ¥%s\n", comma(int64(summary.TotalCarryingCost)))
	fmt.Printf("Spread Cost:      ¥%s\n", comma(int64(summary.TotalSpreadCost)))
}

func formatDate(ts int64) string {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	return time.UnixMilli(ts).In(loc).Format("2006-01-02")
}

func formatDurationSeconds(sec int64) string {
	if sec <= 0 {
		return "0m"
	}
	d := time.Duration(sec) * time.Second
	h := int64(d.Hours())
	m := int64(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func comma(v int64) string {
	s := strconv.FormatInt(v, 10)
	n := len(s)
	if n <= 3 {
		return s
	}
	out := make([]byte, 0, n+n/3)
	for i, c := range []byte(s) {
		if i > 0 && (n-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func writeTradesCSV(path string, trades []entity.BacktestTradeRecord) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{
		"trade_id", "entry_time", "exit_time", "side", "entry_price", "exit_price",
		"amount", "pnl", "pnl_percent", "carrying_cost", "spread_cost", "reason_entry", "reason_exit",
	}); err != nil {
		return err
	}
	loc, _ := time.LoadLocation("Asia/Tokyo")
	for _, tr := range trades {
		if err := w.Write([]string{
			strconv.FormatInt(tr.TradeID, 10),
			time.UnixMilli(tr.EntryTime).In(loc).Format(time.RFC3339),
			time.UnixMilli(tr.ExitTime).In(loc).Format(time.RFC3339),
			tr.Side,
			strconv.FormatFloat(tr.EntryPrice, 'f', -1, 64),
			strconv.FormatFloat(tr.ExitPrice, 'f', -1, 64),
			strconv.FormatFloat(tr.Amount, 'f', -1, 64),
			strconv.FormatFloat(tr.PnL, 'f', -1, 64),
			strconv.FormatFloat(tr.PnLPercent, 'f', -1, 64),
			strconv.FormatFloat(tr.CarryingCost, 'f', -1, 64),
			strconv.FormatFloat(tr.SpreadCost, 'f', -1, 64),
			tr.ReasonEntry,
			tr.ReasonExit,
		}); err != nil {
			return err
		}
	}
	return w.Error()
}
