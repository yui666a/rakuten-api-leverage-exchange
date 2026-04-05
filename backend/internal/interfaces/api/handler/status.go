package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type StatusHandler struct {
	riskMgr *usecase.RiskManager
}

func NewStatusHandler(riskMgr *usecase.RiskManager) *StatusHandler {
	return &StatusHandler{riskMgr: riskMgr}
}

func (h *StatusHandler) GetStatus(c *gin.Context) {
	status := h.riskMgr.GetStatus()
	engineStatus := statusLabel(status)

	c.JSON(http.StatusOK, gin.H{
		"status":          engineStatus,
		"tradingHalted":   status.TradingHalted,
		"manuallyStopped": status.ManuallyStopped,
		"balance":         status.Balance,
		"dailyLoss":       status.DailyLoss,
		"totalPosition":   status.TotalPosition,
	})
}

func statusLabel(status usecase.RiskStatus) string {
	if status.ManuallyStopped {
		return "stopped"
	}
	return "running"
}
