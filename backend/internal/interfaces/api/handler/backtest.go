package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	infrabt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/backtest"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

// orderbookReplayStaleAfterMs is how far we let a snapshot drift before
// declaring it stale. 60 s matches the design discussion: 5 s persist
// throttle × ~10 missed frames is treated as "venue feed gap" and the
// trade is skipped rather than filled at a stale price.
const orderbookReplayStaleAfterMs = 60_000

// orderbookReplayMinCoverageRatio is the minimum (snapshots / 5s buckets in
// window) ratio required before /backtest/run accepts an "orderbook"
// slippage model run. 0.8 = at most 20% of the bucketed window may be
// missing snapshots.
const orderbookReplayMinCoverageRatio = 0.8

// defaultProfilesBaseDir mirrors the CLI default: strategy profiles live
// under backend/profiles/ at repository root. See spec §8.3 for why the
// path is relative and why profile names are restricted to [a-zA-Z0-9_-].
const defaultProfilesBaseDir = "profiles"

type BacktestHandler struct {
	runner          *bt.BacktestRunner
	repo            repository.BacktestResultRepository
	profilesBaseDir string

	// multiRepo is optional; when nil the multi-period endpoints return 503.
	// This keeps legacy construction paths working without forcing all
	// callers to wire the new repo at once.
	multiRepo repository.MultiPeriodResultRepository

	// wfRepo is optional; when nil, walk-forward runs execute but are not
	// persisted and the GET endpoints return 503. PR-13 shipped compute-
	// only; wiring this repo re-enables /backtest/walk-forward/:id and
	// GET listing without touching the happy path.
	wfRepo repository.WalkForwardResultRepository

	// marketDataSvc supplies persisted L2 snapshots for slippageModel="orderbook".
	// When nil, that model returns 400 ("orderbook replay unavailable").
	marketDataSvc *usecase.MarketDataService
}

// BacktestHandlerOption configures optional aspects of a BacktestHandler at
// construction time. Using a functional option pattern avoids exposing a
// post-construction setter (which would race with concurrent HTTP requests)
// while still letting tests inject a temp profile directory.
type BacktestHandlerOption func(*BacktestHandler)

// WithProfilesBaseDir overrides the base directory used to resolve
// `profileName` in POST /backtest/run requests. Tests inject an absolute
// temp dir so they don't need to chdir. Production code relies on the
// default ("profiles") with cwd=backend/.
func WithProfilesBaseDir(dir string) BacktestHandlerOption {
	return func(h *BacktestHandler) {
		h.profilesBaseDir = dir
	}
}

// WithMultiPeriodRepo wires the MultiPeriodResultRepository so the
// /backtest/run-multi and /backtest/multi-results endpoints can function.
// Without it those endpoints return 503 (rather than panicking).
func WithMultiPeriodRepo(repo repository.MultiPeriodResultRepository) BacktestHandlerOption {
	return func(h *BacktestHandler) {
		h.multiRepo = repo
	}
}

// WithWalkForwardRepo wires the WalkForwardResultRepository so POST
// /backtest/walk-forward persists the envelope and the GET counterparts
// become available. Nil keeps the compute-only behaviour of PR-13.
func WithWalkForwardRepo(repo repository.WalkForwardResultRepository) BacktestHandlerOption {
	return func(h *BacktestHandler) {
		h.wfRepo = repo
	}
}

// WithMarketDataService enables slippageModel="orderbook" by giving the
// handler a way to load persisted L2 snapshots. nil keeps the legacy
// percent-only behaviour.
func WithMarketDataService(svc *usecase.MarketDataService) BacktestHandlerOption {
	return func(h *BacktestHandler) {
		h.marketDataSvc = svc
	}
}

func NewBacktestHandler(runner *bt.BacktestRunner, repo repository.BacktestResultRepository, opts ...BacktestHandlerOption) *BacktestHandler {
	h := &BacktestHandler{
		runner:          runner,
		repo:            repo,
		profilesBaseDir: defaultProfilesBaseDir,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

type runBacktestRequest struct {
	DataPath              string  `json:"data" binding:"required"`
	DataHTFPath           string  `json:"dataHtf"`
	From                  string  `json:"from"`
	To                    string  `json:"to"`
	InitialBalance        float64 `json:"initialBalance"`
	Spread                float64 `json:"spread"`
	CarryingCost          float64 `json:"carryingCost"`
	Slippage              float64 `json:"slippage"`
	// SlippageModel: "" / "percent" → legacy %-based adjustment.
	// "orderbook" → load persisted L2 snapshots for the run window and
	// compute VWAP fills against them. The handler returns 400 when the
	// snapshot coverage is insufficient.
	SlippageModel string `json:"slippageModel,omitempty"`
	TradeAmount           float64 `json:"tradeAmount"`
	StopLossPercent       float64 `json:"stopLossPercent"`
	StopLossATRMultiplier float64 `json:"stopLossAtrMultiplier"` // PR-12
	TrailingATRMultiplier float64 `json:"trailingAtrMultiplier"` // PR-12
	TakeProfitPercent     float64 `json:"takeProfitPercent"`
	MaxPositionAmount     float64 `json:"maxPositionAmount"`
	MaxDailyLoss          float64 `json:"maxDailyLoss"`
	MaxConsecutiveLosses  int     `json:"maxConsecutiveLosses"`
	CooldownMinutes       int     `json:"cooldownMinutes"`

	// PDCA extensions (spec §8.2). All optional; when ProfileName is set,
	// the profile's values become the base and non-zero individual fields
	// above override them.
	ProfileName    string  `json:"profileName,omitempty"`
	PDCACycleID    string  `json:"pdcaCycleId,omitempty"`
	Hypothesis     string  `json:"hypothesis,omitempty"`
	ParentResultID *string `json:"parentResultId,omitempty"`

	// PR-12: FE "edit & run" flow. When set, ProfileOverride supersedes
	// ProfileName for the strategy-construction path: the supplied
	// StrategyProfile is used directly (after Validate) instead of
	// loading a preset from disk. ProfileName is still recorded on the
	// result so the saved row shows which preset the user started from.
	// Router profiles are rejected here (the picker UI hides them from
	// edit-and-run) — editing a router without children is out of scope.
	ProfileOverride *entity.StrategyProfile `json:"profileOverride,omitempty"`
}

func (h *BacktestHandler) Run(c *gin.Context) {
	if h.runner == nil || h.repo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backtest services are not configured"})
		return
	}

	var req runBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Resolve / load the StrategyProfile up-front. Spec §8.3 requires that
	// invalid names, missing files, and profiles that fail Validate() all
	// surface as HTTP 400 (caller-driven errors), not 500.
	baseDir := h.profilesBaseDir
	if baseDir == "" {
		baseDir = defaultProfilesBaseDir
	}
	var profile *entity.StrategyProfile
	if req.ProfileOverride != nil {
		// PR-12: caller-supplied profile from the FE edit-and-run form.
		// Validate up-front so the error surfaces as 400 with the same
		// message shape as a preset that fails loadProfileForRequest.
		if err := req.ProfileOverride.Validate(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid profileOverride: " + err.Error()})
			return
		}
		if req.ProfileOverride.HasRouting() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "profileOverride must not carry regime_routing (router editing is out of scope for edit-and-run)"})
			return
		}
		profile = req.ProfileOverride
	} else {
		var loadErr error
		profile, loadErr = loadProfileForRequest(baseDir, req.ProfileName)
		if loadErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": loadErr.Error()})
			return
		}
	}

	// Apply profile defaults to zero-valued individual fields so spec §8.2's
	// precedence rule holds: profile first, then any non-zero individual
	// parameter in the request overrides. resolveRiskProfile redirects
	// router profiles (which carry no Risk of their own) to their
	// default child's Risk — see resolveRiskProfile for the limitation
	// notes.
	applyProfileDefaults(&req, resolveRiskProfile(baseDir, profile))

	// Legacy callers (no profile) still get the historical hard-coded
	// defaults when individual fields are zero.
	applyLegacyDefaults(&req)

	primary, err := csvinfra.LoadCandles(req.DataPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to load primary csv: " + err.Error()})
		return
	}

	var higherCandles []entity.Candle
	if req.DataHTFPath != "" {
		htf, err := csvinfra.LoadCandles(req.DataHTFPath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to load higher tf csv: " + err.Error()})
			return
		}
		higherCandles = htf.Candles
	}

	fromTs, err := parseBacktestDateStart(req.From)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	toTs, err := parseBacktestDateEnd(req.To)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if fromTs == 0 && len(primary.Candles) > 0 {
		fromTs = primary.Candles[0].Time
	}
	if toTs == 0 && len(primary.Candles) > 0 {
		toTs = primary.Candles[len(primary.Candles)-1].Time
	}

	cfg := entity.BacktestConfig{
		Symbol:           primary.Symbol,
		SymbolID:         primary.SymbolID,
		PrimaryInterval:  primary.Interval,
		HigherTFInterval: "PT1H",
		FromTimestamp:    fromTs,
		ToTimestamp:      toTs,
		InitialBalance:   req.InitialBalance,
		SpreadPercent:    req.Spread,
		DailyCarryCost:   req.CarryingCost,
		SlippagePercent:  req.Slippage,
		SlippageModel:    req.SlippageModel,
	}
	if len(higherCandles) == 0 {
		cfg.HigherTFInterval = ""
	}

	// When a profile is specified, build a one-shot runner wired with a
	// ConfigurableStrategy or a regime-aware ProfileRouter, depending on
	// whether the profile carries a regime_routing block. We do NOT
	// mutate h.runner (it is shared across requests) so profile
	// selection is per-request.
	runner := h.runner
	if profile != nil {
		strat, err := strategyuc.BuildStrategyFromProfile(strategyprofile.NewLoader(baseDir), profile)
		if err != nil {
			// A profile that loaded but fails strategy construction is still
			// caller-driven (the profile JSON is on disk because the caller
			// referenced it) — return 400 rather than 500.
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid profile: " + err.Error()})
			return
		}
		runner = bt.NewBacktestRunner(bt.WithStrategy(strat))
	}

	// cycle44: plumb the profile's bb_squeeze_lookback into the run so
	// the IndicatorHandler picks it up (legacy code hardcoded 5). Zero
	// value on the profile keeps the legacy default via the runner's
	// "only override if > 0" guard.
	var bbLookback int
	var positionSizing *entity.PositionSizingConfig
	if profile != nil {
		resolved := resolveRiskProfile(baseDir, profile)
		bbLookback = resolved.StanceRules.BBSqueezeLookback
		positionSizing = resolved.Risk.PositionSizing
	}

	var fillSource infrabt.FillPriceSource
	if req.SlippageModel == "orderbook" {
		var err error
		fillSource, err = h.buildOrderbookFillSource(c.Request.Context(), cfg)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	result, err := runner.Run(context.Background(), bt.RunInput{
		Config:            cfg,
		TradeAmount:       req.TradeAmount,
		PrimaryCandles:    primary.Candles,
		HigherCandles:     higherCandles,
		BBSqueezeLookback: bbLookback,
		PositionSizing:    positionSizing,
		FillPriceSource:   fillSource,
		RiskConfig: entity.RiskConfig{
			MaxPositionAmount:     req.MaxPositionAmount,
			MaxDailyLoss:          req.MaxDailyLoss,
			StopLossPercent:       req.StopLossPercent,
			StopLossATRMultiplier: req.StopLossATRMultiplier,
			TrailingATRMultiplier: req.TrailingATRMultiplier,
			TakeProfitPercent:     req.TakeProfitPercent,
			InitialCapital:        req.InitialBalance,
			MaxConsecutiveLosses:  req.MaxConsecutiveLosses,
			CooldownMinutes:       req.CooldownMinutes,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Attach PDCA metadata before Save so the repository's parent-integrity
	// checks (Task 5) actually see the parent_result_id value. Without this
	// wiring, Task 5's 422 guard could never fire via the HTTP layer.
	result.ProfileName = req.ProfileName
	result.PDCACycleID = req.PDCACycleID
	result.Hypothesis = req.Hypothesis
	result.ParentResultID = req.ParentResultID

	if err := h.repo.Save(c.Request.Context(), *result); err != nil {
		// parent_result_id integrity failures map to 422 so clients can
		// distinguish them from generic 500 persistence errors.
		if errors.Is(err, repository.ErrParentResultSelfReference) ||
			errors.Is(err, repository.ErrParentResultNotFound) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save backtest result: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// buildOrderbookFillSource validates that enough L2 snapshots exist for the
// requested run window and returns an OrderbookReplay-backed FillPriceSource.
// Returns a user-facing error (caller maps to HTTP 400) when:
//   - the handler was constructed without a MarketDataService
//   - the run window is open-ended (we need both endpoints to bound the load)
//   - fewer than orderbookReplayMinCoverageRatio of the 5 s buckets in the
//     window have a snapshot
func (h *BacktestHandler) buildOrderbookFillSource(ctx context.Context, cfg entity.BacktestConfig) (infrabt.FillPriceSource, error) {
	if h.marketDataSvc == nil {
		return nil, errors.New("orderbook replay unavailable: backend was started without market data persistence")
	}
	if cfg.FromTimestamp <= 0 || cfg.ToTimestamp <= 0 || cfg.ToTimestamp <= cfg.FromTimestamp {
		return nil, errors.New("orderbook replay requires bounded from/to timestamps")
	}

	snaps, err := h.marketDataSvc.GetOrderbookHistory(ctx, cfg.SymbolID, cfg.FromTimestamp, cfg.ToTimestamp, 1_000_000)
	if err != nil {
		return nil, errors.New("orderbook replay: failed to load snapshots: " + err.Error())
	}
	if len(snaps) == 0 {
		return nil, errors.New("orderbook replay: no L2 snapshots in requested window — backtest with slippageModel=\"percent\" instead, or wait for the persistence worker to accumulate data")
	}

	// Coverage = snapshots / expected_buckets, where the expected bucket size
	// is the persistence throttle (5 s). Falls back to a conservative 1.0
	// floor when the throttle is 0 (test config).
	bucketSec := int64(5)
	expected := (cfg.ToTimestamp - cfg.FromTimestamp) / 1000 / bucketSec
	if expected <= 0 {
		expected = 1
	}
	coverage := float64(len(snaps)) / float64(expected)
	if coverage < orderbookReplayMinCoverageRatio {
		return nil, fmtCoverageError(len(snaps), expected, coverage)
	}
	return infrabt.NewOrderbookReplay(snaps, orderbookReplayStaleAfterMs), nil
}

func fmtCoverageError(got int, expected int64, coverage float64) error {
	return errors.New(
		"orderbook replay: snapshot coverage too low (" +
			strconv.Itoa(got) + " of ~" + strconv.FormatInt(expected, 10) +
			" expected buckets, " +
			strconv.FormatFloat(coverage*100, 'f', 1, 64) +
			"%). Wait for more data or use slippageModel=\"percent\".",
	)
}

// loadProfileForRequest resolves and loads a profile by name. Returns
// (nil, nil) if name is empty (caller did not request a profile). Any error
// is caller-driven and should map to HTTP 400 at the handler layer.
func loadProfileForRequest(baseDir, name string) (*entity.StrategyProfile, error) {
	if name == "" {
		return nil, nil
	}
	// ResolveProfilePath rejects bad names (regex violation, traversal) and
	// the loader further rejects missing files / unknown JSON fields /
	// Validate failures. All of these are caller-driven (the caller asked
	// for this profile), so everything collapses to a single 400 surface.
	if _, err := strategyprofile.ResolveProfilePath(baseDir, name); err != nil {
		return nil, err
	}
	loader := strategyprofile.NewLoader(baseDir)
	profile, err := loader.Load(name)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// resolveRiskProfile returns the profile whose Risk fields should be
// used to populate per-run RiskConfig defaults.
//
// For flat profiles, this is the loaded profile itself. For routing
// profiles (PR-5 part B), the router's own Risk struct is empty by
// design — the router only declares routing rules, not exit thresholds.
// In that case we fall back to the *default child*'s Risk so the run's
// SL/TP/ATR settings are at least consistent with one of the routed
// strategies rather than the legacy hard-coded SL=5/TP=10 fallback.
//
// Known limitation (tracked as PR-5 part E): per-regime SL/TP
// differentiation is not yet implemented. The tickRiskHandler in the
// runner is constructed once per run with one fixed RiskConfig, so
// even though ProfileRouter swaps signal-generation per regime, every
// bar's exit logic uses the same SL/TP values. Promotion candidates
// surfaced before PR-5 part E are therefore "best of {default child
// risk, router signal mix}" — not "true regime-specialised risk".
//
// On any loader error for the child, we silently fall back to the
// router profile (i.e. legacy defaults will apply downstream). The
// router's own builder will reject the bad child later with a clearer
// 400, so we do not need to surface the lookup error here.
func resolveRiskProfile(baseDir string, profile *entity.StrategyProfile) *entity.StrategyProfile {
	if profile == nil || !profile.HasRouting() {
		return profile
	}
	defaultName := profile.RegimeRouting.Default
	if defaultName == "" {
		return profile
	}
	child, err := loadProfileForRequest(baseDir, defaultName)
	if err != nil || child == nil {
		return profile
	}
	return child
}

// applyProfileDefaults overlays the profile's risk values onto the request
// wherever the request left a field at its zero value. This runs BEFORE
// applyLegacyDefaults so profile values take precedence over the hard-coded
// fallbacks but defer to any non-zero individual field from the request.
func applyProfileDefaults(req *runBacktestRequest, profile *entity.StrategyProfile) {
	if profile == nil {
		return
	}
	if req.StopLossPercent <= 0 && profile.Risk.StopLossPercent > 0 {
		req.StopLossPercent = profile.Risk.StopLossPercent
	}
	if req.TakeProfitPercent <= 0 && profile.Risk.TakeProfitPercent > 0 {
		req.TakeProfitPercent = profile.Risk.TakeProfitPercent
	}
	if req.MaxPositionAmount <= 0 && profile.Risk.MaxPositionAmount > 0 {
		req.MaxPositionAmount = profile.Risk.MaxPositionAmount
	}
	if req.MaxDailyLoss <= 0 && profile.Risk.MaxDailyLoss > 0 {
		req.MaxDailyLoss = profile.Risk.MaxDailyLoss
	}
	// PR-12: route ATR multipliers into the backtest path. Prior to this,
	// stop_loss_atr_multiplier existed in the StrategyProfile entity but was
	// silently ignored by the backtest handler (only the live pipeline
	// consumed it via env var). trailing_atr_multiplier is new in PR-12.
	if req.StopLossATRMultiplier <= 0 && profile.Risk.StopLossATRMultiplier > 0 {
		req.StopLossATRMultiplier = profile.Risk.StopLossATRMultiplier
	}
	if req.TrailingATRMultiplier <= 0 && profile.Risk.TrailingATRMultiplier > 0 {
		req.TrailingATRMultiplier = profile.Risk.TrailingATRMultiplier
	}
}

// applyLegacyDefaults restores the historical zero-value fallbacks used by
// the handler before PDCA. Extracted so tests and the profile path share
// the same fallback logic.
func applyLegacyDefaults(req *runBacktestRequest) {
	if req.InitialBalance <= 0 {
		req.InitialBalance = 100000
	}
	if req.Spread <= 0 {
		req.Spread = 0.1
	}
	if req.CarryingCost <= 0 {
		req.CarryingCost = 0.04
	}
	if req.TradeAmount <= 0 {
		req.TradeAmount = 0.01
	}
	if req.StopLossPercent <= 0 {
		req.StopLossPercent = 5
	}
	if req.TakeProfitPercent <= 0 {
		req.TakeProfitPercent = 10
	}
	if req.MaxPositionAmount <= 0 {
		req.MaxPositionAmount = 1_000_000_000
	}
	if req.MaxDailyLoss <= 0 {
		req.MaxDailyLoss = 1_000_000_000
	}
}

func (h *BacktestHandler) ListResults(c *gin.Context) {
	if h.repo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backtest repository is not configured"})
		return
	}
	limit := 20
	offset := 0
	if v := c.Query("limit"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	if v := c.Query("offset"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset"})
			return
		}
		offset = parsed
	}
	if sort := c.Query("sort"); sort != "" && sort != "created_at:desc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sort must be created_at:desc"})
		return
	}

	filter := repository.BacktestResultFilter{
		Limit:       limit,
		Offset:      offset,
		ProfileName: c.Query("profileName"),
		PDCACycleID: c.Query("pdcaCycleId"),
	}

	// parentResultId: per spec §5.3 `(nil = フィルタなし)`. An empty string is a
	// legitimate filter value at the repository layer, but it has no useful
	// semantics as a real parent_result_id (domain values are UUIDs), so we
	// fold empty into "no filter" at the HTTP layer. This keeps the query
	// string ergonomic — callers can safely default `parentResultId=` to
	// disable the filter without having to strip the param from the URL.
	if v, present := c.GetQuery("parentResultId"); present && v != "" {
		filter.ParentResultID = &v
	}

	// hasParent: accept only "true"/"false" (idiomatic strconv.ParseBool,
	// aligns with JS booleans). Any other non-empty value is a 400.
	if v, present := c.GetQuery("hasParent"); present {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "hasParent must be true or false"})
			return
		}
		filter.HasParent = &parsed
	}

	// Precedence: when both parentResultId and hasParent are provided, the
	// specific ID wins. Drop HasParent at the handler layer so the repository
	// layer sees a single intent. Duplicating the rule here keeps the two
	// layers internally consistent and surfaces a debug log for observability.
	if filter.ParentResultID != nil && filter.HasParent != nil {
		slog.Debug("backtest list filter: parentResultId takes precedence over hasParent",
			"parentResultId", *filter.ParentResultID,
			"droppedHasParent", *filter.HasParent,
		)
		filter.HasParent = nil
	}

	results, err := h.repo.List(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

func (h *BacktestHandler) GetResult(c *gin.Context) {
	if h.repo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backtest repository is not configured"})
		return
	}
	id := c.Param("id")
	result, err := h.repo.FindByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "backtest result not found"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *BacktestHandler) CSVMeta(c *gin.Context) {
	dataPath := strings.TrimSpace(c.Query("data"))
	if dataPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "data query is required"})
		return
	}

	file, err := csvinfra.LoadCandles(dataPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to load csv: " + err.Error()})
		return
	}
	if len(file.Candles) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "csv has no candles"})
		return
	}

	fromTs := file.Candles[0].Time
	toTs := file.Candles[0].Time
	for _, candle := range file.Candles[1:] {
		if candle.Time < fromTs {
			fromTs = candle.Time
		}
		if candle.Time > toTs {
			toTs = candle.Time
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data":          dataPath,
		"symbol":        file.Symbol,
		"symbolId":      file.SymbolID,
		"interval":      file.Interval,
		"rowCount":      len(file.Candles),
		"fromTimestamp": fromTs,
		"toTimestamp":   toTs,
	})
}

func parseBacktestDateStart(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	loc, _ := time.LoadLocation("Asia/Tokyo")
	t, err := time.ParseInLocation("2006-01-02", v, loc)
	if err != nil {
		return 0, err
	}
	return t.UnixMilli(), nil
}

func parseBacktestDateEnd(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	loc, _ := time.LoadLocation("Asia/Tokyo")
	t, err := time.ParseInLocation("2006-01-02", v, loc)
	if err != nil {
		return 0, err
	}
	t = t.Add(24*time.Hour - time.Millisecond)
	return t.UnixMilli(), nil
}
