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

	var before int64
	if beforeStr := c.Query("before"); beforeStr != "" {
		before, _ = strconv.ParseInt(beforeStr, 10, 64)
	}

	ctx := c.Request.Context()
	candles, err := h.marketDataSvc.GetCandles(ctx, symbolID, interval, limit, before)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// DB のデータが不足していれば楽天APIからオンデマンド取得して補充する。
	// - before なし: DB が空なら最新データを取得
	// - before あり: DB が limit 未満なら dateTo=before で過去データを取得
	needFetch := h.restClient != nil &&
		(len(candles) == 0 || (before > 0 && len(candles) < limit))
	if needFetch {
		var dateFrom, dateTo *int64
		if before > 0 {
			dateTo = &before
		}
		resp, fetchErr := h.restClient.GetCandlestick(ctx, symbolID, interval, dateFrom, dateTo)
		if fetchErr != nil {
			// フェッチ失敗でも DB のデータがあればそれを返す
			if len(candles) > 0 {
				goto respond
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fetchErr.Error()})
			return
		}
		if len(resp.Candlesticks) > 0 {
			if saveErr := h.marketDataSvc.SaveCandles(ctx, symbolID, interval, resp.Candlesticks); saveErr != nil {
				if len(candles) > 0 {
					goto respond
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": saveErr.Error()})
				return
			}
			candles, err = h.marketDataSvc.GetCandles(ctx, symbolID, interval, limit, before)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}

respond:

	// Lightweight Charts expects oldest -> newest ordering.
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	c.JSON(http.StatusOK, candles)
}
