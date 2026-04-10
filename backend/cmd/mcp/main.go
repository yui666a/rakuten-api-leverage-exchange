package main

import (
	"log"

	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/server"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/config"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
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
	marketDataRepo := database.NewMarketDataRepo(db)
	stanceOverrideRepo := database.NewStanceOverrideRepo(db)

	indicatorCalc := usecase.NewIndicatorCalculator(marketDataRepo)
	stanceResolver := usecase.NewRuleBasedStanceResolver(stanceOverrideRepo)
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: cfg.Risk.MaxPositionAmount,
		MaxDailyLoss:      cfg.Risk.MaxDailyLoss,
		StopLossPercent:   cfg.Risk.StopLossPercent,
		InitialCapital:    cfg.Risk.InitialCapital,
	})

	s := mcpserver.NewServer(mcpserver.Dependencies{
		RiskManager:         riskMgr,
		StanceResolver:      stanceResolver,
		IndicatorCalculator: indicatorCalc,
		OrderClient:         restClient,
	})

	if err := server.ServeStdio(s); err != nil {
		log.Fatal("MCP server error:", err)
	}
}
