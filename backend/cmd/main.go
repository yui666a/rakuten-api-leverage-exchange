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
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/llm"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
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
	claudeClient := llm.NewClaudeClient(cfg.LLM.APIKey, cfg.LLM.Model, cfg.LLM.MaxTokens)
	marketDataRepo := database.NewMarketDataRepo(db)
	tradeHistoryRepo := database.NewTradeHistoryRepo(db)
	riskStateRepo := database.NewRiskStateRepo(db)

	// --- Usecase ---
	marketDataSvc := usecase.NewMarketDataService(marketDataRepo)
	realtimeHub := usecase.NewRealtimeHub()
	marketDataSvc.SetRealtimeHub(realtimeHub)
	indicatorCalc := usecase.NewIndicatorCalculator(marketDataRepo)
	llmSvc := usecase.NewLLMService(claudeClient, time.Duration(cfg.LLM.CacheTTLMin)*time.Minute)
	strategyEngine := usecase.NewStrategyEngine(llmSvc)
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: cfg.Risk.MaxPositionAmount,
		MaxDailyLoss:      cfg.Risk.MaxDailyLoss,
		StopLossPercent:   cfg.Risk.StopLossPercent,
		InitialCapital:    cfg.Risk.InitialCapital,
	})
	orderExecutor := usecase.NewOrderExecutor(restClient, riskMgr)

	symbolID := cfg.Trading.SymbolID

	if err := bootstrapCandles(context.Background(), restClient, marketDataSvc, symbolID, "15min", "PT15M", 500); err != nil {
		slog.Warn("initial candle bootstrap failed", "error", err)
	}

	// --- Risk State Restore ---
	restoreRiskState(context.Background(), riskStateRepo, riskMgr)

	// --- Trading Pipeline ---
	pipeline := NewTradingPipeline(
		TradingPipelineConfig{
			SymbolID:    symbolID,
			Interval:    time.Duration(cfg.Trading.PipelineIntervalSec) * time.Second,
			TradeAmount: cfg.Trading.TradeAmount,
		},
		restClient,
		marketDataSvc,
		indicatorCalc,
		strategyEngine,
		orderExecutor,
		riskMgr,
		tradeHistoryRepo,
		riskStateRepo,
	)

	// 起動時にポジション・残高を同期
	pipeline.syncStateInitial(context.Background())

	// --- REST API ---
	router := api.NewRouter(api.Dependencies{
		RiskManager:         riskMgr,
		LLMService:          llmSvc,
		IndicatorCalculator: indicatorCalc,
		MarketDataService:   marketDataSvc,
		RealtimeHub:         realtimeHub,
		OrderClient:         restClient,
		Pipeline:            pipeline,
	})

	// --- Graceful Shutdown ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	go startMarketRelay(ctx, wsClient, marketDataSvc, realtimeHub, symbolID)
	go startDailyLossReset(ctx, riskMgr)

	slog.Info("Trading pipeline ready",
		"tradeAmount", cfg.Trading.TradeAmount,
		"intervalSec", cfg.Trading.PipelineIntervalSec,
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

func startMarketRelay(ctx context.Context, wsClient *rakuten.WSClient, marketDataSvc *usecase.MarketDataService, realtimeHub *usecase.RealtimeHub, symbolID int64) {
	if wsClient == nil || marketDataSvc == nil {
		return
	}

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

		for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
			if err := wsClient.Subscribe(ctx, symbolID, dataType); err != nil {
				slog.Warn("market websocket subscribe failed", "error", err)
				_ = wsClient.Close()
				waitFor(ctx, backoff)
				backoff = nextBackoff(backoff)
				continue
			}
		}

		slog.Info("market websocket subscribed", "symbolID", symbolID)
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

type rawOrderbook struct {
	SymbolID  int64               `json:"symbolId"`
	Asks      []rawOrderbookEntry `json:"asks"`
	Bids      []rawOrderbookEntry `json:"bids"`
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
	switch detectMessageType(raw) {
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
		var trades entity.MarketTradesResponse
		if err := json.Unmarshal(raw, &trades); err != nil {
			slog.Warn("market websocket trades decode failed", "error", err)
			return
		}
		if realtimeHub != nil {
			_ = realtimeHub.PublishData("market_trades", trades.SymbolID, trades)
		}
	default:
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
	internalInterval string,
	rakutenCandlestickType string,
	limit int,
) error {
	if restClient == nil || marketDataSvc == nil {
		return nil
	}

	resp, err := restClient.GetCandlestick(ctx, symbolID, rakutenCandlestickType, nil, nil)
	if err != nil {
		return err
	}

	candles := resp.Candlesticks
	if limit > 0 && len(candles) > limit {
		candles = candles[len(candles)-limit:]
	}

	if len(candles) == 0 {
		slog.Warn("candle bootstrap returned no candles", "symbolID", symbolID, "interval", internalInterval)
		return nil
	}

	// INSERT OR IGNORE により既存データと重複しないため、毎回全件渡して差分のみ保存される
	if err := marketDataSvc.SaveCandles(ctx, symbolID, internalInterval, candles); err != nil {
		return err
	}

	slog.Info("bootstrapped candles", "count", len(candles), "symbolID", symbolID, "interval", internalInterval)
	return nil
}
