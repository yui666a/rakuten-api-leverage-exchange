package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type OrderHandler struct {
	orderExecutor   *usecase.OrderExecutor
	clientOrderRepo repository.ClientOrderRepository
}

func NewOrderHandler(orderExecutor *usecase.OrderExecutor, clientOrderRepo repository.ClientOrderRepository) *OrderHandler {
	return &OrderHandler{
		orderExecutor:   orderExecutor,
		clientOrderRepo: clientOrderRepo,
	}
}

type createOrderRequest struct {
	SymbolID      int64   `json:"symbolId"`
	Side          string  `json:"side"`
	Amount        float64 `json:"amount"`
	OrderType     string  `json:"orderType"`
	ClientOrderID string  `json:"clientOrderId"`
}

// CreateOrder handles POST /api/v1/orders.
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Validate clientOrderId
	if req.ClientOrderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "clientOrderId is required"})
		return
	}

	// Validate orderType
	if req.OrderType != "MARKET" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "orderType must be MARKET"})
		return
	}

	// Validate side
	if req.Side != string(entity.OrderSideBuy) && req.Side != string(entity.OrderSideSell) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "side must be BUY or SELL"})
		return
	}

	// Validate amount
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be greater than 0"})
		return
	}

	// Idempotency check
	existing, err := h.clientOrderRepo.Find(c.Request.Context(), req.ClientOrderID)
	if err == nil && existing != nil {
		c.JSON(http.StatusOK, gin.H{
			"duplicate":     true,
			"clientOrderId": existing.ClientOrderID,
			"executed":      existing.Executed,
			"orderId":       existing.OrderID,
		})
		return
	}

	// Build signal
	action := entity.SignalActionBuy
	if req.Side == string(entity.OrderSideSell) {
		action = entity.SignalActionSell
	}

	signal := entity.Signal{
		SymbolID:  req.SymbolID,
		Action:    action,
		Reason:    "REST API order",
		Timestamp: time.Now().Unix(),
	}

	result, err := h.orderExecutor.ExecuteSignal(c.Request.Context(), req.ClientOrderID, signal, 0, req.Amount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Save to client order repository
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

	c.JSON(http.StatusOK, gin.H{
		"clientOrderId": req.ClientOrderID,
		"executed":      result.Executed,
		"orderId":       result.OrderID,
		"reason":        result.Reason,
	})
}
