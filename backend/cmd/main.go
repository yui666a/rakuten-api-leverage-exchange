package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/config"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	backtestinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	backtestuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

func main() {
	if err := godotenv.Load(); err != nil {
		slog.Info("no .env file found, using environment variables")
	}

	cfg := config.Load()

	// --- Database ---
	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := database.RunMigrations(db); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// --- Infrastructure ---
	restClient := rakuten.NewRESTClient(cfg.Rakuten.BaseURL, cfg.Rakuten.APIKey, cfg.Rakuten.APISecret)
	wsClient := rakuten.NewWSClient(cfg.Rakuten.WSURL)
	marketDataRepo := database.NewMarketDataRepo(db)
	tradeHistoryRepo := database.NewTradeHistoryRepo(db)
	riskStateRepo := database.NewRiskStateRepo(db)

	// --- Usecase ---
	marketDataSvc := usecase.NewMarketDataService(marketDataRepo)
	realtimeHub := usecase.NewRealtimeHub()
	marketDataSvc.SetRealtimeHub(realtimeHub)
	indicatorCalc := usecase.NewIndicatorCalculator(marketDataRepo)
	stanceOverrideRepo := database.NewStanceOverrideRepo(db)
	clientOrderRepo := database.NewClientOrderRepo(db)
	backtestResultRepo := backtestinfra.NewResultRepository(db)
	stanceResolver := usecase.NewRuleBasedStanceResolver(stanceOverrideRepo)
	strategyEngine := usecase.NewStrategyEngine(stanceResolver)
	// The StrategyRegistry lives in the strategy package and is exercised by
	// its own unit tests. It is intentionally not wired here yet because no
	// downstream code consumes it; leaving dead infrastructure at the
	// composition root would be misleading. It will be wired in the PR that
	// introduces CLI/API strategy-profile selection.
	defaultStrategy := strategyuc.NewDefaultStrategy(strategyEngine)
	backtestRunner := backtestuc.NewBacktestRunner()
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount:     cfg.Risk.MaxPositionAmount,
		MaxDailyLoss:          cfg.Risk.MaxDailyLoss,
		StopLossPercent:       cfg.Risk.StopLossPercent,
		StopLossATRMultiplier: cfg.Risk.StopLossATRMultiplier,
		TakeProfitPercent:     cfg.Risk.TakeProfitPercent,
		InitialCapital:        cfg.Risk.InitialCapital,
		MaxConsecutiveLosses:  cfg.Risk.MaxConsecutiveLosses,
		CooldownMinutes:       cfg.Risk.CooldownMinutes,
	})
	orderExecutor := usecase.NewOrderExecutor(restClient, riskMgr)

	symbolID := cfg.Trading.SymbolID

	if err := bootstrapCandles(context.Background(), restClient, marketDataSvc, symbolID, "PT15M", 500); err != nil {
		slog.Warn("initial candle bootstrap failed", "error", err)
	}

	// --- Risk State Restore ---
	restoreRiskState(context.Background(), riskStateRepo, riskMgr)
	runBacktestRetentionCleanup(context.Background(), backtestResultRepo, cfg.Backtest.RetentionDays)

	// --- Trading Pipeline (Event-Driven) ---
	pipeline := NewEventDrivenPipeline(
		EventDrivenPipelineConfig{
			SymbolID:          symbolID,
			StateSyncInterval: time.Duration(cfg.Trading.StateSyncIntervalSec) * time.Second,
			TradeAmount:       cfg.Trading.TradeAmount,
			MinConfidence:     cfg.Trading.MinConfidence,
			StopLossPercent:   cfg.Risk.StopLossPercent,
			TakeProfitPercent: cfg.Risk.TakeProfitPercent,
		},
		restClient,
		restClient, // SymbolFetcher
		marketDataSvc,
		defaultStrategy,
		riskMgr,
		tradeHistoryRepo,
		riskStateRepo,
	)

	// 初期シンボルの baseStepAmount / minOrderAmount をロード
	pipeline.mu.Lock()
	pipeline.loadSymbolMeta(context.Background(), symbolID)
	pipeline.mu.Unlock()

	// 起動時にポジション・残高を同期
	pipeline.syncStateInitial(context.Background())

	// --- Graceful Shutdown context ---
	// NewRouter より先に ctx/cancel を定義する。onSymbolSwitch クロージャが ctx を
	// キャプチャし、NewRouter に OnSymbolSwitch として渡すため。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Symbol Switch channel + callback ---
	// symbolSwitchCh は pipeline 側から startMarketRelay に切替を伝える。
	// バッファ1の上書き方式: 古い値が取り残されていたら drain して新しい値を入れる。
	symbolSwitchCh := make(chan [2]int64, 1)

	onSymbolSwitch := func(oldID, newID int64) {
		// 新シンボルのローソク足を bootstrap（main の ctx を使う）
		if err := bootstrapCandles(ctx, restClient, marketDataSvc, newID, "PT15M", 500); err != nil {
			slog.Warn("candle bootstrap for new symbol failed", "symbolID", newID, "error", err)
		}

		// 上書き方式: 古い値を drain してから送信。
		// SwitchSymbol は pipeline の switchMu でシリアライズされているため、
		// この関数が並行実行されることはない（drain + send の atomicity は不要）。
		select {
		case <-symbolSwitchCh:
		default:
		}
		select {
		case symbolSwitchCh <- [2]int64{oldID, newID}:
		case <-ctx.Done():
		}
	}

	// --- REST API ---
	dailyPnLCalc := usecase.NewDailyPnLCalculator(restClient, 10*time.Second)

	router := api.NewRouter(api.Dependencies{
		RiskManager:         riskMgr,
		StanceResolver:      stanceResolver,
		IndicatorCalculator: indicatorCalc,
		MarketDataService:   marketDataSvc,
		RealtimeHub:         realtimeHub,
		OrderClient:         restClient,
		OrderExecutor:       orderExecutor,
		Pipeline:            pipeline,
		RESTClient:          restClient,
		ClientOrderRepo:     clientOrderRepo,
		BacktestRunner:      backtestRunner,
		BacktestResultRepo:  backtestResultRepo,
		OnSymbolSwitch:      onSymbolSwitch,
		DailyPnLCalculator:  dailyPnLCalc,
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// REST API server
	go func() {
		slog.Info("REST API starting", "port", cfg.Server.Port)
		if err := router.Run(":" + cfg.Server.Port); err != nil {
			slog.Error("REST API server error", "error", err)
		}
	}()

	slog.Info("Trading Engine started",
		"maxPosition", cfg.Risk.MaxPositionAmount,
		"maxDailyLoss", cfg.Risk.MaxDailyLoss,
		"stopLoss", cfg.Risk.StopLossPercent,
		"capital", cfg.Risk.InitialCapital,
	)

	go startMarketRelay(ctx, wsClient, marketDataSvc, realtimeHub, symbolID, symbolSwitchCh)
	go startDailyLossReset(ctx, riskMgr)
	go startBacktestRetentionCleanup(ctx, backtestResultRepo, cfg.Backtest.RetentionDays)
	// 残高・ポジションの定期同期は auto-trading の start/stop とは独立して常時回す。
	// これにより自動売買停止中でも画面の残高が楽天の実残高に追随し、起動直後に 20010
	// で失敗したケースも 15 秒ごとに再試行される。
	go pipeline.runStateSyncLoop(ctx)

	slog.Info("Trading pipeline ready",
		"tradeAmount", cfg.Trading.TradeAmount,
		"intervalSec", cfg.Trading.PipelineIntervalSec,
		"stateSyncIntervalSec", cfg.Trading.StateSyncIntervalSec,
	)
	slog.Info("Trading pipeline ready. Use POST /api/v1/start to begin auto-trading.")

	// シグナル待機
	select {
	case sig := <-sigCh:
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
	case <-ctx.Done():
	}

	slog.Info("Trading Engine stopped")
}

const (
	wsMaxSessionDuration = 110 * time.Minute // 2時間制限の10分前に事前再接続
	wsInitialBackoff     = 1 * time.Second
	wsMaxBackoff         = 60 * time.Second
)

func startMarketRelay(
	ctx context.Context,
	wsClient *rakuten.WSClient,
	marketDataSvc *usecase.MarketDataService,
	realtimeHub *usecase.RealtimeHub,
	initialSymbolID int64,
	symbolSwitchCh <-chan [2]int64,
) {
	if wsClient == nil || marketDataSvc == nil {
		return
	}

	currentSymbolID := initialSymbolID
	backoff := wsInitialBackoff

	for {
		select {
		case <-ctx.Done():
			_ = wsClient.Close()
			return
		default:
		}

		msgCh, err := wsClient.Connect(ctx)
		if err != nil {
			slog.Warn("market websocket connect failed", "error", err, "retryIn", backoff)
			waitFor(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		// Subscribe — 失敗時は Close して外側ループで reconnect する
		subscribeOK := true
		for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
			if err := wsClient.Subscribe(ctx, currentSymbolID, dataType); err != nil {
				slog.Warn("market websocket subscribe failed", "dataType", dataType, "error", err)
				subscribeOK = false
				break
			}
		}
		if !subscribeOK {
			_ = wsClient.Close()
			waitFor(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		slog.Info("market websocket subscribed", "symbolID", currentSymbolID)
		backoff = wsInitialBackoff // 接続成功でバックオフリセット

		// 2時間制限の事前再接続タイマー
		sessionTimer := time.NewTimer(wsMaxSessionDuration)

		reconnect := false
		for !reconnect {
			select {
			case <-ctx.Done():
				sessionTimer.Stop()
				_ = wsClient.Close()
				return
			case <-sessionTimer.C:
				slog.Info("market websocket session approaching 2h limit, reconnecting proactively")
				reconnect = true
			case ids := <-symbolSwitchCh:
				oldID, newID := ids[0], ids[1]
				slog.Info("switching websocket symbol subscription", "from", oldID, "to", newID)

				// Unsubscribe（エラーはログのみ — 古いシンボルが既に無効でも続行する）
				for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
					if err := wsClient.Unsubscribe(ctx, oldID, dataType); err != nil {
						slog.Warn("market websocket unsubscribe failed", "dataType", dataType, "error", err)
					}
				}

				// Subscribe（エラー時は reconnect。currentSymbolID は newID に進める）
				switchOK := true
				for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
					if err := wsClient.Subscribe(ctx, newID, dataType); err != nil {
						slog.Error("market websocket re-subscribe failed, will reconnect", "dataType", dataType, "error", err)
						switchOK = false
						break
					}
				}
				// pipeline 側は既に newID に切り替え済みなので、Subscribe 成否に関わらず
				// currentSymbolID を newID にして reconnect 時に新シンボルで再接続する
				currentSymbolID = newID
				if !switchOK {
					reconnect = true
				}
			case raw, ok := <-msgCh:
				if !ok {
					reconnect = true
					break
				}
				handleMarketMessage(ctx, raw, marketDataSvc, realtimeHub)
			}
		}

		sessionTimer.Stop()
		slog.Info("market websocket disconnected, reconnecting")
		_ = wsClient.Close()
		waitFor(ctx, wsInitialBackoff)
	}
}

// Raw structures for decoding Rakuten WebSocket messages where numeric values
// are delivered as JSON strings (e.g. "12345.67").
type rawTicker struct {
	SymbolID  int64                `json:"symbolId"`
	BestAsk   entity.StringFloat64 `json:"bestAsk"`
	BestBid   entity.StringFloat64 `json:"bestBid"`
	Open      entity.StringFloat64 `json:"open"`
	High      entity.StringFloat64 `json:"high"`
	Low       entity.StringFloat64 `json:"low"`
	Last      entity.StringFloat64 `json:"last"`
	Volume    entity.StringFloat64 `json:"volume"`
	Timestamp int64                `json:"timestamp"`
}

type rawOrderbookEntry struct {
	Price  entity.StringFloat64 `json:"price"`
	Amount entity.StringFloat64 `json:"amount"`
}

type rawMarketTrade struct {
	ID          int64                `json:"id"`
	OrderSide   string               `json:"orderSide"`
	Price       entity.StringFloat64 `json:"price"`
	Amount      entity.StringFloat64 `json:"amount"`
	AssetAmount entity.StringFloat64 `json:"assetAmount"`
	TradedAt    int64                `json:"tradedAt"`
}

type rawMarketTradesResponse struct {
	SymbolID  int64            `json:"symbolId"`
	Trades    []rawMarketTrade `json:"trades"`
	Timestamp int64            `json:"timestamp"`
}

type rawOrderbook struct {
	SymbolID  int64                `json:"symbolId"`
	Asks      []rawOrderbookEntry  `json:"asks"`
	Bids      []rawOrderbookEntry  `json:"bids"`
	BestAsk   entity.StringFloat64 `json:"bestAsk"`
	BestBid   entity.StringFloat64 `json:"bestBid"`
	MidPrice  entity.StringFloat64 `json:"midPrice"`
	Spread    entity.StringFloat64 `json:"spread"`
	Timestamp int64                `json:"timestamp"`
}

// detectMessageType はJSONのトップレベルキーを軽量パースしてメッセージ種別を判定する。
func detectMessageType(raw []byte) string {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "unknown"
	}
	if _, ok := probe["asks"]; ok {
		return "orderbook"
	}
	if _, ok := probe["trades"]; ok {
		return "trades"
	}
	return "ticker"
}

func handleMarketMessage(ctx context.Context, raw []byte, marketDataSvc *usecase.MarketDataService, realtimeHub *usecase.RealtimeHub) {
	if len(raw) == 0 {
		return
	}

	msgType := detectMessageType(raw)
	if msgType == "unknown" {
		slog.Debug("market websocket unknown message, skipping", "raw", string(raw))
		return
	}

	switch msgType {
	case "orderbook":
		var r rawOrderbook
		if err := json.Unmarshal(raw, &r); err != nil {
			slog.Warn("market websocket orderbook decode failed", "error", err)
			return
		}
		asks := make([]entity.OrderbookEntry, len(r.Asks))
		for i, a := range r.Asks {
			asks[i] = entity.OrderbookEntry{Price: a.Price.Float64(), Amount: a.Amount.Float64()}
		}
		bids := make([]entity.OrderbookEntry, len(r.Bids))
		for i, b := range r.Bids {
			bids[i] = entity.OrderbookEntry{Price: b.Price.Float64(), Amount: b.Amount.Float64()}
		}
		orderbook := entity.Orderbook{
			SymbolID:  r.SymbolID,
			Asks:      asks,
			Bids:      bids,
			BestAsk:   r.BestAsk.Float64(),
			BestBid:   r.BestBid.Float64(),
			MidPrice:  r.MidPrice.Float64(),
			Spread:    r.Spread.Float64(),
			Timestamp: r.Timestamp,
		}
		if realtimeHub != nil {
			_ = realtimeHub.PublishData("orderbook", orderbook.SymbolID, orderbook)
		}
	case "trades":
		var r rawMarketTradesResponse
		if err := json.Unmarshal(raw, &r); err != nil {
			slog.Warn("market websocket trades decode failed", "error", err)
			return
		}
		trades := entity.MarketTradesResponse{
			SymbolID:  r.SymbolID,
			Timestamp: r.Timestamp,
			Trades:    make([]entity.MarketTrade, len(r.Trades)),
		}
		for i, t := range r.Trades {
			trades.Trades[i] = entity.MarketTrade{
				ID:          t.ID,
				OrderSide:   t.OrderSide,
				Price:       t.Price.Float64(),
				Amount:      t.Amount.Float64(),
				AssetAmount: t.AssetAmount.Float64(),
				TradedAt:    t.TradedAt,
			}
		}
		if realtimeHub != nil {
			_ = realtimeHub.PublishData("market_trades", trades.SymbolID, trades)
		}
	case "ticker":
		var r rawTicker
		if err := json.Unmarshal(raw, &r); err != nil {
			slog.Warn("market websocket ticker decode failed", "error", err)
			return
		}
		ticker := entity.Ticker{
			SymbolID:  r.SymbolID,
			BestAsk:   r.BestAsk.Float64(),
			BestBid:   r.BestBid.Float64(),
			Open:      r.Open.Float64(),
			High:      r.High.Float64(),
			Low:       r.Low.Float64(),
			Last:      r.Last.Float64(),
			Volume:    r.Volume.Float64(),
			Timestamp: r.Timestamp,
		}
		marketDataSvc.HandleTicker(ctx, ticker)
	}
}

func waitFor(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > wsMaxBackoff {
		return wsMaxBackoff
	}
	return next
}

func bootstrapCandles(
	ctx context.Context,
	restClient *rakuten.RESTClient,
	marketDataSvc *usecase.MarketDataService,
	symbolID int64,
	interval string,
	limit int,
) error {
	if restClient == nil || marketDataSvc == nil {
		return nil
	}

	resp, err := restClient.GetCandlestick(ctx, symbolID, interval, nil, nil)
	if err != nil {
		return err
	}

	candles := resp.Candlesticks
	if limit > 0 && len(candles) > limit {
		candles = candles[len(candles)-limit:]
	}

	if len(candles) == 0 {
		slog.Warn("candle bootstrap returned no candles", "symbolID", symbolID, "interval", interval)
		return nil
	}

	// INSERT OR IGNORE により既存データと重複しないため、毎回全件渡して差分のみ保存される
	if err := marketDataSvc.SaveCandles(ctx, symbolID, interval, candles); err != nil {
		return err
	}

	slog.Info("bootstrapped candles", "count", len(candles), "symbolID", symbolID, "interval", interval)
	return nil
}

func startBacktestRetentionCleanup(ctx context.Context, repo repository.BacktestResultRepository, retentionDays int) {
	if repo == nil {
		return
	}
	if retentionDays <= 0 {
		retentionDays = 180
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runBacktestRetentionCleanup(ctx, repo, retentionDays)
		}
	}
}

func runBacktestRetentionCleanup(ctx context.Context, repo repository.BacktestResultRepository, retentionDays int) {
	if repo == nil {
		return
	}
	if retentionDays <= 0 {
		retentionDays = 180
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	deleted, err := repo.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		slog.Warn("backtest retention cleanup failed", "error", err, "retentionDays", retentionDays)
		return
	}
	if deleted > 0 {
		slog.Info("backtest retention cleanup completed", "deleted", deleted, "retentionDays", retentionDays)
	}
}
