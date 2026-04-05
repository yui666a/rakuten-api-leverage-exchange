package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type BotHandler struct {
	riskMgr     *usecase.RiskManager
	realtimeHub *usecase.RealtimeHub
}

func NewBotHandler(riskMgr *usecase.RiskManager, realtimeHub *usecase.RealtimeHub) *BotHandler {
	return &BotHandler{riskMgr: riskMgr, realtimeHub: realtimeHub}
}

func (h *BotHandler) Start(c *gin.Context) {
	h.riskMgr.StartTrading()
	status := h.riskMgr.GetStatus()
	resp := gin.H{
		"status":          "running",
		"tradingHalted":   status.TradingHalted,
		"manuallyStopped": status.ManuallyStopped,
	}
	if h.realtimeHub != nil {
		_ = h.realtimeHub.PublishData("status", 0, resp)
	}
	c.JSON(http.StatusOK, resp)
}

func (h *BotHandler) Stop(c *gin.Context) {
	h.riskMgr.StopTrading()
	status := h.riskMgr.GetStatus()
	resp := gin.H{
		"status":          "stopped",
		"tradingHalted":   status.TradingHalted,
		"manuallyStopped": status.ManuallyStopped,
	}
	if h.realtimeHub != nil {
		_ = h.realtimeHub.PublishData("status", 0, resp)
	}
	c.JSON(http.StatusOK, resp)
}
