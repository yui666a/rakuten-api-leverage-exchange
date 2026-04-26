package main

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/live"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/booklimit"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/circuitbreaker"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/reconcile"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/sor"
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
	strategy         port.Strategy
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
	stopLossPercent      float64
	takeProfitPercent    float64
	sorConfig            sor.Config
	circuitBreakerConfig circuitbreaker.Config
	staleCheckIntervalMs int64
	reconcileConfig      reconcile.Config
	clientOrderRepo      repository.ClientOrderRepository

	// indicatorPeriods / bbSqueezeLookback are the live counterparts of the
	// per-run RunInput fields the backtest already consumes. They are read
	// once at startup from the configured StrategyProfile (typically
	// production.json) so the live IndicatorHandler computes indicators on
	// the same lookbacks the strategy was tuned for.
	indicatorPeriods  entity.IndicatorConfig
	bbSqueezeLookback int

	// sleepFn is used by syncState for retry backoff (test-injectable).
	sleepFn func(time.Duration)
}

// EventDrivenPipelineConfig holds the configuration for EventDrivenPipeline.
type EventDrivenPipelineConfig struct {
	SymbolID             int64
	StateSyncInterval    time.Duration
	TradeAmount          float64
	MinConfidence        float64
	StopLossPercent      float64
	TakeProfitPercent    float64
	SOR                  sor.Config
	CircuitBreaker       circuitbreaker.Config
	StaleCheckIntervalMs int64
	Reconcile            reconcile.Config

	// IndicatorPeriods drives the live IndicatorHandler's lookback periods
	// for SMA / EMA / RSI / MACD / BB / ATR / VolumeSMA / ADX / Stochastics /
	// StochRSI / Donchian / OBVSlope / CMF / Ichimoku. Zero-valued fields
	// fall back to the legacy LTC PT15M defaults via WithDefaults so a
	// missing profile keeps the pre-PR-D behaviour bit-identical.
	IndicatorPeriods entity.IndicatorConfig

	// BBSqueezeLookback mirrors profile.StanceRules.BBSqueezeLookback so
	// the live handler's RecentSqueeze gate respects the profile (cycle44
	// fix for the backtest path; this PR brings the same plumbing to live).
	// 0 keeps the legacy default of 5.
	BBSqueezeLookback int
}

func NewEventDrivenPipeline(
	cfg EventDrivenPipelineConfig,
	orderClient repository.OrderClient,
	symbolFetcher repository.SymbolFetcher,
	marketDataSvc *usecase.MarketDataService,
	strategy port.Strategy,
	riskMgr *usecase.RiskManager,
	tradeHistoryRepo repository.TradeHistoryRepository,
	riskStateRepo repository.RiskStateRepository,
	clientOrderRepo repository.ClientOrderRepository,
) *EventDrivenPipeline {
	return &EventDrivenPipeline{
		symbolID:          cfg.SymbolID,
		tradeAmount:       cfg.TradeAmount,
		minConfidence:     cfg.MinConfidence,
		stateSyncInterval: cfg.StateSyncInterval,
		stopLossPercent:      cfg.StopLossPercent,
		takeProfitPercent:    cfg.TakeProfitPercent,
		sorConfig:            cfg.SOR,
		circuitBreakerConfig: cfg.CircuitBreaker,
		staleCheckIntervalMs: cfg.StaleCheckIntervalMs,
		reconcileConfig:      cfg.Reconcile,
		orderClient:          orderClient,
		symbolFetcher:     symbolFetcher,
		marketDataSvc:     marketDataSvc,
		strategy:          strategy,
		riskMgr:           riskMgr,
		tradeHistoryRepo:  tradeHistoryRepo,
		riskStateRepo:     riskStateRepo,
		clientOrderRepo:   clientOrderRepo,
		indicatorPeriods:  cfg.IndicatorPeriods,
		bbSqueezeLookback: cfg.BBSqueezeLookback,
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
	if p.marketDataSvc == nil || p.strategy == nil || p.orderClient == nil || p.riskMgr == nil {
		<-ctx.Done()
		return
	}

	// Create LiveSource for tick-to-candle conversion.
	liveSource := live.NewLiveSource(snap.symbolID, "PT15M")

	// Create RealExecutor for live order execution. The SOR is configured
	// via env vars at startup (see loadSORConfig in main.go); when the
	// strategy is "market" the router degrades to the legacy single-MARKET
	// path so this branch stays bit-identical for callers who don't opt in.
	executorOpts := []live.RealExecutorOption{
		live.WithSOR(sor.New(p.sorConfig)),
	}
	if p.marketDataSvc != nil {
		executorOpts = append(executorOpts, live.WithTouchSource(p.marketDataSvc))
		if hub := p.marketDataSvc.RealtimeHub(); hub != nil {
			executorOpts = append(executorOpts, live.WithPositionPublisher(&positionUpdatePublisher{hub: hub}))
		}
	}
	executor := live.NewRealExecutor(p.orderClient, snap.symbolID, 0, executorOpts...)

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
	// PR-D: profile-driven indicator periods + BB squeeze lookback. The
	// backtest path has been on this since PR-B/C; live now uses the same
	// knob set so the strategy sees identical indicator values whether it
	// is being backtested or run for real.
	indicatorHandler.SetIndicatorPeriods(p.indicatorPeriods)
	if p.bbSqueezeLookback > 0 {
		indicatorHandler.SetBBSqueezeLookback(p.bbSqueezeLookback)
	}
	if p.marketDataSvc != nil {
		// PR-J: feed Microprice / OFI from the live in-memory book cache.
		indicatorHandler.SetBookSource(p.marketDataSvc, 10_000, 60_000, 5)
	}
	bus.Register(entity.EventTypeCandle, 10, indicatorHandler)

	// StrategyHandler: signal generation from indicators (priority 20).
	strategyHandler := &backtest.StrategyHandler{Strategy: p.strategy}
	bus.Register(entity.EventTypeIndicator, 20, strategyHandler)

	// RiskHandler: risk gating for signals (priority 30).
	riskHandler := &backtest.RiskHandler{
		RiskManager: p.riskMgr,
		TradeAmount: snap.tradeAmount,
	}
	// Pre-trade orderbook depth gate. Live mode keeps the gate forgiving on
	// missing/stale snapshots (AllowOnMissingBook=true) so a transient WS
	// gap does not block trading; the simulator path uses the strict mode.
	if cfg := p.riskMgr.GetStatus().Config; cfg.MaxSlippageBps > 0 || cfg.MaxBookSidePct > 0 {
		riskHandler.BookGate = booklimit.New(p.marketDataSvc, booklimit.Config{
			MaxSlippageBps:     cfg.MaxSlippageBps,
			MaxBookSidePct:     cfg.MaxBookSidePct,
			TopN:               booklimit.DefaultTopN,
			StaleAfterMillis:   60_000,
			AllowOnMissingBook: true,
		})
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

	// Adaptive position sync. The pipeline used to poll on a fixed 30 s
	// cadence; that's fine for steady-state monitoring but leaves a long
	// blind spot right after an order fill where the venue knows the new
	// state and we don't. We now check executor.LastOrderAt() on each tick
	// and use a 2 s burst window for the first 60 s after activity, then
	// fall back to the 30 s baseline. The base ticker is the burst rate so
	// the pipeline can react inside a single burst.
	const (
		burstIntervalMs    = 2_000
		idleIntervalMs     = 30_000
		burstWindowAfterMs = 60_000
	)
	positionSyncTicker := time.NewTicker(time.Duration(burstIntervalMs) * time.Millisecond)
	defer positionSyncTicker.Stop()
	var nextSyncMs int64

	// Circuit breaker: opt-in. When any threshold > 0 we wire a Watcher
	// that observes every WS frame via MarketDataService and a heartbeat
	// goroutine that polls CheckStale on the configured cadence.
	cbWatcher := p.startCircuitBreakerLocked(ctx)
	if cbWatcher != nil {
		defer cbWatcher.Reset() // re-arm on next pipeline lifecycle
	}

	// Reconciler: opt-in. Runs on its own goroutine bound to ctx.
	p.startReconciler(ctx, snap)

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
			now := time.Now().UnixMilli()
			if nextSyncMs > 0 && now < nextSyncMs {
				continue
			}
			if err := executor.SyncPositions(ctx); err != nil {
				slog.Warn("event-pipeline: periodic position sync failed", "error", err)
			}
			interval := int64(idleIntervalMs)
			if last := executor.LastOrderAt(); last > 0 && now-last < int64(burstWindowAfterMs) {
				interval = int64(burstIntervalMs)
			}
			nextSyncMs = now + interval
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

// circuitBreakerHubPublisher adapts RealtimeHub.PublishData to
// circuitbreaker.Publisher. Decoupled here so the usecase package does not
// learn about the realtime hub directly.
type circuitBreakerHubPublisher struct {
	hub *usecase.RealtimeHub
}

func (p *circuitBreakerHubPublisher) PublishCircuitBreaker(reason, detail string, ts int64) {
	if p == nil || p.hub == nil {
		return
	}
	payload := usecase.RiskEventPayload{
		Kind:      usecase.RiskEventCircuitBreaker,
		Severity:  usecase.RiskSeverityCritical,
		Message:   "circuit breaker tripped: " + reason,
		Timestamp: ts,
	}
	if detail != "" {
		payload.Message += " (" + detail + ")"
	}
	if err := p.hub.PublishData("risk_event", 0, payload); err != nil {
		slog.Warn("event-pipeline: circuit-breaker publish failed", "error", err)
	}
}

// startCircuitBreakerLocked spins up the watcher + the stale-check heartbeat
// when at least one threshold is configured. Returns the watcher (nil when
// the breaker is disabled) so the caller can Reset() on shutdown.
//
// Called from runEventLoop, where p.mu is *not* held — we don't need the
// lock here because the fields we read are set once at construction and the
// watcher itself is fully thread-safe.
func (p *EventDrivenPipeline) startCircuitBreakerLocked(ctx context.Context) *circuitbreaker.Watcher {
	cfg := p.circuitBreakerConfig
	if cfg.AbnormalSpreadPct == 0 && cfg.PriceJumpPct == 0 && cfg.BookFeedStaleAfterMs == 0 && cfg.EmptyBookHoldMs == 0 {
		return nil
	}

	hubPub := &circuitBreakerHubPublisher{hub: nil}
	// Pull the hub via a side channel so we don't have to expose another
	// field on the pipeline. RealtimeHub on MarketDataService is the same
	// instance the rest of the system uses.
	if p.marketDataSvc != nil {
		hubPub.hub = p.realtimeHubFromMarketData()
	}

	w := circuitbreaker.New(cfg, p.riskMgr, hubPub)
	if p.marketDataSvc != nil {
		p.marketDataSvc.AddObserver(circuitBreakerObserver{w: w})
	}

	// Heartbeat for stale-feed detection.
	if cfg.BookFeedStaleAfterMs > 0 {
		interval := time.Duration(p.staleCheckIntervalMs) * time.Millisecond
		if interval <= 0 {
			interval = 5 * time.Second
		}
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					w.CheckStale()
				}
			}
		}()
	}
	slog.Info("event-pipeline: circuit breaker armed",
		"abnSpreadPct", cfg.AbnormalSpreadPct,
		"jumpPct", cfg.PriceJumpPct,
		"staleAfterMs", cfg.BookFeedStaleAfterMs,
		"emptyBookHoldMs", cfg.EmptyBookHoldMs,
	)
	return w
}

// circuitBreakerObserver bridges MarketDataObserver → circuitbreaker.Watcher.
type circuitBreakerObserver struct{ w *circuitbreaker.Watcher }

func (o circuitBreakerObserver) OnTicker(t entity.Ticker)          { o.w.OnTicker(t) }
func (o circuitBreakerObserver) OnOrderbook(ob entity.Orderbook)   { o.w.OnOrderbook(ob) }

// realtimeHubFromMarketData looks up the hub the MarketDataService publishes
// on. We avoid plumbing an additional field through every constructor by
// reaching through this getter; if the API ever drifts the call simply
// returns nil and the breaker just won't push UI events.
func (p *EventDrivenPipeline) realtimeHubFromMarketData() *usecase.RealtimeHub {
	if p.marketDataSvc == nil {
		return nil
	}
	return p.marketDataSvc.RealtimeHub()
}

// reconcilePublisher adapts realtime hub PublishData to reconcile.Publisher.
type reconcilePublisher struct{ hub *usecase.RealtimeHub }

func (p *reconcilePublisher) PublishDrift(kind, severity, message string, ts int64) {
	if p == nil || p.hub == nil {
		return
	}
	sev := usecase.RiskSeverityWarning
	switch severity {
	case "critical":
		sev = usecase.RiskSeverityCritical
	case "info":
		sev = usecase.RiskSeverityInfo
	}
	payload := usecase.RiskEventPayload{
		Kind:      usecase.RiskEventKind("reconciliation_drift"),
		Severity:  sev,
		Message:   "reconcile " + kind + ": " + message,
		Timestamp: ts,
	}
	if err := p.hub.PublishData("risk_event", 0, payload); err != nil {
		slog.Warn("event-pipeline: reconcile publish failed", "error", err)
	}
}

// startReconciler runs the reconciler on its own goroutine. Lifecycle is
// tied to ctx; the loop exits cleanly when the pipeline is stopped.
//
// Symbol switches update the reconciler's symbol via SetSymbolID through
// the live snapshot — we do not need a separate notification channel
// because the reconciler is read-only.
func (p *EventDrivenPipeline) startReconciler(ctx context.Context, snap eventSnapshot) *reconcile.Reconciler {
	if !p.reconcileConfig.Enable {
		return nil
	}
	pub := &reconcilePublisher{hub: nil}
	if p.marketDataSvc != nil {
		pub.hub = p.marketDataSvc.RealtimeHub()
	}
	r := reconcile.New(p.reconcileConfig, p.orderClient, p.riskMgr, p.riskMgr, p.clientOrderRepo, pub, snap.symbolID)
	interval := time.Duration(p.reconcileConfig.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// First pass after a short warmup so syncState has populated the
		// in-memory state at least once.
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
		r.Run(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Always reconcile against the live symbol — SwitchSymbol
				// updates p.symbolID but the reconciler is built once.
				r.SetSymbolID(p.SymbolID())
				r.Run(ctx)
			}
		}
	}()
	slog.Info("event-pipeline: reconciler armed",
		"intervalSec", p.reconcileConfig.IntervalSec,
		"posHaltPct", p.reconcileConfig.PositionHaltPct,
		"balHaltPct", p.reconcileConfig.BalanceHaltPct,
	)
	return r
}

// positionUpdatePublisher pushes RealExecutor's diff-detected position events
// to the realtime hub so the dashboard can refresh immediately on fill,
// instead of waiting up to one position-sync interval.
type positionUpdatePublisher struct{ hub *usecase.RealtimeHub }

func (p *positionUpdatePublisher) PublishPositionUpdate(symbolID int64, positions []eventengine.Position) {
	if p == nil || p.hub == nil {
		return
	}
	if err := p.hub.PublishData("position_update", symbolID, positions); err != nil {
		slog.Warn("event-pipeline: position_update publish failed", "error", err)
	}
}
