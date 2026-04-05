package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

type PositionHandler struct {
	orderClient repository.OrderClient
}

func NewPositionHandler(orderClient repository.OrderClient) *PositionHandler {
	return &PositionHandler{orderClient: orderClient}
}

// GetPositions は指定銘柄のポジション一覧を返す。
func (h *PositionHandler) GetPositions(c *gin.Context) {
	symbolStr := c.DefaultQuery("symbolId", "7")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbol ID"})
		return
	}

	positions, err := h.orderClient.GetPositions(c.Request.Context(), symbolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, positions)
}
