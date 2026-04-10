package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type TickerHandler struct {
	marketDataSvc *usecase.MarketDataService
}

func NewTickerHandler(marketDataSvc *usecase.MarketDataService) *TickerHandler {
	return &TickerHandler{marketDataSvc: marketDataSvc}
}

// GetTicker handles GET /api/v1/ticker.
func (h *TickerHandler) GetTicker(c *gin.Context) {
	symbolStr := c.DefaultQuery("symbolId", "7")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbolId"})
		return
	}

	ticker, err := h.marketDataSvc.GetLatestTicker(c.Request.Context(), symbolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if ticker == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no ticker data available"})
		return
	}

	c.JSON(http.StatusOK, ticker)
}
