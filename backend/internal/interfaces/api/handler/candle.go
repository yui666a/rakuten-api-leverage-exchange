package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// allowedIntervals は楽天APIの candlestickType をそのまま interval として許容する集合。
var allowedIntervals = map[string]struct{}{
	"PT1M":  {},
	"PT5M":  {},
	"PT15M": {},
	"PT1H":  {},
	"P1D":   {},
	"P1W":   {},
}

type CandleHandler struct {
	marketDataSvc *usecase.MarketDataService
	restClient    *rakuten.RESTClient
}

func NewCandleHandler(marketDataSvc *usecase.MarketDataService, restClient *rakuten.RESTClient) *CandleHandler {
	return &CandleHandler{marketDataSvc: marketDataSvc, restClient: restClient}
}

// GetCandles は銘柄のローソク足データを返す。
// DB にデータが無ければ楽天APIからオンデマンド取得して保存し、改めて返す。
func (h *CandleHandler) GetCandles(c *gin.Context) {
	symbolStr := c.Param("symbol")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbol ID"})
		return
	}

	interval := c.DefaultQuery("interval", "PT15M")
	if _, ok := allowedIntervals[interval]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported interval: " + interval})
		return
	}

	limitStr := c.DefaultQuery("limit", "500")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 500 {
		limit = 500
	}

	ctx := c.Request.Context()
	candles, err := h.marketDataSvc.GetCandles(ctx, symbolID, interval, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// DB にデータが無ければ楽天APIから取得して保存、再取得する。
	if len(candles) == 0 && h.restClient != nil {
		resp, fetchErr := h.restClient.GetCandlestick(ctx, symbolID, interval, nil, nil)
		if fetchErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fetchErr.Error()})
			return
		}
		if len(resp.Candlesticks) > 0 {
			if saveErr := h.marketDataSvc.SaveCandles(ctx, symbolID, interval, resp.Candlesticks); saveErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": saveErr.Error()})
				return
			}
			candles, err = h.marketDataSvc.GetCandles(ctx, symbolID, interval, limit)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}

	// Lightweight Charts expects oldest -> newest ordering.
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	c.JSON(http.StatusOK, candles)
}
