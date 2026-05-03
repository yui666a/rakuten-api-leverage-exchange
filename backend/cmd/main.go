package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/config"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
	backtestinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	backtestuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/circuitbreaker"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/decisionlog"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/quality"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/reconcile"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/sor"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

func main() {
	if err := godotenv.Load(); err != nil {
		slog.Info("no .env file found, using environment variables")
	}

	cfg := config.Load()

	// --- Database ---
	db, err := database.NewDB(cfg.Database.Path)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := database.RunMigrations(db); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// --- Infrastructure ---
	restClient := rakuten.NewRESTClient(cfg.Rakuten.BaseURL, cfg.Rakuten.APIKey, cfg.Rakuten.APISecret)
	wsClient := rakuten.NewWSClient(cfg.Rakuten.WSURL)
	marketDataRepo := database.NewMarketDataRepo(db)
	tradeHistoryRepo := database.NewTradeHistoryRepo(db)
	riskStateRepo := database.NewRiskStateRepo(db)
	tradingConfigRepo := database.NewTradingConfigRepo(db)
	decisionLogRepo := database.NewDecisionLogRepository(db)
	backtestDecisionLogRepo := database.NewBacktestDecisionLogRepository(db)

	// --- Usecase ---
	marketDataSvc := usecase.NewMarketDataServiceWithConfig(marketDataRepo, loadPersistenceConfig())
	realtimeHub := usecase.NewRealtimeHub()
	marketDataSvc.SetRealtimeHub(realtimeHub)
	indicatorCalc := usecase.NewIndicatorCalculator(marketDataRepo)
	stanceOverrideRepo := database.NewStanceOverrideRepo(db)
	clientOrderRepo := database.NewClientOrderRepo(db)
	backtestResultRepo := backtestinfra.NewResultRepository(db)
	multiPeriodRepo := backtestinfra.NewMultiPeriodResultRepository(db, backtestResultRepo)
	walkForwardRepo := backtestinfra.NewWalkForwardResultRepository(db)
	stanceResolver := usecase.NewRuleBasedStanceResolver(stanceOverrideRepo)
	strategyEngine := usecase.NewStrategyEngine(stanceResolver)
	// The StrategyRegistry lives in the strategy package and is exercised by
	// its own unit tests. It is intentionally not wired here yet because no
	// downstream code consumes it; leaving dead infrastructure at the
	// composition root would be misleading. It will be wired in the PR that
	// introduces CLI/API strategy-profile selection.
	defaultStrategy := strategyuc.NewDefaultStrategy(strategyEngine)
	backtestRunner := backtestuc.NewBacktestRunner()
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount:     cfg.Risk.MaxPositionAmount,
		MaxDailyLoss:          cfg.Risk.MaxDailyLoss,
		StopLossPercent:       cfg.Risk.StopLossPercent,
		StopLossATRMultiplier: cfg.Risk.StopLossATRMultiplier,
		TakeProfitPercent:     cfg.Risk.TakeProfitPercent,
		InitialCapital:        cfg.Risk.InitialCapital,
		MaxConsecutiveLosses:  cfg.Risk.MaxConsecutiveLosses,
		CooldownMinutes:       cfg.Risk.CooldownMinutes,
		MaxSlippageBps:        cfg.Risk.MaxSlippageBps,
		MaxBookSidePct:        cfg.Risk.MaxBookSidePct,
	})
	orderExecutor := usecase.NewOrderExecutor(restClient, riskMgr)
	// Browser notifications subscribe to "trade_event" on the realtime hub —
	// wire the executor so successful opens/closes publish there.
	orderExecutor.SetRealtimeHub(realtimeHub)
	// Same hub doubles as the risk-event channel so the front-end
	// notification UI gets DD / consecutive-loss / daily-loss warnings.
	riskMgr.SetRealtimeHub(realtimeHub)

	// Default to the config-file values, then let the persisted state (if any)
	// override them. This is what fixes the "WS resubscribes to BTC after every
	// docker restart even though I picked LTC last time" silent failure: the
	// pipeline now boots with the user's last selection.
	symbolID := cfg.Trading.SymbolID
	tradeAmount := cfg.Trading.TradeAmount
	if persisted, err := tradingConfigRepo.Load(context.Background()); err != nil {
		slog.Warn("trading config restore failed; falling back to config defaults", "error", err)
	} else if persisted != nil {
		if persisted.SymbolID > 0 {
			symbolID = persisted.SymbolID
		}
		if persisted.TradeAmount > 0 {
			tradeAmount = persisted.TradeAmount
		}
		slog.Info("trading config restored from db", "symbolID", symbolID, "tradeAmount", tradeAmount)
	}

	// Load the live strategy profile so the IndicatorCalculator (used by API
	// /indicators handlers + bootstrap) and the EventDrivenPipeline both see
	// the same lookback periods the strategy was tuned for. Failure falls
	// back to legacy defaults so a missing / malformed profile does not
	// prevent the live pipeline from starting — but we log the error so
	// operators see what happened.
	liveProfile := loadLiveProfile()
	if liveProfile != nil {
		indicatorCalc.SetIndicatorPeriods(liveProfile.Indicators)
		if liveProfile.StanceRules.BBSqueezeLookback > 0 {
			indicatorCalc.SetBBSqueezeLookback(liveProfile.StanceRules.BBSqueezeLookback)
		}
		// PR4 (Phase 1): apply profile-driven BookGate / EntryCooldown
		// overrides on top of the env-var defaults already in riskMgr.
		// Zero values in the profile leave the env-var fallback untouched.
		riskMgr.ApplyProfileRiskOverrides(
			liveProfile.Risk.MaxSlippageBps,
			liveProfile.Risk.MaxBookSidePct,
			liveProfile.Risk.EntryCooldownSec,
		)
		slog.Info("profile risk overrides applied",
			"profile", liveProfile.Name,
			"maxSlippageBps", liveProfile.Risk.MaxSlippageBps,
			"maxBookSidePct", liveProfile.Risk.MaxBookSidePct,
			"entryCooldownSec", liveProfile.Risk.EntryCooldownSec,
		)
	}

	if err := bootstrapCandles(context.Background(), restClient, marketDataSvc, symbolID, "PT15M", 500); err != nil {
		slog.Warn("initial candle bootstrap failed", "error", err)
	}

	// --- Risk State Restore ---
	restoreRiskState(context.Background(), riskStateRepo, riskMgr)
	runBacktestRetentionCleanup(context.Background(), backtestResultRepo, cfg.Backtest.RetentionDays)

	// Build the strategy risk policy from the loaded profile, falling
	// back to env values when the profile leaves a knob unset. Validate
	// up front so a misconfigured policy fails before the pipeline
	// subscribes to ticks — the previous behaviour silently HOLD-only'd
	// when stop_loss_percent reached the sizer as 0.
	livePolicy, policyErr := risk.FromProfile(liveProfile, cfg.Risk.StopLossPercent, cfg.Risk.TakeProfitPercent)
	if policyErr != nil && liveProfile != nil {
		// Only fatal on real configuration errors; ErrEmptyPolicy is the
		// nil-profile signal and is handled by the env-fallback path
		// inside FromProfile.
		slog.Error("invalid live risk policy", "error", policyErr)
		os.Exit(1)
	}
	if err := livePolicy.Validate(); err != nil {
		slog.Error("invalid live risk policy", "error", err)
		os.Exit(1)
	}

	// --- Trading Pipeline (Event-Driven) ---
	pipeline := NewEventDrivenPipeline(
		EventDrivenPipelineConfig{
			SymbolID:             symbolID,
			StateSyncInterval:    time.Duration(cfg.Trading.StateSyncIntervalSec) * time.Second,
			TradeAmount:          tradeAmount,
			MinConfidence:        cfg.Trading.MinConfidence,
			RiskPolicy:           livePolicy,
			SOR:                  loadSORConfig(),
			CircuitBreaker:       circuitbreaker.Config{
				AbnormalSpreadPct:    cfg.CircuitBreaker.AbnormalSpreadPct,
				AbnormalSpreadHoldMs: cfg.CircuitBreaker.AbnormalSpreadHoldMs,
				PriceJumpPct:         cfg.CircuitBreaker.PriceJumpPct,
				PriceJumpWindowMs:    cfg.CircuitBreaker.PriceJumpWindowMs,
				BookFeedStaleAfterMs: cfg.CircuitBreaker.BookFeedStaleAfterMs,
				EmptyBookHoldMs:      cfg.CircuitBreaker.EmptyBookHoldMs,
			},
			StaleCheckIntervalMs: cfg.CircuitBreaker.StaleCheckIntervalMs,
			Reconcile: reconcile.Config{
				Enable:          cfg.Reconcile.Enable,
				IntervalSec:     cfg.Reconcile.IntervalSec,
				PositionWarnPct: cfg.Reconcile.PositionWarnPct,
				PositionHaltPct: cfg.Reconcile.PositionHaltPct,
				BalanceWarnPct:  cfg.Reconcile.BalanceWarnPct,
				BalanceHaltPct:  cfg.Reconcile.BalanceHaltPct,
				OrderTTL:        time.Duration(cfg.Reconcile.OrderTTLSec) * time.Second,
			},
			IndicatorPeriods:   liveProfileIndicators(liveProfile),
			BBSqueezeLookback:  liveProfileBBSqueezeLookback(liveProfile),
			DecisionLogRepo:    decisionLogRepo,
			CandlestickFetcher: restClient,
			StanceResolver:     stanceResolver,
			PrimaryInterval:    livePrimaryIntervalFromEnv(),
			HigherTFInterval:   liveHigherTFIntervalFromEnv(),
			PositionSizing:     liveProfilePositionSizing(liveProfile),
			InitialBalance:     cfg.Risk.InitialCapital,
			ExitOnSignal:       liveProfileExitOnSignal(liveProfile),
		},
		restClient,
		restClient, // SymbolFetcher
		marketDataSvc,
		defaultStrategy,
		riskMgr,
		tradeHistoryRepo,
		riskStateRepo,
		clientOrderRepo,
	)

	// 初期シンボルの baseStepAmount / minOrderAmount をロード
	pipeline.mu.Lock()
	pipeline.loadSymbolMeta(context.Background(), symbolID)
	pipeline.mu.Unlock()

	// 起動時にポジション・残高を同期
	pipeline.syncStateInitial(context.Background())

	// --- Graceful Shutdown context ---
	// NewRouter より先に ctx/cancel を定義する。onSymbolSwitch クロージャが ctx を
	// キャプチャし、NewRouter に OnSymbolSwitch として渡すため。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Decision-log retention sweep (backtest only). The 3-day window matches
	// the design contract; live decision_log is never auto-purged.
	decisionLogRetention := decisionlog.NewRetentionCleanup(backtestDecisionLogRepo, decisionlog.RetentionConfig{
		MaxAge:   72 * time.Hour,
		Interval: 1 * time.Hour,
	})
	go decisionLogRetention.Run(ctx)
	slog.Info("decisionlog retention started", "maxAge", "72h", "interval", "1h")

	// --- Symbol Switch channel + callback ---
	// symbolSwitchCh は pipeline 側から startMarketRelay に切替を伝える。
	// バッファ1の上書き方式: 古い値が取り残されていたら drain して新しい値を入れる。
	symbolSwitchCh := make(chan [2]int64, 1)

	onSymbolSwitch := func(oldID, newID int64) {
		// 新シンボルのローソク足を bootstrap（main の ctx を使う）
		if err := bootstrapCandles(ctx, restClient, marketDataSvc, newID, "PT15M", 500); err != nil {
			slog.Warn("candle bootstrap for new symbol failed", "symbolID", newID, "error", err)
		}

		// Persist so the next restart boots with the user's choice instead of
		// the config default. Save errors are logged but don't fail the switch
		// — losing persistence is less bad than refusing the switch.
		if err := tradingConfigRepo.Save(ctx, repository.TradingConfigState{
			SymbolID:    newID,
			TradeAmount: pipeline.TradeAmount(),
		}); err != nil {
			slog.Warn("persist trading config failed", "symbolID", newID, "error", err)
		}

		// 上書き方式: 古い値を drain してから送信。
		// SwitchSymbol は pipeline の switchMu でシリアライズされているため、
		// この関数が並行実行されることはない（drain + send の atomicity は不要）。
		select {
		case <-symbolSwitchCh:
		default:
		}
		select {
		case symbolSwitchCh <- [2]int64{oldID, newID}:
		case <-ctx.Done():
		}
	}

	// --- REST API ---
	dailyPnLCalc := usecase.NewDailyPnLCalculator(restClient, 10*time.Second)

	executionQualityReporter := quality.New(
		restClient,
		marketDataRepo,
		quality.HaltStatusFunc(func() (bool, string) {
			s := riskMgr.GetStatus()
			return s.ManuallyStopped || s.TradingHalted, s.HaltReason
		}),
	)
	executionQualityRepo := database.NewExecutionQualityRepo(db)

	// Snapshot worker. Polls the reporter every 60 s and stores the result
	// so /execution-quality can serve cached values without hitting the
	// venue. Retention sweep removes rows older than 7 days.
	go startExecutionQualitySnapshotWorker(
		context.Background(),
		executionQualityReporter,
		executionQualityRepo,
		pipeline,
	)

	router := api.NewRouter(api.Dependencies{
		RiskManager:         riskMgr,
		StanceResolver:      stanceResolver,
		IndicatorCalculator: indicatorCalc,
		MarketDataService:   marketDataSvc,
		RealtimeHub:         realtimeHub,
		OrderClient:         restClient,
		OrderExecutor:       orderExecutor,
		Pipeline:            pipeline,
		RESTClient:          restClient,
		ClientOrderRepo:     clientOrderRepo,
		BacktestRunner:        backtestRunner,
		BacktestResultRepo:    backtestResultRepo,
		MultiPeriodResultRepo: multiPeriodRepo,
		WalkForwardResultRepo: walkForwardRepo,
		OnSymbolSwitch:        onSymbolSwitch,
		DailyPnLCalculator:    dailyPnLCalc,
		ExecutionQualityReporter: executionQualityReporter,
		ExecutionQualityRepo:     executionQualityRepo,
		DecisionLogRepo:          decisionLogRepo,
		BacktestDecisionLogRepo:  backtestDecisionLogRepo,
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// REST API server
	go func() {
		slog.Info("REST API starting", "port", cfg.Server.Port)
		if err := router.Run(":" + cfg.Server.Port); err != nil {
			slog.Error("REST API server error", "error", err)
		}
	}()

	slog.Info("Trading Engine started",
		"maxPosition", cfg.Risk.MaxPositionAmount,
		"maxDailyLoss", cfg.Risk.MaxDailyLoss,
		"stopLoss", livePolicy.StopLoss.Percent,
		"takeProfit", livePolicy.TakeProfit.Percent,
		"stopLossATR", livePolicy.StopLoss.ATRMultiplier,
		"trailingMode", livePolicy.Trailing.Mode,
		"trailingATR", livePolicy.Trailing.ATRMultiplier,
		"capital", cfg.Risk.InitialCapital,
	)

	marketDataSvc.StartPersistenceWorker(ctx)
	go startMarketRelay(ctx, wsClient, marketDataSvc, realtimeHub, symbolID, symbolSwitchCh)
	go startDailyLossReset(ctx, riskMgr)
	go startBacktestRetentionCleanup(ctx, backtestResultRepo, cfg.Backtest.RetentionDays)
	// 残高・ポジションの定期同期は auto-trading の start/stop とは独立して常時回す。
	// これにより自動売買停止中でも画面の残高が楽天の実残高に追随し、起動直後に 20010
	// で失敗したケースも 15 秒ごとに再試行される。
	go pipeline.runStateSyncLoop(ctx)

	slog.Info("Trading pipeline ready",
		"tradeAmount", cfg.Trading.TradeAmount,
		"intervalSec", cfg.Trading.PipelineIntervalSec,
		"stateSyncIntervalSec", cfg.Trading.StateSyncIntervalSec,
	)

	// 再起動後の自動再開: ユーザが明示的に /api/v1/stop を押していない限り、
	// プロセス起動時に pipeline を自動で立ち上げる。これがないと、コード変更や
	// docker compose up での再起動のたびに手動で /start を叩かないと判定が
	// 走らず、画面 running 表示と実体の食い違いが起きる (2026-04-28 の事故)。
	if !riskMgr.GetStatus().ManuallyStopped {
		pipeline.Start()
		slog.Info("auto-trading resumed automatically (manuallyStopped=false)")
	} else {
		slog.Info("auto-trading paused: manuallyStopped=true. Use POST /api/v1/start to resume.")
	}

	// シグナル待機
	select {
	case sig := <-sigCh:
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
	case <-ctx.Done():
	}

	slog.Info("Trading Engine stopped")
}

const (
	wsMaxSessionDuration = 110 * time.Minute // 2時間制限の10分前に事前再接続
	wsInitialBackoff     = 1 * time.Second
	wsMaxBackoff         = 60 * time.Second

	// wsHeartbeatStaleAfter is the maximum gap between any two messages
	// (TICKER / ORDERBOOK / TRADES combined) before we declare the
	// WebSocket silently dead and force a reconnect. The venue normally
	// pushes ticker frames sub-second during active markets, so 60s is
	// well outside the legitimate silence band and tight enough that the
	// PT15M decision pipeline does not skip a whole bar's worth of data.
	//
	// 2026-04-26 incident root cause: a 7-hour silent WS gap left
	// IndicatorHandler with stale buffers, which combined with the
	// no-history-warmup bug to fill decision_log with all-HOLD rows.
	wsHeartbeatStaleAfter = 60 * time.Second

	// wsHeartbeatCheckInterval controls how often the watchdog wakes up
	// to compare lastMsgAt against now. Picked at 1/4 of the stale
	// threshold so the worst-case detection latency is bounded.
	wsHeartbeatCheckInterval = 15 * time.Second
)

// loadSORConfig reads SOR_* env vars and returns a sor.Config. Unset /
// unparseable values fall back to sor.New defaults.
//
// Env vars (all optional):
//   - SOR_STRATEGY            "market" (default) | "post_only_escalate"
//   - SOR_LIMIT_OFFSET_TICKS  integer ticks inside the touch (default 1)
//   - SOR_TICK_SIZE           float JPY (default 0.1 — LTC/JPY tick)
//   - SOR_ESCALATE_AFTER_MS   integer ms (default 30000)
//   - SOR_MIN_INTERVAL_MS     integer ms (default 250 — > venue 200ms limit)
func loadSORConfig() sor.Config {
	cfg := sor.Config{Strategy: sor.StrategyMarket}
	if v := strings.TrimSpace(os.Getenv("SOR_STRATEGY")); v != "" {
		switch strings.ToLower(v) {
		case string(sor.StrategyMarket):
			cfg.Strategy = sor.StrategyMarket
		case string(sor.StrategyPostOnlyEscalate):
			cfg.Strategy = sor.StrategyPostOnlyEscalate
		case string(sor.StrategyIceberg):
			cfg.Strategy = sor.StrategyIceberg
		}
	}
	if v, err := strconv.Atoi(os.Getenv("SOR_SLICE_COUNT")); err == nil && v > 0 {
		cfg.SliceCount = v
	}
	if v, err := strconv.Atoi(os.Getenv("SOR_LIMIT_OFFSET_TICKS")); err == nil && v >= 0 {
		cfg.LimitOffsetTicks = v
	}
	if v, err := strconv.ParseFloat(os.Getenv("SOR_TICK_SIZE"), 64); err == nil && v > 0 {
		cfg.TickSize = v
	}
	if v, err := strconv.ParseInt(os.Getenv("SOR_ESCALATE_AFTER_MS"), 10, 64); err == nil && v > 0 {
		cfg.EscalateAfterMs = v
	}
	if v, err := strconv.ParseInt(os.Getenv("SOR_MIN_INTERVAL_MS"), 10, 64); err == nil && v > 0 {
		cfg.MinIntervalMs = v
	}
	return cfg
}

// loadPersistenceConfig builds a PersistenceConfig from environment variables,
// falling back to DefaultPersistenceConfig() for anything unset or unparseable.
// Env vars (all optional):
//   - PERSIST_TICK_DATA            "true"/"false"   (default true)
//   - TICKER_PERSIST_INTERVAL_MS   integer ms       (default 1000)
//   - ORDERBOOK_PERSIST_INTERVAL_MS integer ms       (default 5000)
//   - ORDERBOOK_PERSIST_DEPTH      integer levels   (default 20)
//   - TICK_RETENTION_DAYS          integer days     (default 90)
//   - TICK_PERSIST_QUEUE_SIZE      integer          (default 1024)
func loadPersistenceConfig() usecase.PersistenceConfig {
	cfg := usecase.DefaultPersistenceConfig()

	if v := strings.TrimSpace(os.Getenv("PERSIST_TICK_DATA")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			cfg.Enable = true
		case "0", "false", "no", "off":
			cfg.Enable = false
		}
	}
	if v, err := strconv.Atoi(os.Getenv("TICKER_PERSIST_INTERVAL_MS")); err == nil && v >= 0 {
		cfg.TickerInterval = time.Duration(v) * time.Millisecond
	}
	if v, err := strconv.Atoi(os.Getenv("ORDERBOOK_PERSIST_INTERVAL_MS")); err == nil && v >= 0 {
		cfg.OrderbookInterval = time.Duration(v) * time.Millisecond
	}
	if v, err := strconv.Atoi(os.Getenv("ORDERBOOK_PERSIST_DEPTH")); err == nil && v >= 0 {
		cfg.OrderbookDepth = v
	}
	if v, err := strconv.Atoi(os.Getenv("TICK_RETENTION_DAYS")); err == nil && v >= 0 {
		cfg.RetentionDays = v
	}
	if v, err := strconv.Atoi(os.Getenv("TICK_PERSIST_QUEUE_SIZE")); err == nil && v > 0 {
		cfg.QueueSize = v
	}
	return cfg
}

// loadLiveProfile reads the live strategy profile from
// $LIVE_PROFILE (default: "production") under $PROFILES_BASE_DIR
// (default: "profiles"). Returns nil on any failure — the caller treats
// nil as "use legacy hardcoded defaults" so a malformed JSON or missing
// file does not block the live pipeline from starting; the error is
// logged loudly so operators see it.
//
// The profile is loaded once at startup. Hot-swapping requires a process
// restart, mirroring how the backtest path resolves a profile per run.
func loadLiveProfile() *entity.StrategyProfile {
	name := os.Getenv("LIVE_PROFILE")
	if name == "" {
		name = "production"
	}
	baseDir := os.Getenv("PROFILES_BASE_DIR")
	if baseDir == "" {
		baseDir = "profiles"
	}
	loader := strategyprofile.NewLoader(baseDir)
	profile, err := loader.Load(name)
	if err != nil {
		slog.Warn("live strategy profile load failed; falling back to legacy defaults", "name", name, "baseDir", baseDir, "error", err)
		return nil
	}
	slog.Info("live strategy profile loaded", "name", profile.Name, "baseDir", baseDir)
	return profile
}

// liveProfileIndicators returns the IndicatorConfig from a loaded profile,
// or the zero value (which WithDefaults turns into legacy values) when no
// profile is available.
func liveProfileIndicators(p *entity.StrategyProfile) entity.IndicatorConfig {
	if p == nil {
		return entity.IndicatorConfig{}
	}
	return p.Indicators
}

// liveProfileBBSqueezeLookback returns profile.StanceRules.BBSqueezeLookback
// or 0 (legacy fallback) when no profile is available.
func liveProfileBBSqueezeLookback(p *entity.StrategyProfile) int {
	if p == nil {
		return 0
	}
	return p.StanceRules.BBSqueezeLookback
}

// liveProfilePositionSizing returns profile.Risk.PositionSizing or nil when
// the profile is missing / unset. nil means the live RiskHandler keeps the
// legacy fixed-amount path; a non-nil pointer routes the live signal through
// the same positionsize.Sizer the backtest runner uses, so the profile that
// was tuned in PDCA actually shapes production lot size.
func liveProfilePositionSizing(p *entity.StrategyProfile) *entity.PositionSizingConfig {
	if p == nil {
		return nil
	}
	return p.Risk.PositionSizing
}

// liveProfileExitOnSignal returns profile.Risk.ExitOnSignal, defaulting to
// false when the profile is missing. Threading this through main lets the
// pipeline keep its profile-agnostic constructor signature while still
// honouring the profile's opt-in for Decision-driven exits.
func liveProfileExitOnSignal(p *entity.StrategyProfile) bool {
	if p == nil {
		return false
	}
	return p.Risk.ExitOnSignal
}

// livePrimaryIntervalFromEnv reads $LIVE_PRIMARY_INTERVAL with a PT15M
// fallback. Until profile JSONs declare their primary timeframe natively,
// an env var is the smallest knob that lets a PT5M-tuned profile run the
// live pipeline on PT5M without a code change.
func livePrimaryIntervalFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("LIVE_PRIMARY_INTERVAL")); v != "" {
		return v
	}
	return "" // empty → pipeline applies the PT15M legacy default
}

// liveHigherTFIntervalFromEnv mirrors livePrimaryIntervalFromEnv for the
// optional confirmation timeframe, defaulting to PT1H via the pipeline.
func liveHigherTFIntervalFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("LIVE_HIGHER_TF_INTERVAL")); v != "" {
		return v
	}
	return ""
}

func startMarketRelay(
	ctx context.Context,
	wsClient *rakuten.WSClient,
	marketDataSvc *usecase.MarketDataService,
	realtimeHub *usecase.RealtimeHub,
	initialSymbolID int64,
	symbolSwitchCh <-chan [2]int64,
) {
	if wsClient == nil || marketDataSvc == nil {
		return
	}

	currentSymbolID := initialSymbolID
	backoff := wsInitialBackoff

	for {
		select {
		case <-ctx.Done():
			_ = wsClient.Close()
			return
		default:
		}

		msgCh, err := wsClient.Connect(ctx)
		if err != nil {
			slog.Warn("market websocket connect failed", "error", err, "retryIn", backoff)
			waitFor(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		// Subscribe — 失敗時は Close して外側ループで reconnect する
		subscribeOK := true
		for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
			if err := wsClient.Subscribe(ctx, currentSymbolID, dataType); err != nil {
				slog.Warn("market websocket subscribe failed", "dataType", dataType, "error", err)
				subscribeOK = false
				break
			}
		}
		if !subscribeOK {
			_ = wsClient.Close()
			waitFor(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		slog.Info("market websocket subscribed", "symbolID", currentSymbolID)
		backoff = wsInitialBackoff // 接続成功でバックオフリセット

		// 2時間制限の事前再接続タイマー
		sessionTimer := time.NewTimer(wsMaxSessionDuration)

		// Silent-death watchdog: a 2026-04-26 incident showed the venue
		// can stop pushing frames for hours without sending a close. The
		// session timer fires only once per 110 min so it cannot catch
		// these gaps. We instead track lastMsgAt on every incoming frame
		// and force a reconnect whenever the gap exceeds the heartbeat
		// stale threshold.
		lastMsgAt := time.Now()
		heartbeatTicker := time.NewTicker(wsHeartbeatCheckInterval)

		reconnect := false
		for !reconnect {
			select {
			case <-ctx.Done():
				sessionTimer.Stop()
				heartbeatTicker.Stop()
				_ = wsClient.Close()
				return
			case <-sessionTimer.C:
				slog.Info("market websocket session approaching 2h limit, reconnecting proactively")
				reconnect = true
			case now := <-heartbeatTicker.C:
				if wsIsSilentlyDead(now, lastMsgAt, wsHeartbeatStaleAfter) {
					slog.Warn("market websocket silent beyond heartbeat threshold, reconnecting",
						"symbolID", currentSymbolID,
						"silenceFor", now.Sub(lastMsgAt).Round(time.Second).String(),
						"threshold", wsHeartbeatStaleAfter.String(),
					)
					reconnect = true
				}
			case ids := <-symbolSwitchCh:
				oldID, newID := ids[0], ids[1]
				slog.Info("switching websocket symbol subscription", "from", oldID, "to", newID)

				// Unsubscribe（エラーはログのみ — 古いシンボルが既に無効でも続行する）
				for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
					if err := wsClient.Unsubscribe(ctx, oldID, dataType); err != nil {
						slog.Warn("market websocket unsubscribe failed", "dataType", dataType, "error", err)
					}
				}

				// Subscribe（エラー時は reconnect。currentSymbolID は newID に進める）
				switchOK := true
				for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
					if err := wsClient.Subscribe(ctx, newID, dataType); err != nil {
						slog.Error("market websocket re-subscribe failed, will reconnect", "dataType", dataType, "error", err)
						switchOK = false
						break
					}
				}
				// pipeline 側は既に newID に切り替え済みなので、Subscribe 成否に関わらず
				// currentSymbolID を newID にして reconnect 時に新シンボルで再接続する
				currentSymbolID = newID
				if !switchOK {
					reconnect = true
				}
			case raw, ok := <-msgCh:
				if !ok {
					reconnect = true
					break
				}
				lastMsgAt = time.Now()
				handleMarketMessage(ctx, raw, marketDataSvc, realtimeHub)
			}
		}

		sessionTimer.Stop()
		heartbeatTicker.Stop()
		slog.Info("market websocket disconnected, reconnecting")
		_ = wsClient.Close()
		waitFor(ctx, wsInitialBackoff)
	}
}

// Raw structures for decoding Rakuten WebSocket messages where numeric values
// are delivered as JSON strings (e.g. "12345.67").
type rawTicker struct {
	SymbolID  int64                `json:"symbolId"`
	BestAsk   entity.StringFloat64 `json:"bestAsk"`
	BestBid   entity.StringFloat64 `json:"bestBid"`
	Open      entity.StringFloat64 `json:"open"`
	High      entity.StringFloat64 `json:"high"`
	Low       entity.StringFloat64 `json:"low"`
	Last      entity.StringFloat64 `json:"last"`
	Volume    entity.StringFloat64 `json:"volume"`
	Timestamp int64                `json:"timestamp"`
}

type rawOrderbookEntry struct {
	Price  entity.StringFloat64 `json:"price"`
	Amount entity.StringFloat64 `json:"amount"`
}

type rawMarketTrade struct {
	ID          int64                `json:"id"`
	OrderSide   string               `json:"orderSide"`
	Price       entity.StringFloat64 `json:"price"`
	Amount      entity.StringFloat64 `json:"amount"`
	AssetAmount entity.StringFloat64 `json:"assetAmount"`
	TradedAt    int64                `json:"tradedAt"`
}

type rawMarketTradesResponse struct {
	SymbolID  int64            `json:"symbolId"`
	Trades    []rawMarketTrade `json:"trades"`
	Timestamp int64            `json:"timestamp"`
}

type rawOrderbook struct {
	SymbolID  int64                `json:"symbolId"`
	Asks      []rawOrderbookEntry  `json:"asks"`
	Bids      []rawOrderbookEntry  `json:"bids"`
	BestAsk   entity.StringFloat64 `json:"bestAsk"`
	BestBid   entity.StringFloat64 `json:"bestBid"`
	MidPrice  entity.StringFloat64 `json:"midPrice"`
	Spread    entity.StringFloat64 `json:"spread"`
	Timestamp int64                `json:"timestamp"`
}

// detectMessageType はJSONのトップレベルキーを軽量パースしてメッセージ種別を判定する。
func detectMessageType(raw []byte) string {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "unknown"
	}
	if _, ok := probe["asks"]; ok {
		return "orderbook"
	}
	if _, ok := probe["trades"]; ok {
		return "trades"
	}
	return "ticker"
}

// handleMarketMessage routes a raw venue WS payload into MarketDataService.
// The realtime hub is owned by the service itself (set via SetRealtimeHub) so
// callers no longer need to pass it through here.
func handleMarketMessage(ctx context.Context, raw []byte, marketDataSvc *usecase.MarketDataService, _ *usecase.RealtimeHub) {
	if len(raw) == 0 {
		return
	}

	msgType := detectMessageType(raw)
	if msgType == "unknown" {
		slog.Debug("market websocket unknown message, skipping", "raw", string(raw))
		return
	}

	switch msgType {
	case "orderbook":
		var r rawOrderbook
		if err := json.Unmarshal(raw, &r); err != nil {
			slog.Warn("market websocket orderbook decode failed", "error", err)
			return
		}
		asks := make([]entity.OrderbookEntry, len(r.Asks))
		for i, a := range r.Asks {
			asks[i] = entity.OrderbookEntry{Price: a.Price.Float64(), Amount: a.Amount.Float64()}
		}
		bids := make([]entity.OrderbookEntry, len(r.Bids))
		for i, b := range r.Bids {
			bids[i] = entity.OrderbookEntry{Price: b.Price.Float64(), Amount: b.Amount.Float64()}
		}
		orderbook := entity.Orderbook{
			SymbolID:  r.SymbolID,
			Asks:      asks,
			Bids:      bids,
			BestAsk:   r.BestAsk.Float64(),
			BestBid:   r.BestBid.Float64(),
			MidPrice:  r.MidPrice.Float64(),
			Spread:    r.Spread.Float64(),
			Timestamp: r.Timestamp,
		}
		marketDataSvc.HandleOrderbook(ctx, orderbook)
	case "trades":
		var r rawMarketTradesResponse
		if err := json.Unmarshal(raw, &r); err != nil {
			slog.Warn("market websocket trades decode failed", "error", err)
			return
		}
		trades := entity.MarketTradesResponse{
			SymbolID:  r.SymbolID,
			Timestamp: r.Timestamp,
			Trades:    make([]entity.MarketTrade, len(r.Trades)),
		}
		for i, t := range r.Trades {
			trades.Trades[i] = entity.MarketTrade{
				ID:          t.ID,
				OrderSide:   t.OrderSide,
				Price:       t.Price.Float64(),
				Amount:      t.Amount.Float64(),
				AssetAmount: t.AssetAmount.Float64(),
				TradedAt:    t.TradedAt,
			}
		}
		marketDataSvc.HandleTrades(ctx, trades)
	case "ticker":
		var r rawTicker
		if err := json.Unmarshal(raw, &r); err != nil {
			slog.Warn("market websocket ticker decode failed", "error", err)
			return
		}
		ticker := entity.Ticker{
			SymbolID:  r.SymbolID,
			BestAsk:   r.BestAsk.Float64(),
			BestBid:   r.BestBid.Float64(),
			Open:      r.Open.Float64(),
			High:      r.High.Float64(),
			Low:       r.Low.Float64(),
			Last:      r.Last.Float64(),
			Volume:    r.Volume.Float64(),
			Timestamp: r.Timestamp,
		}
		marketDataSvc.HandleTicker(ctx, ticker)
	}
}

// wsIsSilentlyDead returns true when the gap between now and the most
// recent message timestamp exceeds the heartbeat threshold. Extracted so
// the threshold logic stays unit-testable without spinning up a real
// WebSocket — the production caller is the watchdog branch in
// startMarketRelay's select.
func wsIsSilentlyDead(now, lastMsgAt time.Time, threshold time.Duration) bool {
	return now.Sub(lastMsgAt) > threshold
}

func waitFor(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > wsMaxBackoff {
		return wsMaxBackoff
	}
	return next
}

func bootstrapCandles(
	ctx context.Context,
	restClient *rakuten.RESTClient,
	marketDataSvc *usecase.MarketDataService,
	symbolID int64,
	interval string,
	limit int,
) error {
	if restClient == nil || marketDataSvc == nil {
		return nil
	}

	resp, err := restClient.GetCandlestick(ctx, symbolID, interval, nil, nil)
	if err != nil {
		return err
	}

	candles := resp.Candlesticks
	if limit > 0 && len(candles) > limit {
		candles = candles[len(candles)-limit:]
	}

	if len(candles) == 0 {
		slog.Warn("candle bootstrap returned no candles", "symbolID", symbolID, "interval", interval)
		return nil
	}

	// INSERT OR IGNORE により既存データと重複しないため、毎回全件渡して差分のみ保存される
	if err := marketDataSvc.SaveCandles(ctx, symbolID, interval, candles); err != nil {
		return err
	}

	slog.Info("bootstrapped candles", "count", len(candles), "symbolID", symbolID, "interval", interval)
	return nil
}

func startBacktestRetentionCleanup(ctx context.Context, repo repository.BacktestResultRepository, retentionDays int) {
	if repo == nil {
		return
	}
	if retentionDays <= 0 {
		retentionDays = 180
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runBacktestRetentionCleanup(ctx, repo, retentionDays)
		}
	}
}

func runBacktestRetentionCleanup(ctx context.Context, repo repository.BacktestResultRepository, retentionDays int) {
	if repo == nil {
		return
	}
	if retentionDays <= 0 {
		retentionDays = 180
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	deleted, err := repo.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		slog.Warn("backtest retention cleanup failed", "error", err, "retentionDays", retentionDays)
		return
	}
	if deleted > 0 {
		slog.Info("backtest retention cleanup completed", "deleted", deleted, "retentionDays", retentionDays)
	}
}

// startExecutionQualitySnapshotWorker periodically captures the live
// execution-quality report and persists it so the API can serve cached
// values. Also sweeps rows older than 7 days once a day.
//
// Runs forever; the process lifecycle (signal handling in main) bounds it.
func startExecutionQualitySnapshotWorker(
	ctx context.Context,
	reporter *quality.Reporter,
	repo *database.ExecutionQualityRepo,
	pipeline *EventDrivenPipeline,
) {
	if reporter == nil || repo == nil || pipeline == nil {
		return
	}
	const (
		captureInterval = 60 * time.Second
		retentionDays   = 7
	)

	captureTicker := time.NewTicker(captureInterval)
	defer captureTicker.Stop()
	retentionTicker := time.NewTicker(24 * time.Hour)
	defer retentionTicker.Stop()

	capture := func() {
		symbolID := pipeline.SymbolID()
		if symbolID <= 0 {
			return
		}
		var report entity.ExecutionQualityReport
		err := retryOn20010(ctx, time.Sleep, func() error {
			r, e := reporter.Build(ctx, symbolID, 86400)
			if e != nil {
				return e
			}
			report = r
			return nil
		})
		if err != nil {
			slog.Warn("execution-quality snapshot failed", "error", err)
			return
		}
		if err := repo.Save(ctx, symbolID, time.Now().UnixMilli(), report); err != nil {
			slog.Warn("execution-quality snapshot save failed", "error", err)
		}
	}
	sweep := func() {
		cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).UnixMilli()
		deleted, err := repo.PurgeOlderThan(ctx, cutoff)
		if err != nil {
			slog.Warn("execution-quality retention sweep failed", "error", err)
			return
		}
		if deleted > 0 {
			slog.Info("execution-quality retention sweep", "deleted", deleted, "retentionDays", retentionDays)
		}
	}

	// Warm up: short delay then capture once so the cache is populated
	// before the first user request.
	select {
	case <-ctx.Done():
		return
	case <-time.After(15 * time.Second):
	}
	capture()
	sweep()

	for {
		select {
		case <-ctx.Done():
			return
		case <-captureTicker.C:
			capture()
		case <-retentionTicker.C:
			sweep()
		}
	}
}
