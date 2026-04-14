package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

type BacktestHandler struct {
	runner *bt.BacktestRunner
	repo   repository.BacktestResultRepository
}

func NewBacktestHandler(runner *bt.BacktestRunner, repo repository.BacktestResultRepository) *BacktestHandler {
	return &BacktestHandler{
		runner: runner,
		repo:   repo,
	}
}

type runBacktestRequest struct {
	DataPath       string  `json:"data" binding:"required"`
	DataHTFPath    string  `json:"dataHtf"`
	From           string  `json:"from"`
	To             string  `json:"to"`
	InitialBalance float64 `json:"initialBalance"`
	Spread         float64 `json:"spread"`
	CarryingCost   float64 `json:"carryingCost"`
	Slippage       float64 `json:"slippage"`
	TradeAmount    float64 `json:"tradeAmount"`
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

	result, err := h.runner.Run(context.Background(), bt.RunInput{
		Config:         cfg,
		TradeAmount:    req.TradeAmount,
		PrimaryCandles: primary.Candles,
		HigherCandles:  higherCandles,
		RiskConfig: entity.RiskConfig{
			MaxPositionAmount:    1_000_000_000,
			MaxDailyLoss:         1_000_000_000,
			StopLossPercent:      5,
			TakeProfitPercent:    10,
			InitialCapital:       req.InitialBalance,
			MaxConsecutiveLosses: 0,
			CooldownMinutes:      0,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := h.repo.Save(c.Request.Context(), *result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save backtest result: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
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

	results, err := h.repo.List(c.Request.Context(), repository.BacktestResultFilter{
		Limit:  limit,
		Offset: offset,
	})
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
