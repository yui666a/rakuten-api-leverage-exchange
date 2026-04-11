package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type RiskHandler struct {
	riskMgr     *usecase.RiskManager
	realtimeHub *usecase.RealtimeHub
	pnlCalc     *usecase.DailyPnLCalculator
}

func NewRiskHandler(riskMgr *usecase.RiskManager, realtimeHub *usecase.RealtimeHub, pnlCalc *usecase.DailyPnLCalculator) *RiskHandler {
	return &RiskHandler{riskMgr: riskMgr, realtimeHub: realtimeHub, pnlCalc: pnlCalc}
}

func (h *RiskHandler) GetConfig(c *gin.Context) {
	status := h.riskMgr.GetStatus()
	c.JSON(http.StatusOK, status.Config)
}

func validateRiskConfig(cfg entity.RiskConfig) error {
	if cfg.MaxPositionAmount <= 0 {
		return fmt.Errorf("maxPositionAmount must be positive")
	}
	if cfg.MaxDailyLoss <= 0 {
		return fmt.Errorf("maxDailyLoss must be positive")
	}
	if cfg.StopLossPercent <= 0 {
		return fmt.Errorf("stopLossPercent must be positive")
	}
	if cfg.InitialCapital <= 0 {
		return fmt.Errorf("initialCapital must be positive")
	}
	return nil
}

func (h *RiskHandler) UpdateConfig(c *gin.Context) {
	var req entity.RiskConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateRiskConfig(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.riskMgr.UpdateConfig(req)
	if h.realtimeHub != nil {
		_ = h.realtimeHub.PublishData("config", 0, req)
		status := h.riskMgr.GetStatus()
		_ = h.realtimeHub.PublishData("status", 0, gin.H{
			"status":          statusLabel(status),
			"tradingHalted":   status.TradingHalted,
			"manuallyStopped": status.ManuallyStopped,
			"balance":         status.Balance,
			"dailyLoss":       status.DailyLoss,
			"totalPosition":   status.TotalPosition,
		})
	}
	c.JSON(http.StatusOK, req)
}

func (h *RiskHandler) GetPnL(c *gin.Context) {
	status := h.riskMgr.GetStatus()

	resp := gin.H{
		"balance":       status.Balance,
		"dailyLoss":     status.DailyLoss,
		"totalPosition": status.TotalPosition,
		"tradingHalted": status.TradingHalted,
	}

	if h.pnlCalc != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		pnl, err := h.pnlCalc.Compute(ctx)
		if err != nil {
			slog.Warn("daily pnl compute failed", "error", err)
			// エラー時は dailyPnl ブロック自体を省略 (フロントは undefined として handle する)
		} else {
			resp["dailyPnl"] = pnl
		}
	}

	c.JSON(http.StatusOK, resp)
}
