package api

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api/handler"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	backtestuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

// PipelineController はTrading Pipelineの開始/停止・銘柄切替を制御するインターフェース。
type PipelineController interface {
	Start()
	Stop()
	Running() bool
	SymbolID() int64
	TradeAmount() float64
	SwitchSymbol(symbolID int64, tradeAmount float64, onSwitch func(oldID, newID int64))
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
	DailyPnLCalculator  *usecase.DailyPnLCalculator
	BacktestRunner      *backtestuc.BacktestRunner
	BacktestResultRepo  repository.BacktestResultRepository
	// MultiPeriodResultRepo is optional; when nil the /backtest/run-multi
	// and /backtest/multi-results endpoints respond with 503.
	MultiPeriodResultRepo repository.MultiPeriodResultRepository
	// WalkForwardResultRepo is optional; when nil the walk-forward endpoint
	// still computes but does not persist, and GET endpoints return 503.
	WalkForwardResultRepo repository.WalkForwardResultRepository
	// OnSymbolSwitch はシンボル切替時に pipeline から呼び出されるコールバック。
	// main 側で WebSocket 購読切替とローソク足 bootstrap を実行する。
	OnSymbolSwitch func(oldID, newID int64)
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

	riskHandler := handler.NewRiskHandler(deps.RiskManager, deps.RealtimeHub, deps.DailyPnLCalculator)
	v1.GET("/config", riskHandler.GetConfig)
	v1.PUT("/config", riskHandler.UpdateConfig)
	v1.GET("/pnl", riskHandler.GetPnL)

	strategyHandler := handler.NewStrategyHandler(deps.StanceResolver)
	// Wire the live snapshot so GET /strategy reflects the pipeline's
	// current-bar stance instead of "insufficient indicator data". Nil-guard
	// each dependency so tests / limited-feature deployments still work.
	if deps.Pipeline != nil && deps.IndicatorCalculator != nil && deps.MarketDataService != nil {
		strategyHandler.WithLiveSnapshot(handler.NewPipelineLiveSnapshot(
			deps.Pipeline, deps.IndicatorCalculator, deps.MarketDataService, "PT15M",
		))
	}
	v1.GET("/strategy", strategyHandler.GetStrategy)
	v1.PUT("/strategy", strategyHandler.SetStrategy)
	v1.DELETE("/strategy/override", strategyHandler.DeleteOverride)

	indicatorHandler := handler.NewIndicatorHandler(deps.IndicatorCalculator)
	v1.GET("/indicators/:symbol", indicatorHandler.GetIndicators)

	if deps.MarketDataService != nil {
		candleHandler := handler.NewCandleHandler(deps.MarketDataService, deps.RESTClient)
		v1.GET("/candles/:symbol", candleHandler.GetCandles)

		realtimeHandler := handler.NewRealtimeHandler(deps.MarketDataService, deps.RiskManager, deps.RealtimeHub)
		v1.GET("/ws", realtimeHandler.Stream)
	}

	if deps.OrderClient != nil {
		positionHandler := handler.NewPositionHandler(deps.OrderClient, deps.OrderExecutor, deps.ClientOrderRepo)
		v1.GET("/positions", positionHandler.GetPositions)
		if deps.OrderExecutor != nil && deps.ClientOrderRepo != nil {
			v1.POST("/positions/:id/close", positionHandler.ClosePosition)
		}

		tradeHandler := handler.NewTradeHandler(deps.OrderClient, deps.RESTClient)
		v1.GET("/trades", tradeHandler.GetTrades)
		v1.GET("/trades/all", tradeHandler.GetAllTrades)
	}

	if deps.MarketDataService != nil {
		tickerHandler := handler.NewTickerHandler(deps.MarketDataService)
		v1.GET("/ticker", tickerHandler.GetTicker)
	}

	if deps.RESTClient != nil {
		orderbookHandler := handler.NewOrderbookHandler(deps.RESTClient, deps.MarketDataService)
		v1.GET("/orderbook", orderbookHandler.GetOrderbook)
		// History endpoint reads persisted snapshots; only useful when the
		// MarketDataService is wired (composition root attaches it). The
		// handler itself nil-guards and returns 503 when missing.
		v1.GET("/orderbook/history", orderbookHandler.GetOrderbookHistory)

		symbolHandler := handler.NewSymbolHandler(deps.RESTClient)
		v1.GET("/symbols", symbolHandler.GetSymbols)
	}

	if deps.Pipeline != nil && deps.RESTClient != nil {
		// switchSymbol を「pipeline.SwitchSymbol + OnSymbolSwitch」のクロージャに包んで
		// handler に渡す。handler は onSwitch を知らない。
		pipeline := deps.Pipeline
		onSwitch := deps.OnSymbolSwitch
		switchSymbolFn := func(symbolID int64, tradeAmount float64) {
			pipeline.SwitchSymbol(symbolID, tradeAmount, onSwitch)
		}
		tradingConfigHandler := handler.NewTradingConfigHandler(
			deps.Pipeline.SymbolID,
			deps.Pipeline.TradeAmount,
			switchSymbolFn,
			deps.RESTClient,
		)
		v1.GET("/trading-config", tradingConfigHandler.GetTradingConfig)
		v1.PUT("/trading-config", tradingConfigHandler.UpdateTradingConfig)
	}

	if deps.OrderExecutor != nil && deps.ClientOrderRepo != nil {
		orderHandler := handler.NewOrderHandler(deps.OrderExecutor, deps.ClientOrderRepo)
		v1.POST("/orders", orderHandler.CreateOrder)
	}

	if deps.BacktestRunner != nil && deps.BacktestResultRepo != nil {
		opts := []handler.BacktestHandlerOption{}
		if deps.MultiPeriodResultRepo != nil {
			opts = append(opts, handler.WithMultiPeriodRepo(deps.MultiPeriodResultRepo))
		}
		if deps.WalkForwardResultRepo != nil {
			opts = append(opts, handler.WithWalkForwardRepo(deps.WalkForwardResultRepo))
		}
		if deps.MarketDataService != nil {
			opts = append(opts, handler.WithMarketDataService(deps.MarketDataService))
		}
		backtestHandler := handler.NewBacktestHandler(deps.BacktestRunner, deps.BacktestResultRepo, opts...)
		// PR-12: profile discovery endpoints used by the FE backtest picker.
		// The same profilesBaseDir default is used so /profiles and
		// /backtest/run share the same filesystem view.
		profileHandler := handler.NewProfileHandler("")
		v1.GET("/profiles", profileHandler.List)
		v1.GET("/profiles/:name", profileHandler.Get)

		v1.POST("/backtest/run", backtestHandler.Run)
		v1.GET("/backtest/csv-meta", backtestHandler.CSVMeta)
		v1.GET("/backtest/results", backtestHandler.ListResults)
		v1.GET("/backtest/results/:id", backtestHandler.GetResult)
		if deps.MultiPeriodResultRepo != nil {
			v1.POST("/backtest/run-multi", backtestHandler.RunMulti)
			v1.GET("/backtest/multi-results", backtestHandler.ListMultiResults)
			v1.GET("/backtest/multi-results/:id", backtestHandler.GetMultiResult)
		}
		// PR-13 follow-up (#120): walk-forward now persists to the DB.
		// GET endpoints require the repo; POST always computes but only
		// persists when the repo is wired.
		v1.POST("/backtest/walk-forward", backtestHandler.RunWalkForward)
		if deps.WalkForwardResultRepo != nil {
			v1.GET("/backtest/walk-forward", backtestHandler.ListWalkForward)
			v1.GET("/backtest/walk-forward/:id", backtestHandler.GetWalkForward)
		}
	}

	return r
}
