package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/usecase"
)

// MarketController handles market-related HTTP requests
type MarketController struct {
	marketUsecase *usecase.MarketUsecase
}

// NewMarketController creates a new MarketController instance
func NewMarketController(marketUsecase *usecase.MarketUsecase) *MarketController {
	return &MarketController{
		marketUsecase: marketUsecase,
	}
}

// GetMarket handles GET /api/markets/:symbol
func (c *MarketController) GetMarket(ctx *gin.Context) {
	symbol := ctx.Param("symbol")

	market, err := c.marketUsecase.GetMarket(ctx.Request.Context(), symbol)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, market)
}

// GetAllMarkets handles GET /api/markets
func (c *MarketController) GetAllMarkets(ctx *gin.Context) {
	markets, err := c.marketUsecase.GetAllMarkets(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, markets)
}
