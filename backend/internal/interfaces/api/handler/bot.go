package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type BotHandler struct {
	riskMgr *usecase.RiskManager
}

func NewBotHandler(riskMgr *usecase.RiskManager) *BotHandler {
	return &BotHandler{riskMgr: riskMgr}
}

func (h *BotHandler) Start(c *gin.Context) {
	h.riskMgr.StartTrading()
	status := h.riskMgr.GetStatus()
	c.JSON(http.StatusOK, gin.H{
		"status":          "running",
		"tradingHalted":   status.TradingHalted,
		"manuallyStopped": status.ManuallyStopped,
	})
}

func (h *BotHandler) Stop(c *gin.Context) {
	h.riskMgr.StopTrading()
	status := h.riskMgr.GetStatus()
	c.JSON(http.StatusOK, gin.H{
		"status":          "stopped",
		"tradingHalted":   status.TradingHalted,
		"manuallyStopped": status.ManuallyStopped,
	})
}
