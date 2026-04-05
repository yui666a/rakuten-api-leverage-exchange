package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type CandleHandler struct {
	marketDataSvc *usecase.MarketDataService
}

func NewCandleHandler(marketDataSvc *usecase.MarketDataService) *CandleHandler {
	return &CandleHandler{marketDataSvc: marketDataSvc}
}

// GetCandles は銘柄のローソク足データを返す。
func (h *CandleHandler) GetCandles(c *gin.Context) {
	symbolStr := c.Param("symbol")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbol ID"})
		return
	}

	interval := c.DefaultQuery("interval", "15min")
	limitStr := c.DefaultQuery("limit", "500")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 500 {
		limit = 500
	}

	candles, err := h.marketDataSvc.GetCandles(c.Request.Context(), symbolID, interval, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, candles)
}
