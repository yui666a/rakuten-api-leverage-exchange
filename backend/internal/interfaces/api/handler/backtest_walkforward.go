package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

// runWalkForwardRequest is the POST /backtest/walk-forward body shape.
// The shared backtest knobs (CSV paths, balance, risk caps) live here as
// flat fields; the walk-forward-specific knobs (windows, grid, objective)
// live under the explicit names documented in the design doc.
type runWalkForwardRequest struct {
	DataPath       string  `json:"data" binding:"required"`
	DataHTFPath    string  `json:"dataHtf"`
	InitialBalance float64 `json:"initialBalance"`
	Spread         float64 `json:"spread"`
	CarryingCost   float64 `json:"carryingCost"`
	Slippage       float64 `json:"slippage"`
	TradeAmount    float64 `json:"tradeAmount"`

	From              string `json:"from"`              // "YYYY-MM-DD"
	To                string `json:"to"`                // "YYYY-MM-DD"
	InSampleMonths    int    `json:"inSampleMonths"`    // default 6
	OutOfSampleMonths int    `json:"outOfSampleMonths"` // default 3
	StepMonths        int    `json:"stepMonths"`        // default 3

	BaseProfile   string                  `json:"baseProfile" binding:"required"`
	ParameterGrid []bt.ParameterOverride  `json:"parameterGrid"`
	Objective     string                  `json:"objective"` // "return" | "sharpe" | "profit_factor"

	PDCACycleID    string  `json:"pdcaCycleId,omitempty"`
	Hypothesis     string  `json:"hypothesis,omitempty"`
	ParentResultID *string `json:"parentResultId,omitempty"`
}

// RunWalkForward handles POST /backtest/walk-forward. It loads the CSV and
// profile once, expands the grid, computes the window schedule, then lets
// WalkForwardRunner drive IS selection and OOS scoring across every window.
//
// Persistence is intentionally out of scope for this MVP: the caller
// receives the full WalkForwardResult envelope in the HTTP response and
// each per-window BacktestResult appears inside that payload. A follow-up
// PR will add DB storage and a GET counterpart (see design doc's
// "Scope 変更" section).
func (h *BacktestHandler) RunWalkForward(c *gin.Context) {
	if h.runner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backtest runner is not configured"})
		return
	}

	var req runWalkForwardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Defaults mirror the design doc (IS=6, OOS=3, step=3).
	if req.InSampleMonths == 0 {
		req.InSampleMonths = 6
	}
	if req.OutOfSampleMonths == 0 {
		req.OutOfSampleMonths = 3
	}
	if req.StepMonths == 0 {
		req.StepMonths = 3
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
	if fromTs == 0 || toTs == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to are required for walk-forward"})
		return
	}
	fromT := time.UnixMilli(fromTs).UTC()
	toT := time.UnixMilli(toTs).UTC()

	windows, err := bt.ComputeWindows(fromT, toT, req.InSampleMonths, req.OutOfSampleMonths, req.StepMonths)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	grid, err := bt.ExpandGrid(req.ParameterGrid)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate the objective at the API boundary so a typo (e.g.
	// "returns" vs "return") fails the request up front instead of
	// silently defaulting to TotalReturn in SelectByObjective and
	// producing results that disagree with the caller's intent.
	switch req.Objective {
	case "", "return", "sharpe", "profit_factor":
		// ok; "" resolves to TotalReturn in SelectByObjective.
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid objective %q: must be one of return, sharpe, profit_factor", req.Objective),
		})
		return
	}

	// Pre-validate that every override path is supported by ApplyOverrides,
	// so an unsupported path surfaces as HTTP 400 up front instead of
	// deep inside WalkForwardRunner.Run where it would currently bubble
	// out as HTTP 500.
	for _, ov := range req.ParameterGrid {
		if _, err := bt.ApplyOverrides(entity.StrategyProfile{}, map[string]float64{ov.Path: 0}); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	baseDir := h.profilesBaseDir
	if baseDir == "" {
		baseDir = defaultProfilesBaseDir
	}
	baseProfile, err := loadProfileForRequest(baseDir, req.BaseProfile)
	if err != nil || baseProfile == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid base profile: %v", err)})
		return
	}

	// Legacy defaults for zero-valued risk/balance fields so the WFO runs
	// behave like a normal backtest.
	shared := runBacktestRequest{
		DataPath:       req.DataPath,
		DataHTFPath:    req.DataHTFPath,
		InitialBalance: req.InitialBalance,
		Spread:         req.Spread,
		CarryingCost:   req.CarryingCost,
		Slippage:       req.Slippage,
		TradeAmount:    req.TradeAmount,
	}
	applyProfileDefaults(&shared, baseProfile)
	applyLegacyDefaults(&shared)

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

	runner := bt.NewWalkForwardRunner()
	input := bt.WalkForwardInput{
		BaseProfile:    *baseProfile,
		Windows:        windows,
		Grid:           grid,
		Objective:      req.Objective,
		PDCACycleID:    req.PDCACycleID,
		Hypothesis:     req.Hypothesis,
		ParentResultID: req.ParentResultID,
		RunWindow: func(ctx context.Context, phase bt.WalkForwardPhase, profile entity.StrategyProfile, wFrom, wTo time.Time) (*entity.BacktestResult, error) {
			strat, err := strategyuc.NewConfigurableStrategy(&profile)
			if err != nil {
				return nil, fmt.Errorf("strategy: %w", err)
			}
			cfg := entity.BacktestConfig{
				Symbol:           primary.Symbol,
				SymbolID:         primary.SymbolID,
				PrimaryInterval:  primary.Interval,
				HigherTFInterval: "PT1H",
				FromTimestamp:    wFrom.UnixMilli(),
				ToTimestamp:      wTo.UnixMilli(),
				InitialBalance:   shared.InitialBalance,
				SpreadPercent:    shared.Spread,
				DailyCarryCost:   shared.CarryingCost,
				SlippagePercent:  shared.Slippage,
			}
			if len(higherCandles) == 0 {
				cfg.HigherTFInterval = ""
			}
			// Risk config must come from the per-combination profile —
			// otherwise parameter axes like strategy_risk.take_profit_percent
			// listed in the grid are silently ignored because every window
			// run would use the shared request-level value. Fall back to
			// the shared values (applied from baseProfile by
			// applyProfileDefaults) only when the per-combination profile
			// left a field at zero.
			risk := entity.RiskConfig{
				MaxPositionAmount:     nonZeroFloat(profile.Risk.MaxPositionAmount, shared.MaxPositionAmount),
				MaxDailyLoss:          nonZeroFloat(profile.Risk.MaxDailyLoss, shared.MaxDailyLoss),
				StopLossPercent:       nonZeroFloat(profile.Risk.StopLossPercent, shared.StopLossPercent),
				StopLossATRMultiplier: nonZeroFloat(profile.Risk.StopLossATRMultiplier, shared.StopLossATRMultiplier),
				TrailingATRMultiplier: nonZeroFloat(profile.Risk.TrailingATRMultiplier, shared.TrailingATRMultiplier),
				TakeProfitPercent:     nonZeroFloat(profile.Risk.TakeProfitPercent, shared.TakeProfitPercent),
				InitialCapital:        shared.InitialBalance,
			}
			windowRunner := bt.NewBacktestRunner(bt.WithStrategy(strat))
			result, err := windowRunner.Run(ctx, bt.RunInput{
				Config:         cfg,
				TradeAmount:    shared.TradeAmount,
				PrimaryCandles: primary.Candles,
				HigherCandles:  higherCandles,
				RiskConfig:     risk,
			})
			if err != nil {
				return nil, err
			}
			return result, nil
		},
	}

	out, err := runner.Run(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

// nonZeroFloat returns primary when it is strictly positive, otherwise the
// fallback. This matches the profile/request precedence used elsewhere in
// the backtest handlers (applyProfileDefaults), so a zero in the per-
// combination profile means "inherit the shared / baseline value" rather
// than "literally disable".
func nonZeroFloat(primary, fallback float64) float64 {
	if primary > 0 {
		return primary
	}
	return fallback
}
