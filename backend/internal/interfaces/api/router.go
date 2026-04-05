package api

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api/handler"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type Dependencies struct {
	RiskManager         *usecase.RiskManager
	LLMService          *usecase.LLMService
	IndicatorCalculator *usecase.IndicatorCalculator
	MarketDataService   *usecase.MarketDataService
	OrderClient         repository.OrderClient
}

func NewRouter(deps Dependencies) *gin.Engine {
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "PUT", "POST", "DELETE"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
	}))

	v1 := r.Group("/api/v1")

	statusHandler := handler.NewStatusHandler(deps.RiskManager)
	v1.GET("/status", statusHandler.GetStatus)

	riskHandler := handler.NewRiskHandler(deps.RiskManager)
	v1.GET("/config", riskHandler.GetConfig)
	v1.PUT("/config", riskHandler.UpdateConfig)
	v1.GET("/pnl", riskHandler.GetPnL)

	strategyHandler := handler.NewStrategyHandler(deps.LLMService)
	v1.GET("/strategy", strategyHandler.GetStrategy)

	indicatorHandler := handler.NewIndicatorHandler(deps.IndicatorCalculator)
	v1.GET("/indicators/:symbol", indicatorHandler.GetIndicators)

	if deps.MarketDataService != nil {
		candleHandler := handler.NewCandleHandler(deps.MarketDataService)
		v1.GET("/candles/:symbol", candleHandler.GetCandles)
	}

	if deps.OrderClient != nil {
		positionHandler := handler.NewPositionHandler(deps.OrderClient)
		v1.GET("/positions", positionHandler.GetPositions)
	}

	return r
}
