package main

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	domainexitplan "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/exitplan"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/live"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/booklimit"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/circuitbreaker"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/decision"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/decisionlog"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
	usecaseexitplan "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/exitplan"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/positionsize"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/reconcile"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/sor"
)

// PipelineStanceResolver is the narrow port the pipeline needs to expose
// the strategy's stance to the decision recorder. It mirrors the subset of
// usecase.StanceResolver used here so the pipeline does not pull the whole
// resolver type into its API.
type PipelineStanceResolver interface {
	Resolve(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) usecase.StanceResult
}

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
	stanceResolver   PipelineStanceResolver
	marketDataSvc    *usecase.MarketDataService
	orderClient      repository.OrderClient
	symbolFetcher    repository.SymbolFetcher
	tradeHistoryRepo repository.TradeHistoryRepository
	riskStateRepo    repository.RiskStateRepository

	// Config
	symbolID          int64
	currencyPair      string
	tradeAmount       float64
	baseStepAmount    float64
	minOrderAmount    float64
	minConfidence     float64
	stateSyncInterval time.Duration
	// riskPolicy is the single source of truth for SL / TP / trailing
	// distance behaviour. Built from the strategy profile (or the env
	// fallback) by risk.FromProfile and validated at startup, so a
	// missing or misconfigured profile fails before runEventLoop subscribes
	// to ticks. The legacy float64 fields (stopLossPercent etc.) and the
	// SetATRMultipliers setter that used to glue them together are gone —
	// the policy is locked at construction so "live forgot to call
	// SetX" is no longer possible.
	riskPolicy           risk.RiskPolicy
	sorConfig            sor.Config
	circuitBreakerConfig circuitbreaker.Config
	staleCheckIntervalMs int64
	reconcileConfig      reconcile.Config
	clientOrderRepo      repository.ClientOrderRepository

	// decisionLogRepo, when non-nil, attaches a DecisionRecorder to the
	// EventBus so every pipeline cycle persists a row. nil disables the
	// recorder; the rest of the pipeline is unaffected.
	decisionLogRepo repository.DecisionLogRepository

	// exitPlanRepo, when non-nil, attaches the Phase 1 ExitPlan shadow
	// handler so every fill is mirrored into the exit_plans table.
	// シャドウなので発注パスへは干渉しない。Phase 2 で SL/TP/Trailing
	// 発火本体が ExitHandler に移管されたら shadow handler は退役する。
	exitPlanRepo domainexitplan.Repository

	// candlestickFetcher is used at event-loop start to seed the LiveSource
	// CandleBuilder with the in-progress PT15M bar reconstructed from PT1M
	// candles. Optional — when nil the first emitted bar after a restart
	// only contains post-restart ticks (legacy behaviour).
	candlestickFetcher repository.CandlestickFetcher

	// indicatorPeriods / bbSqueezeLookback are the live counterparts of the
	// per-run RunInput fields the backtest already consumes. They are read
	// once at startup from the configured StrategyProfile (typically
	// production.json) so the live IndicatorHandler computes indicators on
	// the same lookbacks the strategy was tuned for.
	indicatorPeriods  entity.IndicatorConfig
	bbSqueezeLookback int

	// positionSizing mirrors the profile's PositionSizing block so the live
	// RiskHandler runs through the same sizer the backtest does. nil (or
	// Mode == "" / "fixed") falls back to the legacy fixed-amount path
	// where TradeAmount is used as proposal.Amount verbatim.
	positionSizing *entity.PositionSizingConfig
	// initialBalance seeds the PeakTracker for drawdown-based lot scaling.
	// Sourced from cfg.Risk.InitialCapital at startup.
	initialBalance float64
	// exitOnSignal mirrors profile.Risk.ExitOnSignal. When true, RiskHandler
	// closes positions on IntentExitCandidate via the live executor instead
	// of leaving the exit to TP/SL/Trailing. Defaults false so profiles
	// that have not opted in keep the Phase 1 behaviour.
	exitOnSignal bool

	// primaryInterval / higherTFInterval drive the LiveSource, the
	// IndicatorHandler, and every per-bar handler that needs to know which
	// timeframe the pipeline is operating on. Sourced from the strategy
	// profile so a profile tuned for PT5M can run the live pipeline on PT5M
	// without touching code. Defaults to PT15M / PT1H (the legacy LTC
	// hardcode) when the config leaves them empty.
	primaryInterval  string
	higherTFInterval string

	// latestIndicators caches the most recent IndicatorEvent payload so
	// the decision recorder's StanceProvider can re-resolve stance without
	// reaching into the strategy's internal state. Updated by a side
	// handler registered on EventTypeIndicator. Guarded by indicatorMu so
	// the recorder (also on the bus) and the tick path can read it
	// concurrently with the indicator-handler write. Empty
	// (hasLatestIndicators=false) until the first IndicatorEvent fires.
	indicatorMu          sync.RWMutex
	latestIndicators     entity.IndicatorSet
	latestLastPrice      float64
	hasLatestIndicators  bool

	// sleepFn is used by syncState for retry backoff (test-injectable).
	sleepFn func(time.Duration)
}

// EventDrivenPipelineConfig holds the configuration for EventDrivenPipeline.
type EventDrivenPipelineConfig struct {
	SymbolID             int64
	StateSyncInterval    time.Duration
	TradeAmount          float64
	MinConfidence        float64
	// RiskPolicy is the strategy-level SL / TP / trailing policy. Build
	// it via risk.FromProfile(profile, envSL, envTP) and call
	// policy.Validate() at startup so a misconfigured profile fails fast
	// instead of producing silent HOLD-only behaviour. The constructor
	// asserts the policy is non-zero (StopLoss.Percent > 0) so callers
	// cannot wire an empty zero-value by accident.
	RiskPolicy           risk.RiskPolicy
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

	// DecisionLogRepo, when non-nil, attaches a DecisionRecorder to the
	// pipeline's EventBus so every cycle (BAR_CLOSE plus tick-driven
	// SL/TP/Trailing closes) persists into decision_log. nil disables it
	// without otherwise affecting the pipeline.
	DecisionLogRepo repository.DecisionLogRepository

	// ExitPlanRepo, when non-nil, attaches the Phase 1 ExitPlan shadow
	// handler so the pipeline mirrors fills into the exit_plans table.
	// シャドウ運用専用。SL/TP/Trailing 発火経路は変更しない。
	ExitPlanRepo domainexitplan.Repository

	// CandlestickFetcher is used to seed the LiveSource CandleBuilder with
	// the in-progress PT15M bar (reconstructed from PT1M) at startup so the
	// first emitted bar after a restart has correct OHLC. nil keeps the
	// legacy behaviour where the first bar only reflects post-restart ticks.
	CandlestickFetcher repository.CandlestickFetcher

	// StanceResolver, when non-nil, is invoked by the decision recorder's
	// StanceProvider so every persisted row records the live stance the
	// strategy is currently classifying against (TREND_FOLLOW / CONTRARIAN /
	// BREAKOUT / HOLD), instead of the placeholder UNKNOWN.
	//
	// nil disables stance reporting; the recorder falls back to "UNKNOWN".
	// The pipeline shares this resolver with the StrategyEngine wired in
	// composition.go, so override-driven stance changes apply consistently.
	StanceResolver PipelineStanceResolver

	// PrimaryInterval is the timeframe the strategy operates on. Mirrors the
	// strategy profile's primary period (e.g. "PT15M" for LTC, "PT5M" for
	// ETH). Empty string falls back to "PT15M" so existing callers stay
	// bit-identical without explicitly setting it.
	PrimaryInterval string

	// HigherTFInterval is the optional confirmation timeframe (used by the
	// HTF gate inside StrategyEngine). Empty falls back to "PT1H".
	HigherTFInterval string

	// PositionSizing mirrors profile.Risk.PositionSizing so the live
	// RiskHandler computes per-trade lot size via the same sizer the
	// backtest uses. nil keeps the legacy fixed-amount behaviour.
	PositionSizing *entity.PositionSizingConfig

	// InitialBalance seeds the PeakTracker for drawdown-aware lot scaling.
	// Typically cfg.Risk.InitialCapital. 0 disables PeakTracker.
	InitialBalance float64

	// ExitOnSignal mirrors profile.Risk.ExitOnSignal. When true the live
	// RiskHandler closes positions on Decision-layer IntentExitCandidate
	// instead of leaving exits to the TP/SL/Trailing tick path. Defaults
	// false to preserve Phase 1 behaviour for profiles that have not
	// opted in.
	ExitOnSignal bool
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
		riskPolicy:           cfg.RiskPolicy,
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
		indicatorPeriods:   cfg.IndicatorPeriods,
		bbSqueezeLookback:  cfg.BBSqueezeLookback,
		decisionLogRepo:    cfg.DecisionLogRepo,
		exitPlanRepo:       cfg.ExitPlanRepo,
		candlestickFetcher: cfg.CandlestickFetcher,
		stanceResolver:     cfg.StanceResolver,
		primaryInterval:    primaryIntervalOrDefault(cfg.PrimaryInterval),
		higherTFInterval:   higherTFIntervalOrDefault(cfg.HigherTFInterval),
		positionSizing:     cfg.PositionSizing,
		initialBalance:     cfg.InitialBalance,
		exitOnSignal:       cfg.ExitOnSignal,
	}
}

// policyView translates a risk.RiskPolicy into the flat, package-private
// view backtest.NewTickRiskHandlerWithPolicy consumes. It lives here
// (in cmd/) rather than in usecase/backtest so the latter can stay free
// of the domain/risk import — keeping the dependency direction
// domain ← usecase ← cmd as Clean Architecture demands.
func policyView(p risk.RiskPolicy) backtest.PolicyView {
	mode := backtest.TrailingModeDisabled
	switch p.Trailing.Mode {
	case risk.TrailingModeATR:
		mode = backtest.TrailingModeATR
	case risk.TrailingModePercent:
		mode = backtest.TrailingModePercent
	}
	return backtest.PolicyView{
		StopLossPercent:       p.StopLoss.Percent,
		StopLossATRMultiplier: p.StopLoss.ATRMultiplier,
		TakeProfitPercent:     p.TakeProfit.Percent,
		TrailingMode:          mode,
		TrailingATRMultiplier: p.Trailing.ATRMultiplier,
	}
}

// primaryIntervalOrDefault returns the configured primary interval, falling
// back to the legacy LTC PT15M default when the field is left empty so
// callers that have not yet plumbed a profile-driven value through stay
// bit-identical.
func primaryIntervalOrDefault(s string) string {
	if s == "" {
		return "PT15M"
	}
	return s
}

// higherTFIntervalOrDefault mirrors primaryIntervalOrDefault for the
// optional confirmation timeframe.
func higherTFIntervalOrDefault(s string) string {
	if s == "" {
		return "PT1H"
	}
	return s
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
	oldAmount := p.tradeAmount
	wasRunning := p.cancel != nil
	p.mu.RUnlock()

	// No-op guard: a switch to the same symbol with the same trade amount
	// would otherwise tear down and rebuild the event loop, dropping the
	// in-flight CandleBuilder state and the recorder's pending bar draft.
	// We saw this in production when the frontend re-PUTs trading-config
	// on every focus change — a single redundant call destroyed ~30 min of
	// decision-log accumulation. Cheap to guard here; the SwitchSymbol
	// contract still allows reordering callers to detect the no-op via the
	// onSwitch callback (which we still skip below).
	if symbolID == oldID && (tradeAmount <= 0 || tradeAmount == oldAmount) {
		return
	}

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

// seedCandleBuilderFromMinutes pulls PT1M candles covering the current
// primary-interval window and folds them into the LiveSource's
// CandleBuilder so the first emit after a restart contains the *whole*
// bar's OHLC, not just post-restart ticks. Best-effort: any error path
// leaves the builder in its legacy state (next live tick will initialise
// it from the tick).
//
// PT1M is used as the universal probe regardless of the primary interval —
// we just truncate `now` to the primary interval boundary to find the
// in-progress bar. For sub-minute primaries this would degrade (and the
// venue does not offer those), so the fetch is skipped when the configured
// interval is not a clean multiple of one minute.
func (p *EventDrivenPipeline) seedCandleBuilderFromMinutes(ctx context.Context, symbolID int64, liveSource *live.LiveSource) {
	if p.candlestickFetcher == nil {
		return
	}
	primaryDur := liveIntervalToDuration(p.primaryInterval)
	if primaryDur <= 0 || primaryDur < time.Minute {
		return
	}
	now := time.Now().UTC()
	periodStart := now.Truncate(primaryDur)
	// Pull from the period boundary up to one minute past now so we never
	// miss the first PT1M bar even if the venue is a few seconds behind.
	from := periodStart.UnixMilli()
	to := now.Add(time.Minute).UnixMilli()
	resp, err := p.candlestickFetcher.GetCandlestick(ctx, symbolID, "PT1M", &from, &to)
	if err != nil {
		slog.Warn("event-pipeline: PT1M bootstrap fetch failed; first bar will only see post-restart ticks",
			"symbolID", symbolID, "error", err)
		return
	}
	if resp == nil || len(resp.Candlesticks) == 0 {
		return
	}
	folded := liveSource.SeedFromMinuteCandles(now, resp.Candlesticks)
	if folded > 0 {
		slog.Info("event-pipeline: seeded CandleBuilder from PT1M",
			"symbolID", symbolID,
			"primaryInterval", p.primaryInterval,
			"foldedMinutes", folded,
			"periodStart", periodStart.Format(time.RFC3339),
		)
	}
}

// seedIndicatorHistory pulls historical primary-interval candles and folds
// them into the IndicatorHandler's buffer so the very first CandleEvent
// emitted from the WebSocket already produces a fully-populated
// IndicatorSet. Without this, SMA(50) / Ichimoku(52) / MACD signal stay nil
// for many bars after every restart and the strategy is forced into HOLD —
// which is exactly the failure mode the decision_log table surfaced
// (every row UNKNOWN/HOLD/SKIPPED/NOOP).
//
// Best-effort: any error path leaves the buffer empty and falls back to
// "fill indicators as live bars close" (the legacy behaviour).
//
// barsToSeed should comfortably exceed the slowest indicator period in use.
// 64 covers Ichimoku Senkou B (52) + MACD signal warm-up (~9) with margin.
func (p *EventDrivenPipeline) seedIndicatorHistory(
	ctx context.Context,
	symbolID int64,
	primaryInterval string,
	indicatorHandler *backtest.IndicatorHandler,
	barsToSeed int,
) {
	if p.candlestickFetcher == nil || indicatorHandler == nil || barsToSeed <= 0 {
		return
	}
	intervalDur := liveIntervalToDuration(primaryInterval)
	if intervalDur <= 0 {
		return
	}
	now := time.Now().UTC()
	periodStart := now.Truncate(intervalDur)
	from := periodStart.Add(-time.Duration(barsToSeed) * intervalDur).UnixMilli()
	// Stop one millisecond before the *current* bar start so we never feed
	// the in-progress bar twice (the live source will close it from ticks).
	to := periodStart.UnixMilli() - 1
	resp, err := p.candlestickFetcher.GetCandlestick(ctx, symbolID, primaryInterval, &from, &to)
	if err != nil {
		slog.Warn("event-pipeline: indicator history fetch failed; first bar will see all-nil indicators",
			"symbolID", symbolID, "interval", primaryInterval, "error", err)
		return
	}
	if resp == nil || len(resp.Candlesticks) == 0 {
		return
	}
	indicatorHandler.SeedPrimary(symbolID, resp.Candlesticks)
	slog.Info("event-pipeline: seeded indicator history",
		"symbolID", symbolID,
		"interval", primaryInterval,
		"bars", len(resp.Candlesticks),
		"oldestTime", resp.Candlesticks[0].Time,
		"newestTime", resp.Candlesticks[len(resp.Candlesticks)-1].Time,
	)
}

// indicatorEventTap is a 0-output EventBus handler that copies every
// IndicatorEvent's primary snapshot into the pipeline's
// latestIndicators / latestLastPrice cache. The decision recorder's
// StanceProvider reads from that cache, so by registering this tap before
// the recorder (priority 25 vs recorder's 99 on EventTypeIndicator) we
// guarantee the recorder sees the same indicators it is about to record.
type indicatorEventTap struct {
	pipeline *EventDrivenPipeline
}

func (t *indicatorEventTap) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
	ev, ok := event.(entity.IndicatorEvent)
	if !ok {
		return nil, nil
	}
	t.pipeline.indicatorMu.Lock()
	t.pipeline.latestIndicators = ev.Primary
	t.pipeline.latestLastPrice = ev.LastPrice
	t.pipeline.hasLatestIndicators = true
	t.pipeline.indicatorMu.Unlock()
	return nil, nil
}

// currentStance returns the stance the resolver classifies for the most
// recently observed IndicatorEvent. Returns "UNKNOWN" when the resolver is
// not wired or no indicator event has fired yet (e.g. immediately after a
// restart, before the first PT15M close).
func (p *EventDrivenPipeline) currentStance(ctx context.Context) string {
	if p.stanceResolver == nil {
		return "UNKNOWN"
	}
	p.indicatorMu.RLock()
	if !p.hasLatestIndicators {
		p.indicatorMu.RUnlock()
		return "UNKNOWN"
	}
	indicators := p.latestIndicators
	lastPrice := p.latestLastPrice
	p.indicatorMu.RUnlock()
	res := p.stanceResolver.Resolve(ctx, indicators, lastPrice)
	return string(res.Stance)
}

// liveIntervalToDuration mirrors live.parseInterval but kept private here
// so cmd doesn't reach into the live package's unexported helpers.
func liveIntervalToDuration(s string) time.Duration {
	switch s {
	case "PT1M":
		return time.Minute
	case "PT5M":
		return 5 * time.Minute
	case "PT15M":
		return 15 * time.Minute
	case "PT30M":
		return 30 * time.Minute
	case "PT1H":
		return time.Hour
	case "PT4H":
		return 4 * time.Hour
	case "P1D":
		return 24 * time.Hour
	default:
		return 0
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
			p.currencyPair = s.CurrencyPair
			slog.Info("event-pipeline: loaded symbol meta",
				"symbolID", symbolID,
				"baseStepAmount", p.baseStepAmount,
				"minOrderAmount", p.minOrderAmount,
				"currencyPair", p.currencyPair,
			)
			return
		}
	}
	slog.Warn("event-pipeline: symbol not found in API response", "symbolID", symbolID)
}

// eventSnapshot is a copy of config fields taken under lock.
type eventSnapshot struct {
	symbolID       int64
	tradeAmount    float64
	baseStepAmount float64
	minOrderAmount float64
	minConfidence  float64
	riskPolicy     risk.RiskPolicy
}

func (p *EventDrivenPipeline) snapshotLocked() eventSnapshot {
	return eventSnapshot{
		symbolID:       p.symbolID,
		tradeAmount:    p.tradeAmount,
		baseStepAmount: p.baseStepAmount,
		minOrderAmount: p.minOrderAmount,
		minConfidence:  p.minConfidence,
		riskPolicy:     p.riskPolicy,
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
	liveSource := live.NewLiveSource(snap.symbolID, p.primaryInterval)

	// Bootstrap the in-progress 15-min bar from PT1M candles so the first
	// emit after a restart has correct OHLC instead of just post-restart
	// ticks. Best-effort: failures are logged and we fall back to the
	// legacy "first bar only sees post-restart ticks" behaviour.
	p.seedCandleBuilderFromMinutes(ctx, snap.symbolID, liveSource)

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
	//
	// The handler is constructed with the policy locked in, so there is
	// no follow-up SetX call the live wiring could forget. Compare with
	// the previous PR (#TBD) which had to add SetATRMultipliers to fix a
	// silent gap — the constructor signature now makes that gap a
	// compile error.
	tickRiskHandler := backtest.NewTickRiskHandlerWithPolicy(
		p.primaryInterval,
		executor,
		policyView(snap.riskPolicy),
	)
	bus.Register(entity.EventTypeTick, 15, tickRiskHandler)

	// IndicatorHandler: calculates technical indicators on candle close (priority 10).
	indicatorHandler := backtest.NewIndicatorHandler(p.primaryInterval, p.higherTFInterval, 500)
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
	// Prime the candle history before subscribing to the WS so the very
	// first live bar already has SMA/RSI/MACD/BB/ATR populated. Defaults
	// need 52 bars (Ichimoku Senkou B); 64 gives margin without making the
	// API call expensive.
	p.seedIndicatorHistory(ctx, snap.symbolID, p.primaryInterval, indicatorHandler, 64)
	bus.Register(entity.EventTypeCandle, 10, indicatorHandler)

	// StrategyHandler: signal generation from indicators (priority 20).
	strategyHandler := &backtest.StrategyHandler{Strategy: p.strategy}
	bus.Register(entity.EventTypeIndicator, 20, strategyHandler)

	// RiskHandler: risk gating for signals (priority 30).
	//
	// The backtest runner attaches a positionsize.Sizer + EquityFunc + PeakTracker
	// here so the strategy's profile-driven position_sizing block actually shapes
	// per-trade lot. Live used to skip this wiring, which left every signal
	// hitting the risk manager with TradeAmount as a literal coin count
	// (e.g. 1000 LTC × ¥8.8k = ¥8.8M) and the position-limit guard rejecting
	// every BUY. Live now mirrors the runner's wiring so the sizer the strategy
	// was tuned against is the same one production runs against.
	riskHandler := &backtest.RiskHandler{
		RiskManager:     p.riskMgr,
		TradeAmount:     snap.tradeAmount,
		StopLossPercent: snap.riskPolicy.StopLoss.Percent,
		MinConfidence:   snap.minConfidence,
		ExitOnSignal:    p.exitOnSignal,
		Executor:        executor,
	}
	if ps := p.positionSizing; ps != nil && ps.Mode != "" && ps.Mode != "fixed" {
		defaults := positionsize.VenueDefaults(p.currencyPair)
		riskHandler.Sizer = positionsize.New(ps, defaults)
		rm := p.riskMgr
		riskHandler.Equity = backtest.EquityFunc(func() float64 { return rm.LocalBalance() })
		if p.initialBalance > 0 {
			riskHandler.Peak = backtest.NewPeakTracker(p.initialBalance)
		}
		slog.Info("event-pipeline: position sizer attached",
			"mode", ps.Mode,
			"riskPerTradePct", ps.RiskPerTradePct,
			"minLot", ps.MinLot,
			"initialBalance", p.initialBalance,
			"currencyPair", p.currencyPair,
		)
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
	// PR3 (Signal/Decision/ExecutionPolicy three-layer separation): execution
	// is now driven by the Decision layer. DecisionHandler at priority 27
	// sits after StrategyHandler (20) / indicatorEventTap (25); RiskHandler
	// receives ActionDecisionEvent at priority 30 (replacing the legacy
	// EventTypeSignal route) and OrderEvent at priority 50 so it can arm
	// the entry cooldown via RiskManager.NoteClose on close fills.
	// PositionView is the executor-backed implementation that returns the
	// *net* side per symbol — the structural fix for the two-sided sum bug
	// that motivated the whole separation.
	decisionHandler := decision.NewHandler(decision.Config{
		Positions: decision.ExecutorPositionView{Executor: executor},
		Cooldown:  p.riskMgr,
	})
	bus.Register(entity.EventTypeMarketSignal, 27, decisionHandler)
	bus.Register(entity.EventTypeDecision, 30, riskHandler)
	bus.Register(entity.EventTypeOrder, 50, riskHandler)

	// ExitPlan shadow (priority 60): OrderEvent をシャドウで listen して
	// ExitPlan の作成・close だけ行う。発注パスには干渉しない。Phase 2b で
	// 出口判定本体を Exit レイヤに移管したらこの shadow は退役する。
	if p.exitPlanRepo != nil {
		shadow := usecaseexitplan.NewShadowHandler(usecaseexitplan.ShadowHandlerConfig{
			Repo:   p.exitPlanRepo,
			Policy: snap.riskPolicy,
		})
		bus.Register(entity.EventTypeOrder, 60, shadow)

		// ATRSource (priority 26): IndicatorEvent から ATR を吸い上げて
		// ExitPlan の動的計算に使う in-memory 値を保持する。indicatorEventTap
		// (25) の直後、recorder (99) より前。
		atrSrc := usecaseexitplan.NewATRSource()
		bus.Register(entity.EventTypeIndicator, 26, atrSrc)

		// TrailingPersistenceHandler (priority 16): TickEvent ごとに ExitPlan
		// の HWM を更新する。既存 TickRiskHandler (priority 15) の直後に
		// 置くことで発火後に残った plan の HWM を更新する順序になる。
		// Phase 2a では既存発火経路には影響を与えず、永続化された HWM を
		// 観察するだけ (Phase 2b で TickRiskHandler を置き換える)。
		trailing := usecaseexitplan.NewTrailingPersistenceHandler(usecaseexitplan.TrailingPersistenceConfig{
			Repo: p.exitPlanRepo,
		})
		bus.Register(entity.EventTypeTick, 16, trailing)

		slog.Info("event-pipeline: ExitPlan shadow + trailing persistence registered (Phase 1+2a)")
	}

	// ExecutionHandler: opens orders from approved signals (priority 40).
	executionHandler := &backtest.ExecutionHandler{
		Executor:    executor,
		TradeAmount: snap.tradeAmount,
	}
	bus.Register(entity.EventTypeApproved, 40, executionHandler)

	// indicator tap: caches every IndicatorEvent so the recorder's
	// StanceProvider (and the tick-driven order rows) can re-resolve the
	// current stance without reaching back through the strategy. Priority
	// 25 places this between the StrategyHandler (20) and the recorder
	// (99) so the recorder sees the snapshot it's about to persist.
	bus.Register(entity.EventTypeIndicator, 25, &indicatorEventTap{pipeline: p})

	if p.decisionLogRepo != nil {
		recorder := decisionlog.NewRecorder(p.decisionLogRepo, decisionlog.RecorderConfig{
			SymbolID:        snap.symbolID,
			CurrencyPair:    p.currencyPair,
			PrimaryInterval: p.primaryInterval,
			StanceProvider: func() string {
				return p.currentStance(ctx)
			},
		})
		bus.Register(entity.EventTypeIndicator, 99, recorder)
		bus.Register(entity.EventTypeSignal, 99, recorder)
		bus.Register(entity.EventTypeMarketSignal, 99, recorder)
		bus.Register(entity.EventTypeDecision, 99, recorder)
		bus.Register(entity.EventTypeApproved, 99, recorder)
		bus.Register(entity.EventTypeRejected, 99, recorder)
		bus.Register(entity.EventTypeOrder, 99, recorder)
		slog.Info("event-pipeline: decision recorder attached",
			"symbolID", snap.symbolID, "currencyPair", p.currencyPair,
			"stanceResolverWired", p.stanceResolver != nil)
	}

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
