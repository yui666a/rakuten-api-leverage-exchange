package main

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/live"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

// EventDrivenPipeline replaces the polling-based TradingPipeline with an
// EventEngine-driven architecture. LiveSource converts real-time tickers into
// TickEvent/CandleEvent, and the EventBus dispatches them through the handler
// chain (indicator -> strategy -> risk -> execution, plus tick-based SL/TP).
//
// The struct satisfies the api.PipelineController interface so the REST API
// (POST /start, POST /stop, PUT /trading-config) continues to work.
type EventDrivenPipeline struct {
	switchMu sync.Mutex   // serialises SwitchSymbol / Start / Stop
	mu       sync.RWMutex // protects mutable fields

	cancel context.CancelFunc

	// Existing dependencies (injected at construction)
	riskMgr          *usecase.RiskManager
	strategyEngine   *usecase.StrategyEngine
	marketDataSvc    *usecase.MarketDataService
	orderClient      repository.OrderClient
	symbolFetcher    repository.SymbolFetcher
	tradeHistoryRepo repository.TradeHistoryRepository
	riskStateRepo    repository.RiskStateRepository

	// Config
	symbolID          int64
	tradeAmount       float64
	baseStepAmount    float64
	minOrderAmount    float64
	minConfidence     float64
	stateSyncInterval time.Duration
	stopLossPercent   float64
	takeProfitPercent float64

	// sleepFn is used by syncState for retry backoff (test-injectable).
	sleepFn func(time.Duration)
}

// EventDrivenPipelineConfig holds the configuration for EventDrivenPipeline.
type EventDrivenPipelineConfig struct {
	SymbolID          int64
	StateSyncInterval time.Duration
	TradeAmount       float64
	MinConfidence     float64
	StopLossPercent   float64
	TakeProfitPercent float64
}

func NewEventDrivenPipeline(
	cfg EventDrivenPipelineConfig,
	orderClient repository.OrderClient,
	symbolFetcher repository.SymbolFetcher,
	marketDataSvc *usecase.MarketDataService,
	strategyEngine *usecase.StrategyEngine,
	riskMgr *usecase.RiskManager,
	tradeHistoryRepo repository.TradeHistoryRepository,
	riskStateRepo repository.RiskStateRepository,
) *EventDrivenPipeline {
	return &EventDrivenPipeline{
		symbolID:          cfg.SymbolID,
		tradeAmount:       cfg.TradeAmount,
		minConfidence:     cfg.MinConfidence,
		stateSyncInterval: cfg.StateSyncInterval,
		stopLossPercent:   cfg.StopLossPercent,
		takeProfitPercent: cfg.TakeProfitPercent,
		orderClient:       orderClient,
		symbolFetcher:     symbolFetcher,
		marketDataSvc:     marketDataSvc,
		strategyEngine:    strategyEngine,
		riskMgr:           riskMgr,
		tradeHistoryRepo:  tradeHistoryRepo,
		riskStateRepo:     riskStateRepo,
	}
}

// Start begins the event-driven trading pipeline.
// Satisfies api.PipelineController.
func (p *EventDrivenPipeline) Start() {
	p.switchMu.Lock()
	defer p.switchMu.Unlock()
	p.startLocked()
}

func (p *EventDrivenPipeline) startLocked() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		return // already running
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	// Snapshot config under lock for goroutine use.
	snap := p.snapshotLocked()

	go p.runEventLoop(ctx, snap)

	slog.Info("event-driven pipeline started", "symbolID", snap.symbolID)
}

// Stop halts the event-driven trading pipeline.
// Satisfies api.PipelineController.
func (p *EventDrivenPipeline) Stop() {
	p.switchMu.Lock()
	defer p.switchMu.Unlock()
	p.stopLocked()
}

func (p *EventDrivenPipeline) stopLocked() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return
	}

	p.cancel()
	p.cancel = nil

	slog.Info("event-driven pipeline stopped")
}

// Running returns whether the pipeline is currently active.
// Satisfies api.PipelineController.
func (p *EventDrivenPipeline) Running() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cancel != nil
}

// SymbolID returns the current trading symbol.
// Satisfies api.PipelineController.
func (p *EventDrivenPipeline) SymbolID() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.symbolID
}

// TradeAmount returns the current trade amount per order.
// Satisfies api.PipelineController.
func (p *EventDrivenPipeline) TradeAmount() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tradeAmount
}

// SwitchSymbol switches the trading target symbol.
// Satisfies api.PipelineController.
func (p *EventDrivenPipeline) SwitchSymbol(symbolID int64, tradeAmount float64, onSwitch func(oldID, newID int64)) {
	p.switchMu.Lock()
	defer p.switchMu.Unlock()

	p.mu.RLock()
	oldID := p.symbolID
	wasRunning := p.cancel != nil
	p.mu.RUnlock()

	if wasRunning {
		p.stopLocked()
	}

	p.mu.Lock()
	p.symbolID = symbolID
	if tradeAmount > 0 {
		p.tradeAmount = tradeAmount
	}
	p.loadSymbolMeta(context.Background(), symbolID)
	p.mu.Unlock()

	if onSwitch != nil {
		onSwitch(oldID, symbolID)
	}

	if wasRunning {
		p.startLocked()
	}
}

// loadSymbolMeta fetches baseStepAmount / minOrderAmount from the API.
// Must be called with mu held.
func (p *EventDrivenPipeline) loadSymbolMeta(ctx context.Context, symbolID int64) {
	if p.symbolFetcher == nil {
		return
	}
	symbols, err := p.symbolFetcher.GetSymbols(ctx)
	if err != nil {
		slog.Warn("event-pipeline: failed to fetch symbols for step amount", "error", err)
		return
	}
	for _, s := range symbols {
		if s.ID == symbolID {
			p.baseStepAmount = s.BaseStepAmount.Float64()
			p.minOrderAmount = s.MinOrderAmount.Float64()
			slog.Info("event-pipeline: loaded symbol meta",
				"symbolID", symbolID,
				"baseStepAmount", p.baseStepAmount,
				"minOrderAmount", p.minOrderAmount,
			)
			return
		}
	}
	slog.Warn("event-pipeline: symbol not found in API response", "symbolID", symbolID)
}

// eventSnapshot is a copy of config fields taken under lock.
type eventSnapshot struct {
	symbolID          int64
	tradeAmount       float64
	baseStepAmount    float64
	minOrderAmount    float64
	minConfidence     float64
	stopLossPercent   float64
	takeProfitPercent float64
}

func (p *EventDrivenPipeline) snapshotLocked() eventSnapshot {
	return eventSnapshot{
		symbolID:          p.symbolID,
		tradeAmount:       p.tradeAmount,
		baseStepAmount:    p.baseStepAmount,
		minOrderAmount:    p.minOrderAmount,
		minConfidence:     p.minConfidence,
		stopLossPercent:   p.stopLossPercent,
		takeProfitPercent: p.takeProfitPercent,
	}
}

func (p *EventDrivenPipeline) snapshot() eventSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snapshotLocked()
}

// runEventLoop is the core goroutine. It subscribes to real-time tickers,
// feeds them through LiveSource to produce events, and dispatches them
// through the EventEngine handler chain.
func (p *EventDrivenPipeline) runEventLoop(ctx context.Context, snap eventSnapshot) {
	if p.marketDataSvc == nil || p.strategyEngine == nil || p.orderClient == nil || p.riskMgr == nil {
		<-ctx.Done()
		return
	}

	// Create LiveSource for tick-to-candle conversion.
	liveSource := live.NewLiveSource(snap.symbolID, "PT15M")

	// Create RealExecutor for live order execution.
	executor := live.NewRealExecutor(p.orderClient, snap.symbolID, 0)

	// Sync positions from API into the executor at startup.
	if err := executor.SyncPositions(ctx); err != nil {
		slog.Warn("event-pipeline: initial position sync failed", "error", err)
	}

	// Wire up EventBus with handlers (same architecture as backtest runner).
	bus := eventengine.NewEventBus()

	// TickRiskHandler: SL/TP on every tick (priority 15).
	tickRiskHandler := backtest.NewTickRiskHandler(
		"PT15M",
		executor,
		snap.stopLossPercent,
		snap.takeProfitPercent,
	)
	bus.Register(entity.EventTypeTick, 15, tickRiskHandler)

	// IndicatorHandler: calculates technical indicators on candle close (priority 10).
	indicatorHandler := backtest.NewIndicatorHandler("PT15M", "PT1H", 500)
	bus.Register(entity.EventTypeCandle, 10, indicatorHandler)

	// StrategyHandler: signal generation from indicators (priority 20).
	strategyHandler := &backtest.StrategyHandler{Engine: p.strategyEngine}
	bus.Register(entity.EventTypeIndicator, 20, strategyHandler)

	// RiskHandler: risk gating for signals (priority 30).
	riskHandler := &backtest.RiskHandler{
		RiskManager: p.riskMgr,
		TradeAmount: snap.tradeAmount,
	}
	bus.Register(entity.EventTypeSignal, 30, riskHandler)

	// ExecutionHandler: opens orders from approved signals (priority 40).
	executionHandler := &backtest.ExecutionHandler{
		Executor:    executor,
		TradeAmount: snap.tradeAmount,
	}
	bus.Register(entity.EventTypeApproved, 40, executionHandler)

	engine := eventengine.NewEventEngine(bus)

	// Subscribe to real-time ticker stream.
	tickerCh := p.marketDataSvc.SubscribeTicker()
	defer p.marketDataSvc.UnsubscribeTicker(tickerCh)

	// Periodic position sync for the executor (reconcile in-memory state with API).
	positionSyncInterval := 30 * time.Second
	positionSyncTicker := time.NewTicker(positionSyncInterval)
	defer positionSyncTicker.Stop()

	slog.Info("event-pipeline: event loop running", "symbolID", snap.symbolID)

	for {
		select {
		case <-ctx.Done():
			return

		case ticker, ok := <-tickerCh:
			if !ok {
				return
			}
			if ticker.SymbolID != snap.symbolID {
				continue
			}

			// Feed ticker into LiveSource to produce events (TickEvent + possibly CandleEvent).
			events := liveSource.HandleTick(ticker)
			if len(events) == 0 {
				continue
			}

			// Dispatch events through the EventEngine.
			if err := engine.Run(ctx, events); err != nil {
				slog.Error("event-pipeline: dispatch error", "error", err)
			}

		case <-positionSyncTicker.C:
			if err := executor.SyncPositions(ctx); err != nil {
				slog.Warn("event-pipeline: periodic position sync failed", "error", err)
			}
		}
	}
}

// runStateSyncLoop periodically syncs positions and balance from the Rakuten API
// into the RiskManager. This is identical to TradingPipeline.runStateSyncLoop.
func (p *EventDrivenPipeline) runStateSyncLoop(ctx context.Context) {
	if p.orderClient == nil || p.riskMgr == nil {
		<-ctx.Done()
		return
	}

	syncInterval := p.stateSyncInterval
	if syncInterval <= 0 {
		syncInterval = 15 * time.Second
	}

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.syncState(ctx)
			p.persistRiskState(ctx)
		}
	}
}

// syncState fetches positions and balance from the Rakuten API.
func (p *EventDrivenPipeline) syncState(ctx context.Context) {
	sleep := p.sleepFn
	if sleep == nil {
		sleep = time.Sleep
	}

	snap := p.snapshot()

	var positions []entity.Position
	posErr := retryOn20010(ctx, sleep, func() error {
		var err error
		positions, err = p.orderClient.GetPositions(ctx, snap.symbolID)
		return err
	})
	if posErr != nil {
		slog.Warn("event-pipeline: failed to sync positions", "error", posErr)
	} else {
		p.riskMgr.UpdatePositions(positions)
	}

	var assets []entity.Asset
	assetErr := retryOn20010(ctx, sleep, func() error {
		var err error
		assets, err = p.orderClient.GetAssets(ctx)
		return err
	})
	if assetErr != nil {
		slog.Warn("event-pipeline: failed to sync assets", "error", assetErr)
	} else {
		for _, a := range assets {
			if a.Currency == "JPY" {
				if balance, err := strconv.ParseFloat(a.OnhandAmount, 64); err == nil {
					p.riskMgr.UpdateBalance(balance)
				}
			}
		}
	}
}

// syncStateInitial performs a one-shot sync at startup.
func (p *EventDrivenPipeline) syncStateInitial(ctx context.Context) {
	p.syncState(ctx)
	slog.Info("event-pipeline: initial state sync completed")
}

// persistRiskState saves the current risk state to the database.
func (p *EventDrivenPipeline) persistRiskState(ctx context.Context) {
	if p.riskStateRepo == nil {
		return
	}
	status := p.riskMgr.GetStatus()
	if err := p.riskStateRepo.Save(ctx, repository.RiskState{
		DailyLoss: status.DailyLoss,
		Balance:   status.Balance,
	}); err != nil {
		slog.Error("event-pipeline: failed to persist risk state", "error", err)
	}
}
