package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

// runMultiBacktestRequest is the POST /backtest/run-multi body shape. Unlike
// runBacktestRequest this endpoint requires periods with explicit labels and
// shares the rest of the parameters (profile, tradeAmount, costs) across
// every period. Per-period overrides are out of scope for PR-2.
type runMultiBacktestRequest struct {
	DataPath              string  `json:"data" binding:"required"`
	DataHTFPath           string  `json:"dataHtf"`
	InitialBalance        float64 `json:"initialBalance"`
	Spread                float64 `json:"spread"`
	CarryingCost          float64 `json:"carryingCost"`
	Slippage              float64 `json:"slippage"`
	TradeAmount           float64 `json:"tradeAmount"`
	StopLossPercent       float64 `json:"stopLossPercent"`
	StopLossATRMultiplier float64 `json:"stopLossAtrMultiplier"` // PR-12
	TrailingATRMultiplier float64 `json:"trailingAtrMultiplier"` // PR-12
	TakeProfitPercent     float64 `json:"takeProfitPercent"`
	MaxPositionAmount     float64 `json:"maxPositionAmount"`
	MaxDailyLoss          float64 `json:"maxDailyLoss"`

	// Execution-quality knobs propagated to every period (PR-Q3 follow-up).
	// Empty / zero values fall back to legacy behaviour (percent slippage,
	// no fees, no book gate). Mirrors runBacktestRequest.
	SlippageModel        string  `json:"slippageModel,omitempty"`
	MakerFillProbability float64 `json:"makerFillProbability,omitempty"`
	MakerFeeRate         float64 `json:"makerFeeRate,omitempty"`
	TakerFeeRate         float64 `json:"takerFeeRate,omitempty"`
	MaxSlippageBps       float64 `json:"maxSlippageBps,omitempty"`
	MaxBookSidePct       float64 `json:"maxBookSidePct,omitempty"`
	LatencyMs            int64   `json:"latencyMs,omitempty"`

	Periods []entity.PeriodSpec `json:"periods" binding:"required"`

	ProfileName    string  `json:"profileName,omitempty"`
	PDCACycleID    string  `json:"pdcaCycleId,omitempty"`
	Hypothesis     string  `json:"hypothesis,omitempty"`
	ParentResultID *string `json:"parentResultId,omitempty"`
}

// RunMulti handles POST /backtest/run-multi. It loads the CSV(s) and profile
// once, fans out to BacktestRunner in parallel via MultiPeriodRunner, saves
// every per-period result into backtest_results and the envelope into
// multi_period_results, then returns the hydrated envelope.
func (h *BacktestHandler) RunMulti(c *gin.Context) {
	if h.runner == nil || h.repo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backtest services are not configured"})
		return
	}
	if h.multiRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "multi-period repository is not configured"})
		return
	}

	var req runMultiBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Periods) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "periods must contain at least one entry"})
		return
	}

	// Load profile upfront so all periods share the same ConfigurableStrategy.
	baseDir := h.profilesBaseDir
	if baseDir == "" {
		baseDir = defaultProfilesBaseDir
	}
	profile, err := loadProfileForRequest(baseDir, req.ProfileName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Apply profile defaults onto zero-valued request fields. Re-use the
	// existing helpers to keep behaviour consistent with POST /backtest/run.
	shared := runBacktestRequest{
		DataPath:              req.DataPath,
		DataHTFPath:           req.DataHTFPath,
		InitialBalance:        req.InitialBalance,
		Spread:                req.Spread,
		CarryingCost:          req.CarryingCost,
		Slippage:              req.Slippage,
		TradeAmount:           req.TradeAmount,
		StopLossPercent:       req.StopLossPercent,
		StopLossATRMultiplier: req.StopLossATRMultiplier,
		TrailingATRMultiplier: req.TrailingATRMultiplier,
		TakeProfitPercent:     req.TakeProfitPercent,
		MaxPositionAmount:     req.MaxPositionAmount,
		MaxDailyLoss:          req.MaxDailyLoss,
	}
	// resolveRiskProfile redirects router profiles to their default
	// child's Risk so per-run SL/TP/ATR defaults match one of the
	// routed strategies rather than the legacy SL=5/TP=10 fallback.
	// See resolveRiskProfile for the per-regime SL/TP limitation.
	applyProfileDefaults(&shared, resolveRiskProfile(baseDir, profile))
	applyLegacyDefaults(&shared)

	// Load CSVs once and share across all period runs. Each period uses the
	// same candle slice; only the time-window filter changes.
	primary, err := csvinfra.LoadCandles(shared.DataPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to load primary csv: " + err.Error()})
		return
	}
	var higherCandles []entity.Candle
	if shared.DataHTFPath != "" {
		htf, err := csvinfra.LoadCandles(shared.DataHTFPath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to load higher tf csv: " + err.Error()})
			return
		}
		higherCandles = htf.Candles
	}

	// When a profile is specified, build a fresh Strategy per period.
	// ConfigurableStrategy is stateless and could be shared, but a
	// ProfileRouter (regime_routing profile) carries detector
	// hysteresis state across bars — sharing one router across N
	// periods would let one period's regime memory bleed into the
	// next one. Build per period to avoid that.
	loader := strategyprofile.NewLoader(baseDir)
	buildStrategy := func() (port.Strategy, error) {
		if profile == nil {
			return nil, nil
		}
		return strategyuc.BuildStrategyFromProfile(loader, profile)
	}
	if profile != nil {
		// Smoke-test the build once up front so a bad profile fails
		// the whole request with HTTP 400 rather than producing N-1
		// successful periods + 1 failure.
		if _, err := buildStrategy(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid profile: " + err.Error()})
			return
		}
	}

	mpRunner := bt.NewMultiPeriodRunner()
	mpInput := bt.MultiPeriodInput{
		ProfileName:    req.ProfileName,
		PDCACycleID:    req.PDCACycleID,
		Hypothesis:     req.Hypothesis,
		ParentResultID: req.ParentResultID,
		Periods:        req.Periods,
		RunInputForPeriod: func(p entity.PeriodSpec) (*bt.BacktestRunner, bt.RunInput, error) {
			fromTs, err := parseBacktestDateStart(p.From)
			if err != nil {
				return nil, bt.RunInput{}, err
			}
			toTs, err := parseBacktestDateEnd(p.To)
			if err != nil {
				return nil, bt.RunInput{}, err
			}
			if fromTs == 0 && len(primary.Candles) > 0 {
				fromTs = primary.Candles[0].Time
			}
			if toTs == 0 && len(primary.Candles) > 0 {
				toTs = primary.Candles[len(primary.Candles)-1].Time
			}

			cfg := entity.BacktestConfig{
				Symbol:               primary.Symbol,
				SymbolID:             primary.SymbolID,
				PrimaryInterval:      primary.Interval,
				HigherTFInterval:     "PT1H",
				FromTimestamp:        fromTs,
				ToTimestamp:          toTs,
				InitialBalance:       shared.InitialBalance,
				SpreadPercent:        shared.Spread,
				DailyCarryCost:       shared.CarryingCost,
				SlippagePercent:      shared.Slippage,
				SlippageModel:        req.SlippageModel,
				MakerFillProbability: req.MakerFillProbability,
				MakerFeeRate:         req.MakerFeeRate,
				TakerFeeRate:         req.TakerFeeRate,
				LatencyMs:            req.LatencyMs,
			}
			if len(higherCandles) == 0 {
				cfg.HigherTFInterval = ""
			}

			var runner *bt.BacktestRunner
			if profile != nil {
				strat, err := buildStrategy()
				if err != nil {
					return nil, bt.RunInput{}, fmt.Errorf("build strategy: %w", err)
				}
				runner = bt.NewBacktestRunner(bt.WithStrategy(strat))
			} else {
				runner = h.runner
			}
			// cycle44: plumb profile.StanceRules.BBSqueezeLookback so
			// the IndicatorHandler's RecentSqueeze actually respects
			// the profile. Zero falls back to the legacy default of 5.
			var bbLookback int
			var positionSizing *entity.PositionSizingConfig
			var indicatorPeriods entity.IndicatorConfig
			if profile != nil {
				resolved := resolveRiskProfile(baseDir, profile)
				bbLookback = resolved.StanceRules.BBSqueezeLookback
				positionSizing = resolved.Risk.PositionSizing
				indicatorPeriods = resolved.Indicators
			}
			fillSrc, bookSrc, buildErr := h.buildExecutionSourcesForCfg(c.Request.Context(), cfg)
			if buildErr != nil {
				return nil, bt.RunInput{}, buildErr
			}
			input := bt.RunInput{
				Config:            cfg,
				TradeAmount:       shared.TradeAmount,
				PrimaryCandles:    primary.Candles,
				HigherCandles:     higherCandles,
				BBSqueezeLookback: bbLookback,
				IndicatorPeriods:  indicatorPeriods,
				PositionSizing:    positionSizing,
				FillPriceSource:   fillSrc,
				BookSource:        bookSrc,
				RiskConfig: entity.RiskConfig{
					MaxPositionAmount:     shared.MaxPositionAmount,
					MaxDailyLoss:          shared.MaxDailyLoss,
					StopLossPercent:       shared.StopLossPercent,
					StopLossATRMultiplier: shared.StopLossATRMultiplier,
					TrailingATRMultiplier: shared.TrailingATRMultiplier,
					TakeProfitPercent:     shared.TakeProfitPercent,
					InitialCapital:        shared.InitialBalance,
					MaxSlippageBps:        req.MaxSlippageBps,
					MaxBookSidePct:        req.MaxBookSidePct,
				},
			}
			return runner, input, nil
		},
	}

	envelope, err := mpRunner.Run(c.Request.Context(), mpInput)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Persist each per-period BacktestResult individually and then the
	// envelope. Per-period rows get the PDCA metadata so existing
	// profileName / pdcaCycleId filters on /backtest/results keep working.
	for i := range envelope.Periods {
		envelope.Periods[i].Result.ProfileName = req.ProfileName
		envelope.Periods[i].Result.PDCACycleID = req.PDCACycleID
		envelope.Periods[i].Result.Hypothesis = req.Hypothesis
		// Periods inherit the envelope-level ParentResultID so tree
		// filters on backtest_results still work per-period.
		envelope.Periods[i].Result.ParentResultID = req.ParentResultID
		if err := h.repo.Save(c.Request.Context(), envelope.Periods[i].Result); err != nil {
			if errors.Is(err, repository.ErrParentResultSelfReference) ||
				errors.Is(err, repository.ErrParentResultNotFound) {
				c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save period result: " + err.Error()})
			return
		}
	}
	if err := h.multiRepo.Save(c.Request.Context(), *envelope); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save multi-period envelope: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, envelope)
}

// ListMultiResults handles GET /backtest/multi-results.
func (h *BacktestHandler) ListMultiResults(c *gin.Context) {
	if h.multiRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "multi-period repository is not configured"})
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

	filter := repository.MultiPeriodResultFilter{
		Limit:       limit,
		Offset:      offset,
		ProfileName: c.Query("profileName"),
		PDCACycleID: c.Query("pdcaCycleId"),
	}

	results, err := h.multiRepo.List(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// GetMultiResult handles GET /backtest/multi-results/:id.
func (h *BacktestHandler) GetMultiResult(c *gin.Context) {
	if h.multiRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "multi-period repository is not configured"})
		return
	}
	id := c.Param("id")
	result, err := h.multiRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "multi-period result not found"})
		return
	}
	c.JSON(http.StatusOK, result)
}
