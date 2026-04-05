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
	c.JSON(http.StatusOK, gin.H{
		"status":        "running",
		"tradingHalted": status.TradingHalted,
		"balance":       status.Balance,
		"dailyLoss":     status.DailyLoss,
		"totalPosition": status.TotalPosition,
	})
}
