package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type IndicatorHandler struct {
	calculator *usecase.IndicatorCalculator
}

func NewIndicatorHandler(calculator *usecase.IndicatorCalculator) *IndicatorHandler {
	return &IndicatorHandler{calculator: calculator}
}

func (h *IndicatorHandler) GetIndicators(c *gin.Context) {
	symbolStr := c.Param("symbol")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbol ID"})
		return
	}

	interval := c.DefaultQuery("interval", "PT15M")

	indicators, err := h.calculator.Calculate(c.Request.Context(), symbolID, interval)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, indicators)
}
