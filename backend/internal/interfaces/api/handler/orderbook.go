package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
)

type OrderbookHandler struct {
	restClient *rakuten.RESTClient
}

func NewOrderbookHandler(restClient *rakuten.RESTClient) *OrderbookHandler {
	return &OrderbookHandler{restClient: restClient}
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
