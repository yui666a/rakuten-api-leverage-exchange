package api

import (
	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api/handler"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type Dependencies struct {
	RiskManager         *usecase.RiskManager
	LLMService          *usecase.LLMService
	IndicatorCalculator *usecase.IndicatorCalculator
}

func NewRouter(deps Dependencies) *gin.Engine {
	r := gin.Default()

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

	return r
}
