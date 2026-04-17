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
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

// defaultProfilesBaseDir mirrors the CLI default: strategy profiles live
// under backend/profiles/ at repository root. See spec §8.3 for why the
// path is relative and why profile names are restricted to [a-zA-Z0-9_-].
const defaultProfilesBaseDir = "profiles"

type BacktestHandler struct {
	runner          *bt.BacktestRunner
	repo            repository.BacktestResultRepository
	profilesBaseDir string
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
	DataPath             string  `json:"data" binding:"required"`
	DataHTFPath          string  `json:"dataHtf"`
	From                 string  `json:"from"`
	To                   string  `json:"to"`
	InitialBalance       float64 `json:"initialBalance"`
	Spread               float64 `json:"spread"`
	CarryingCost         float64 `json:"carryingCost"`
	Slippage             float64 `json:"slippage"`
	TradeAmount          float64 `json:"tradeAmount"`
	StopLossPercent      float64 `json:"stopLossPercent"`
	TakeProfitPercent    float64 `json:"takeProfitPercent"`
	MaxPositionAmount    float64 `json:"maxPositionAmount"`
	MaxDailyLoss         float64 `json:"maxDailyLoss"`
	MaxConsecutiveLosses int     `json:"maxConsecutiveLosses"`
	CooldownMinutes      int     `json:"cooldownMinutes"`

	// PDCA extensions (spec §8.2). All optional; when ProfileName is set,
	// the profile's values become the base and non-zero individual fields
	// above override them.
	ProfileName    string  `json:"profileName,omitempty"`
	PDCACycleID    string  `json:"pdcaCycleId,omitempty"`
	Hypothesis     string  `json:"hypothesis,omitempty"`
	ParentResultID *string `json:"parentResultId,omitempty"`
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
	profile, loadErr := loadProfileForRequest(baseDir, req.ProfileName)
	if loadErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": loadErr.Error()})
		return
	}

	// Apply profile defaults to zero-valued individual fields so spec §8.2's
	// precedence rule holds: profile first, then any non-zero individual
	// parameter in the request overrides.
	applyProfileDefaults(&req, profile)

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
	}
	if len(higherCandles) == 0 {
		cfg.HigherTFInterval = ""
	}

	// When a profile is specified, build a one-shot runner wired with a
	// ConfigurableStrategy. We do NOT mutate h.runner (it is shared across
	// requests) so profile selection is per-request.
	runner := h.runner
	if profile != nil {
		strat, err := strategyuc.NewConfigurableStrategy(profile)
		if err != nil {
			// A profile that loaded but fails strategy construction is still
			// caller-driven (the profile JSON is on disk because the caller
			// referenced it) — return 400 rather than 500.
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid profile: " + err.Error()})
			return
		}
		runner = bt.NewBacktestRunner(bt.WithStrategy(strat))
	}

	result, err := runner.Run(context.Background(), bt.RunInput{
		Config:         cfg,
		TradeAmount:    req.TradeAmount,
		PrimaryCandles: primary.Candles,
		HigherCandles:  higherCandles,
		RiskConfig: entity.RiskConfig{
			MaxPositionAmount:    req.MaxPositionAmount,
			MaxDailyLoss:         req.MaxDailyLoss,
			StopLossPercent:      req.StopLossPercent,
			TakeProfitPercent:    req.TakeProfitPercent,
			InitialCapital:       req.InitialBalance,
			MaxConsecutiveLosses: req.MaxConsecutiveLosses,
			CooldownMinutes:      req.CooldownMinutes,
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
