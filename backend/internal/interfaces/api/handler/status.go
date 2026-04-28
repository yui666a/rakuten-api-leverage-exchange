package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// StatusHandler は /api/v1/status を扱う。
//
// 「自動売買が動いているか」は2つの独立した条件の AND で決まる:
//  1. ユーザが手で停止していない (RiskManager.ManuallyStopped == false)
//  2. pipeline goroutine が実際に走っている (PipelineController.Running() == true)
//
// 過去の実装は 1 だけを見ていたため、プロセス再起動直後 (ManuallyStopped=false
// だが pipeline.Start() を呼んでいない状態) を "running" と詐称し、シグナルを
// 取りこぼしているのに画面上は正常に見える事故が起きた (2026-04-28)。
type StatusHandler struct {
	riskMgr  *usecase.RiskManager
	pipeline PipelineController
}

func NewStatusHandler(riskMgr *usecase.RiskManager, pipeline PipelineController) *StatusHandler {
	return &StatusHandler{riskMgr: riskMgr, pipeline: pipeline}
}

func (h *StatusHandler) GetStatus(c *gin.Context) {
	status := h.riskMgr.GetStatus()
	pipelineRunning := h.pipeline != nil && h.pipeline.Running()
	engineStatus := statusLabel(status, pipelineRunning)

	c.JSON(http.StatusOK, gin.H{
		"status":          engineStatus,
		"tradingHalted":   status.TradingHalted,
		"manuallyStopped": status.ManuallyStopped,
		"pipelineRunning": pipelineRunning,
		"balance":         status.Balance,
		"dailyLoss":       status.DailyLoss,
		"totalPosition":   status.TotalPosition,
	})
}

func statusLabel(status usecase.RiskStatus, pipelineRunning bool) string {
	if status.ManuallyStopped {
		return "stopped"
	}
	if !pipelineRunning {
		return "stopped"
	}
	return "running"
}
