package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// PipelineController はTrading Pipelineの開始/停止を制御するインターフェース。
type PipelineController interface {
	Start()
	Stop()
	Running() bool
}

type BotHandler struct {
	riskMgr     *usecase.RiskManager
	realtimeHub *usecase.RealtimeHub
	pipeline    PipelineController
}

func NewBotHandler(riskMgr *usecase.RiskManager, realtimeHub *usecase.RealtimeHub, pipeline PipelineController) *BotHandler {
	return &BotHandler{riskMgr: riskMgr, realtimeHub: realtimeHub, pipeline: pipeline}
}

func (h *BotHandler) Start(c *gin.Context) {
	h.riskMgr.StartTrading()
	if h.pipeline != nil {
		h.pipeline.Start()
	}
	status := h.riskMgr.GetStatus()
	resp := gin.H{
		"status":          "running",
		"tradingHalted":   status.TradingHalted,
		"manuallyStopped": status.ManuallyStopped,
		"pipelineRunning": h.pipeline != nil && h.pipeline.Running(),
	}
	if h.realtimeHub != nil {
		_ = h.realtimeHub.PublishData("status", 0, resp)
	}
	c.JSON(http.StatusOK, resp)
}

func (h *BotHandler) Stop(c *gin.Context) {
	if h.pipeline != nil {
		h.pipeline.Stop()
	}
	h.riskMgr.StopTrading()
	status := h.riskMgr.GetStatus()
	resp := gin.H{
		"status":          "stopped",
		"tradingHalted":   status.TradingHalted,
		"manuallyStopped": status.ManuallyStopped,
		"pipelineRunning": false,
	}
	if h.realtimeHub != nil {
		_ = h.realtimeHub.PublishData("status", 0, resp)
	}
	c.JSON(http.StatusOK, resp)
}
