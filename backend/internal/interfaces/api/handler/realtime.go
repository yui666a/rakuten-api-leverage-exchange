package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	"nhooyr.io/websocket"
)

type RealtimeHandler struct {
	marketDataSvc *usecase.MarketDataService
}

type realtimeMessage struct {
	Type string        `json:"type"`
	Data entity.Ticker `json:"data"`
}

func NewRealtimeHandler(marketDataSvc *usecase.MarketDataService) *RealtimeHandler {
	return &RealtimeHandler{marketDataSvc: marketDataSvc}
}

func (h *RealtimeHandler) StreamTicker(c *gin.Context) {
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
	if latest, err := h.marketDataSvc.GetLatestTicker(ctx, symbolID); err == nil && latest != nil {
		if err := conn.Write(ctx, websocket.MessageText, mustMarshalRealtimeMessage(*latest)); err != nil {
			return
		}
	}

	sub := h.marketDataSvc.SubscribeTicker()
	defer h.marketDataSvc.UnsubscribeTicker(sub)

	for {
		select {
		case <-ctx.Done():
			return
		case ticker, ok := <-sub:
			if !ok {
				return
			}
			if ticker.SymbolID != symbolID {
				continue
			}

			writeCtx, cancel := context.WithTimeout(ctx, websocketWriteTimeout)
			err := conn.Write(writeCtx, websocket.MessageText, mustMarshalRealtimeMessage(ticker))
			cancel()
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					log.Printf("frontend websocket write failed: %v", err)
				}
				return
			}
		}
	}
}

const websocketWriteTimeout = 5 * time.Second

func mustMarshalRealtimeMessage(ticker entity.Ticker) []byte {
	payload, err := jsonMarshal(realtimeMessage{
		Type: "ticker",
		Data: ticker,
	})
	if err != nil {
		panic(err)
	}
	return payload
}

var jsonMarshal = func(v any) ([]byte, error) {
	return json.Marshal(v)
}
