package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/domain"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/usecase"
)

// OrderController handles order-related HTTP requests
type OrderController struct {
	orderUsecase *usecase.OrderUsecase
}

// NewOrderController creates a new OrderController instance
func NewOrderController(orderUsecase *usecase.OrderUsecase) *OrderController {
	return &OrderController{
		orderUsecase: orderUsecase,
	}
}

// CreateOrder handles POST /api/orders
func (c *OrderController) CreateOrder(ctx *gin.Context) {
	var order domain.Order
	
	if err := ctx.ShouldBindJSON(&order); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body",
		})
		return
	}

	if err := c.orderUsecase.CreateOrder(ctx.Request.Context(), &order); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusCreated, order)
}

// GetOrder handles GET /api/orders/:id
func (c *OrderController) GetOrder(ctx *gin.Context) {
	id := ctx.Param("id")

	order, err := c.orderUsecase.GetOrder(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, order)
}

// GetOrders handles GET /api/orders
func (c *OrderController) GetOrders(ctx *gin.Context) {
	orders, err := c.orderUsecase.GetOrders(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, orders)
}

// CancelOrder handles DELETE /api/orders/:id
func (c *OrderController) CancelOrder(ctx *gin.Context) {
	id := ctx.Param("id")

	if err := c.orderUsecase.CancelOrder(ctx.Request.Context(), id); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message": "Order cancelled successfully",
	})
}
