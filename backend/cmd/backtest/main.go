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
	"time"

	"github.com/joho/godotenv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/config"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

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
	fmt.Println("  go run ./cmd/backtest download ...")
}

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var (
		dataPath       = fs.String("data", "", "primary timeframe CSV path")
		dataHTFPath    = fs.String("data-htf", "", "higher timeframe CSV path")
		fromDate       = fs.String("from", "", "start date (YYYY-MM-DD)")
		toDate         = fs.String("to", "", "end date (YYYY-MM-DD)")
		initialBalance = fs.Float64("initial-balance", 100000, "initial balance in JPY")
		spread         = fs.Float64("spread", 0.1, "spread percent")
		carryingCost   = fs.Float64("carrying-cost", 0.04, "daily carrying cost percent")
		slippage       = fs.Float64("slippage", 0, "slippage percent")
		tradeAmount    = fs.Float64("trade-amount", 0.01, "trade amount")
		outputDir      = fs.String("output", "", "output directory for trades/result")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dataPath == "" {
		return fmt.Errorf("--data is required")
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

	fromTs, err := parseDateStart(*fromDate)
	if err != nil {
		return err
	}
	toTs, err := parseDateEnd(*toDate)
	if err != nil {
		return err
	}
	if fromTs == 0 && len(primary.Candles) > 0 {
		fromTs = primary.Candles[0].Time
	}
	if toTs == 0 && len(primary.Candles) > 0 {
		toTs = primary.Candles[len(primary.Candles)-1].Time
	}

	cfg := entity.BacktestConfig{
		Symbol:           primary.Symbol,
		SymbolID:         primary.SymbolID,
		PrimaryInterval:  primary.Interval,
		HigherTFInterval: "PT1H",
		FromTimestamp:    fromTs,
		ToTimestamp:      toTs,
		InitialBalance:   *initialBalance,
		SpreadPercent:    *spread,
		DailyCarryCost:   *carryingCost,
		SlippagePercent:  *slippage,
	}
	if len(higherCandles) == 0 {
		cfg.HigherTFInterval = ""
	}

	runner := bt.NewBacktestRunner()
	result, err := runner.Run(context.Background(), bt.RunInput{
		Config:         cfg,
		TradeAmount:    *tradeAmount,
		PrimaryCandles: primary.Candles,
		HigherCandles:  higherCandles,
		RiskConfig: entity.RiskConfig{
			MaxPositionAmount:    1_000_000_000,
			MaxDailyLoss:         1_000_000_000,
			StopLossPercent:      5,
			TakeProfitPercent:    10,
			InitialCapital:       *initialBalance,
			MaxConsecutiveLosses: 0,
			CooldownMinutes:      0,
		},
	})
	if err != nil {
		return err
	}

	printSummary(result.Summary)
	if *outputDir != "" {
		if err := os.MkdirAll(*outputDir, 0o755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
		tradesPath := filepath.Join(*outputDir, "trades.csv")
		if err := writeTradesCSV(tradesPath, result.Trades); err != nil {
			return fmt.Errorf("write trades csv: %w", err)
		}
		fmt.Printf("trades csv: %s\n", tradesPath)
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
