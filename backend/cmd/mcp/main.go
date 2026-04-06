package main

import (
	"log"
	"time"

	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/server"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/config"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/llm"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	mcpserver "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/mcp"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()

	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		log.Fatal("failed to open database:", err)
	}
	defer db.Close()

	if err := database.RunMigrations(db); err != nil {
		log.Fatal("failed to run migrations:", err)
	}

	restClient := rakuten.NewRESTClient(cfg.Rakuten.BaseURL, cfg.Rakuten.APIKey, cfg.Rakuten.APISecret)
	claudeClient := llm.NewClaudeClient(cfg.LLM.APIKey, cfg.LLM.Model, cfg.LLM.MaxTokens)
	marketDataRepo := database.NewMarketDataRepo(db)

	indicatorCalc := usecase.NewIndicatorCalculator(marketDataRepo)
	llmSvc := usecase.NewLLMService(claudeClient, time.Duration(cfg.LLM.CacheTTLMin)*time.Minute)
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: cfg.Risk.MaxPositionAmount,
		MaxDailyLoss:      cfg.Risk.MaxDailyLoss,
		StopLossPercent:   cfg.Risk.StopLossPercent,
		InitialCapital:    cfg.Risk.InitialCapital,
	})

	s := mcpserver.NewServer(mcpserver.Dependencies{
		RiskManager:         riskMgr,
		LLMService:          llmSvc,
		IndicatorCalculator: indicatorCalc,
		OrderClient:         restClient,
	})

	if err := server.ServeStdio(s); err != nil {
		log.Fatal("MCP server error:", err)
	}
}
