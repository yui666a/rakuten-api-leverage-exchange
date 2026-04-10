package api

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api/handler"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// PipelineController はTrading Pipelineの開始/停止を制御するインターフェース。
type PipelineController interface {
	Start()
	Stop()
	Running() bool
}

type Dependencies struct {
	RiskManager         *usecase.RiskManager
	StanceResolver      *usecase.RuleBasedStanceResolver
	IndicatorCalculator *usecase.IndicatorCalculator
	MarketDataService   *usecase.MarketDataService
	RealtimeHub         *usecase.RealtimeHub
	OrderClient         repository.OrderClient
	OrderExecutor       *usecase.OrderExecutor
	Pipeline            PipelineController
	RESTClient          *rakuten.RESTClient
	ClientOrderRepo     repository.ClientOrderRepository
}

func NewRouter(deps Dependencies) *gin.Engine {
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://localhost:33000"},
		AllowMethods:     []string{"GET", "PUT", "POST", "DELETE"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
	}))

	v1 := r.Group("/api/v1")

	statusHandler := handler.NewStatusHandler(deps.RiskManager)
	v1.GET("/status", statusHandler.GetStatus)
	botHandler := handler.NewBotHandler(deps.RiskManager, deps.RealtimeHub, deps.Pipeline)
	v1.POST("/start", botHandler.Start)
	v1.POST("/stop", botHandler.Stop)

	riskHandler := handler.NewRiskHandler(deps.RiskManager, deps.RealtimeHub)
	v1.GET("/config", riskHandler.GetConfig)
	v1.PUT("/config", riskHandler.UpdateConfig)
	v1.GET("/pnl", riskHandler.GetPnL)

	strategyHandler := handler.NewStrategyHandler(deps.StanceResolver)
	v1.GET("/strategy", strategyHandler.GetStrategy)
	v1.PUT("/strategy", strategyHandler.SetStrategy)
	v1.DELETE("/strategy/override", strategyHandler.DeleteOverride)

	indicatorHandler := handler.NewIndicatorHandler(deps.IndicatorCalculator)
	v1.GET("/indicators/:symbol", indicatorHandler.GetIndicators)

	if deps.MarketDataService != nil {
		candleHandler := handler.NewCandleHandler(deps.MarketDataService)
		v1.GET("/candles/:symbol", candleHandler.GetCandles)

		realtimeHandler := handler.NewRealtimeHandler(deps.MarketDataService, deps.RiskManager, deps.RealtimeHub)
		v1.GET("/ws", realtimeHandler.Stream)
	}

	if deps.OrderClient != nil {
		positionHandler := handler.NewPositionHandler(deps.OrderClient)
		v1.GET("/positions", positionHandler.GetPositions)

		tradeHandler := handler.NewTradeHandler(deps.OrderClient)
		v1.GET("/trades", tradeHandler.GetTrades)
	}

	if deps.MarketDataService != nil {
		tickerHandler := handler.NewTickerHandler(deps.MarketDataService)
		v1.GET("/ticker", tickerHandler.GetTicker)
	}

	if deps.RESTClient != nil {
		orderbookHandler := handler.NewOrderbookHandler(deps.RESTClient)
		v1.GET("/orderbook", orderbookHandler.GetOrderbook)
	}

	if deps.OrderExecutor != nil && deps.ClientOrderRepo != nil {
		orderHandler := handler.NewOrderHandler(deps.OrderExecutor, deps.ClientOrderRepo)
		v1.POST("/orders", orderHandler.CreateOrder)
	}

	return r
}
