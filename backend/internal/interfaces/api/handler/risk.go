package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type RiskHandler struct {
	riskMgr     *usecase.RiskManager
	realtimeHub *usecase.RealtimeHub
}

func NewRiskHandler(riskMgr *usecase.RiskManager, realtimeHub *usecase.RealtimeHub) *RiskHandler {
	return &RiskHandler{riskMgr: riskMgr, realtimeHub: realtimeHub}
}

func (h *RiskHandler) GetConfig(c *gin.Context) {
	status := h.riskMgr.GetStatus()
	c.JSON(http.StatusOK, status.Config)
}

func (h *RiskHandler) UpdateConfig(c *gin.Context) {
	var req entity.RiskConfig
	if err := c.ShouldBindJSON(&req); err != nil {
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
	c.JSON(http.StatusOK, gin.H{
		"balance":       status.Balance,
		"dailyLoss":     status.DailyLoss,
		"totalPosition": status.TotalPosition,
		"tradingHalted": status.TradingHalted,
	})
}
