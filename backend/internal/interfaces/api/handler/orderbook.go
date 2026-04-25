package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type OrderbookHandler struct {
	restClient    *rakuten.RESTClient
	marketDataSvc *usecase.MarketDataService
}

func NewOrderbookHandler(restClient *rakuten.RESTClient, marketDataSvc *usecase.MarketDataService) *OrderbookHandler {
	return &OrderbookHandler{restClient: restClient, marketDataSvc: marketDataSvc}
}

// GetOrderbook handles GET /api/v1/orderbook.
func (h *OrderbookHandler) GetOrderbook(c *gin.Context) {
	symbolStr := c.DefaultQuery("symbolId", "7")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbolId"})
		return
	}

	ob, err := h.restClient.GetOrderbook(c.Request.Context(), symbolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if ob == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no orderbook data available"})
		return
	}

	c.JSON(http.StatusOK, ob)
}

// GetOrderbookHistory handles GET /api/v1/orderbook/history.
// Query: symbolId (required), from/to (unix-millis, optional), limit (default 1000).
// Returns persisted orderbook snapshots in ascending timestamp order.
func (h *OrderbookHandler) GetOrderbookHistory(c *gin.Context) {
	if h.marketDataSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "market data service unavailable"})
		return
	}
	symbolStr := c.DefaultQuery("symbolId", "7")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbolId"})
		return
	}
	from, _ := strconv.ParseInt(c.DefaultQuery("from", "0"), 10, 64)
	to, _ := strconv.ParseInt(c.DefaultQuery("to", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "1000"))
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}

	rows, err := h.marketDataSvc.GetOrderbookHistory(c.Request.Context(), symbolID, from, to, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"symbolId":  symbolID,
		"count":     len(rows),
		"snapshots": rows,
	})
}
