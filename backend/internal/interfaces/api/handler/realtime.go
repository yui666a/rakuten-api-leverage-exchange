package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	"nhooyr.io/websocket"
)

type RealtimeHandler struct {
	marketDataSvc *usecase.MarketDataService
	riskMgr       *usecase.RiskManager
	realtimeHub   *usecase.RealtimeHub
}

func NewRealtimeHandler(
	marketDataSvc *usecase.MarketDataService,
	riskMgr *usecase.RiskManager,
	realtimeHub *usecase.RealtimeHub,
) *RealtimeHandler {
	return &RealtimeHandler{
		marketDataSvc: marketDataSvc,
		riskMgr:       riskMgr,
		realtimeHub:   realtimeHub,
	}
}

func (h *RealtimeHandler) Stream(c *gin.Context) {
	if h.realtimeHub == nil {
		c.JSON(503, gin.H{"error": "realtime hub unavailable"})
		return
	}

	symbolStr := c.DefaultQuery("symbolId", "7")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid symbol ID"})
		return
	}

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := c.Request.Context()
	if err := h.writeInitialSnapshot(ctx, conn, symbolID); err != nil {
		return
	}

	sub := h.realtimeHub.Subscribe()
	defer h.realtimeHub.Unsubscribe(sub)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub:
			if !ok {
				return
			}
			if event.SymbolID != 0 && event.SymbolID != symbolID {
				continue
			}

			writeCtx, cancel := context.WithTimeout(ctx, websocketWriteTimeout)
			err := conn.Write(writeCtx, websocket.MessageText, mustMarshalEvent(event))
			cancel()
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.Warn("frontend websocket write failed", "error", err)
				}
				return
			}
		}
	}
}

func (h *RealtimeHandler) writeInitialSnapshot(ctx context.Context, conn *websocket.Conn, symbolID int64) error {
	status := h.riskMgr.GetStatus()
	initialEvents := []usecase.RealtimeEvent{
		mustRealtimeEvent("status", 0, gin.H{
			"status":          statusLabel(status),
			"tradingHalted":   status.TradingHalted,
			"manuallyStopped": status.ManuallyStopped,
			"balance":         status.Balance,
			"dailyLoss":       status.DailyLoss,
			"totalPosition":   status.TotalPosition,
		}),
		mustRealtimeEvent("config", 0, status.Config),
	}

	if latest, err := h.marketDataSvc.GetLatestTicker(ctx, symbolID); err == nil && latest != nil {
		initialEvents = append(initialEvents, mustRealtimeEvent("ticker", latest.SymbolID, latest))
	}

	for _, event := range initialEvents {
		writeCtx, cancel := context.WithTimeout(ctx, websocketWriteTimeout)
		err := conn.Write(writeCtx, websocket.MessageText, mustMarshalEvent(event))
		cancel()
		if err != nil {
			return err
		}
	}

	return nil
}

const websocketWriteTimeout = 5 * time.Second

func mustRealtimeEvent(eventType string, symbolID int64, payload any) usecase.RealtimeEvent {
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return usecase.RealtimeEvent{
		Type:     eventType,
		SymbolID: symbolID,
		Data:     data,
	}
}

func mustMarshalEvent(event usecase.RealtimeEvent) []byte {
	payload, err := json.Marshal(event)
	if err != nil {
		panic(err)
	}
	return payload
}
