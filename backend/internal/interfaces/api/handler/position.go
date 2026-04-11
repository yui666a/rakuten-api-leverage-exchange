package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type PositionHandler struct {
	orderClient     repository.OrderClient
	orderExecutor   *usecase.OrderExecutor
	clientOrderRepo repository.ClientOrderRepository
}

func NewPositionHandler(
	orderClient repository.OrderClient,
	orderExecutor *usecase.OrderExecutor,
	clientOrderRepo repository.ClientOrderRepository,
) *PositionHandler {
	return &PositionHandler{
		orderClient:     orderClient,
		orderExecutor:   orderExecutor,
		clientOrderRepo: clientOrderRepo,
	}
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

type closePositionRequest struct {
	SymbolID      int64  `json:"symbolId"`
	ClientOrderID string `json:"clientOrderId"`
}

// ClosePosition handles POST /api/v1/positions/:id/close.
// Closes the specified position by its full remaining amount via market order.
func (h *PositionHandler) ClosePosition(c *gin.Context) {
	if h.orderExecutor == nil || h.clientOrderRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "close position is not configured"})
		return
	}

	idStr := c.Param("id")
	positionID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || positionID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid position id"})
		return
	}

	var req closePositionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.SymbolID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbolId is required"})
		return
	}
	if req.ClientOrderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "clientOrderId is required"})
		return
	}

	// Idempotency: a previous close with the same clientOrderId returns the original result.
	if existing, lookupErr := h.clientOrderRepo.Find(c.Request.Context(), req.ClientOrderID); lookupErr == nil && existing != nil {
		c.JSON(http.StatusOK, gin.H{
			"duplicate":     true,
			"clientOrderId": existing.ClientOrderID,
			"executed":      existing.Executed,
			"orderId":       existing.OrderID,
		})
		return
	}

	// Find the target position by symbolId + positionId.
	positions, err := h.orderClient.GetPositions(c.Request.Context(), req.SymbolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var pos *entity.Position
	for i := range positions {
		if positions[i].ID == positionID {
			pos = &positions[i]
			break
		}
	}
	if pos == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "position not found"})
		return
	}

	result, err := h.orderExecutor.ClosePosition(c.Request.Context(), req.ClientOrderID, *pos, pos.Price)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	record := repository.ClientOrderRecord{
		ClientOrderID: req.ClientOrderID,
		Executed:      result.Executed,
		OrderID:       result.OrderID,
		CreatedAt:     time.Now().Unix(),
	}
	if saveErr := h.clientOrderRepo.Save(c.Request.Context(), record); saveErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save client order record"})
		return
	}

	slog.Info("position close requested",
		"event", "position_close",
		"positionID", positionID,
		"symbolID", req.SymbolID,
		"clientOrderID", req.ClientOrderID,
		"executed", result.Executed,
		"orderID", result.OrderID,
	)

	c.JSON(http.StatusOK, gin.H{
		"clientOrderId": req.ClientOrderID,
		"executed":      result.Executed,
		"orderId":       result.OrderID,
		"reason":        result.Reason,
	})
}
