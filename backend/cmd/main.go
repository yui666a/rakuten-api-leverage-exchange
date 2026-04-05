package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/config"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/llm"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

func main() {
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

	// --- REST API ---
	router := api.NewRouter(api.Dependencies{
		RiskManager:         riskMgr,
		LLMService:          llmSvc,
		IndicatorCalculator: indicatorCalc,
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

	// コンポーネントの参照を保持（Trading Pipeline実装時に使用）
	_ = marketDataSvc
	_ = strategyEngine
	_ = orderExecutor
	_ = wsClient
	_ = ctx

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
