package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
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
		log.Println("no .env file found, using environment variables")
	}

	cfg := config.Load()

	// --- Database ---
	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		log.Fatal("failed to open database:", err)
	}
	defer db.Close()

	if err := database.RunMigrations(db); err != nil {
		log.Fatal("failed to run migrations:", err)
	}

	// --- Infrastructure ---
	restClient := rakuten.NewRESTClient(cfg.Rakuten.BaseURL, cfg.Rakuten.APIKey, cfg.Rakuten.APISecret)
	wsClient := rakuten.NewWSClient(cfg.Rakuten.WSURL)
	claudeClient := llm.NewClaudeClient(cfg.LLM.APIKey, cfg.LLM.Model, cfg.LLM.MaxTokens)
	marketDataRepo := database.NewMarketDataRepo(db)

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

	if err := bootstrapCandles(context.Background(), restClient, marketDataSvc, 7, "15min", "PT15M", 500); err != nil {
		log.Printf("initial candle bootstrap failed: %v", err)
	}

	// --- REST API ---
	router := api.NewRouter(api.Dependencies{
		RiskManager:         riskMgr,
		LLMService:          llmSvc,
		IndicatorCalculator: indicatorCalc,
		MarketDataService:   marketDataSvc,
		RealtimeHub:         realtimeHub,
		OrderClient:         restClient,
	})

	// --- Graceful Shutdown ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// REST API server
	go func() {
		log.Printf("REST API starting on :%s", cfg.Server.Port)
		if err := router.Run(":" + cfg.Server.Port); err != nil {
			log.Printf("REST API server error: %v", err)
		}
	}()

	log.Println("Trading Engine started")
	log.Printf("Config: maxPosition=%.0f, maxDailyLoss=%.0f, stopLoss=%.1f%%, capital=%.0f",
		cfg.Risk.MaxPositionAmount, cfg.Risk.MaxDailyLoss, cfg.Risk.StopLossPercent, cfg.Risk.InitialCapital)

	go startMarketRelay(ctx, wsClient, marketDataSvc, realtimeHub, 7)

	// コンポーネントの参照を保持（Trading Pipeline実装時に使用）
	_ = strategyEngine
	_ = orderExecutor

	// TODO: WebSocket接続 → Ticker受信ループ → 指標計算 → 戦略判定 → 注文実行
	// 現時点ではREST APIサーバーとして稼働し、Trading Pipelineは次のイテレーションで実装

	// シグナル待機
	select {
	case sig := <-sigCh:
		log.Printf("received signal %s, shutting down...", sig)
		cancel()
	case <-ctx.Done():
	}

	log.Println("Trading Engine stopped")
}

func startMarketRelay(ctx context.Context, wsClient *rakuten.WSClient, marketDataSvc *usecase.MarketDataService, realtimeHub *usecase.RealtimeHub, symbolID int64) {
	if wsClient == nil || marketDataSvc == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			_ = wsClient.Close()
			return
		default:
		}

		msgCh, err := wsClient.Connect(ctx)
		if err != nil {
			log.Printf("market websocket connect failed: %v", err)
			waitForReconnect(ctx)
			continue
		}

		for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
			if err := wsClient.Subscribe(ctx, symbolID, dataType); err != nil {
				log.Printf("market websocket subscribe failed: %v", err)
				_ = wsClient.Close()
				waitForReconnect(ctx)
				continue
			}
		}

		log.Printf("market websocket subscribed: symbol=%d", symbolID)

		reconnect := false
		for !reconnect {
			select {
			case <-ctx.Done():
				_ = wsClient.Close()
				return
			case raw, ok := <-msgCh:
				if !ok {
					reconnect = true
					break
				}

				handleMarketMessage(ctx, raw, marketDataSvc, realtimeHub)
			}
		}

		log.Println("market websocket disconnected, reconnecting")
		_ = wsClient.Close()
		waitForReconnect(ctx)
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

func handleMarketMessage(ctx context.Context, raw []byte, marketDataSvc *usecase.MarketDataService, realtimeHub *usecase.RealtimeHub) {
	switch {
	case bytes.Contains(raw, []byte(`"asks"`)):
		var r rawOrderbook
		if err := json.Unmarshal(raw, &r); err != nil {
			log.Printf("market websocket orderbook decode failed: %v", err)
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
	case bytes.Contains(raw, []byte(`"trades"`)):
		var trades entity.MarketTradesResponse
		if err := json.Unmarshal(raw, &trades); err != nil {
			log.Printf("market websocket trades decode failed: %v", err)
			return
		}
		if realtimeHub != nil {
			_ = realtimeHub.PublishData("market_trades", trades.SymbolID, trades)
		}
	default:
		var r rawTicker
		if err := json.Unmarshal(raw, &r); err != nil {
			log.Printf("market websocket ticker decode failed: %v", err)
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

func waitForReconnect(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
	}
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

	existing, err := marketDataSvc.GetCandles(ctx, symbolID, internalInterval, 1)
	if err == nil && len(existing) > 0 {
		log.Printf("skip candle bootstrap: symbol=%d interval=%s already has data", symbolID, internalInterval)
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
		log.Printf("candle bootstrap returned no candles: symbol=%d interval=%s", symbolID, internalInterval)
		return nil
	}

	if err := marketDataSvc.SaveCandles(ctx, symbolID, internalInterval, candles); err != nil {
		return err
	}

	log.Printf("bootstrapped %d candles: symbol=%d interval=%s", len(candles), symbolID, internalInterval)
	return nil
}
