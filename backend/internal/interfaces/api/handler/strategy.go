package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type StrategyHandler struct {
	llmService *usecase.LLMService
}

func NewStrategyHandler(llmService *usecase.LLMService) *StrategyHandler {
	return &StrategyHandler{llmService: llmService}
}

func (h *StrategyHandler) GetStrategy(c *gin.Context) {
	symbolID := int64(7) // デフォルト: BTC_JPY
	if q := c.Query("symbolId"); q != "" {
		if v, err := strconv.ParseInt(q, 10, 64); err == nil {
			symbolID = v
		}
	}

	advice := h.llmService.GetCachedAdvice(symbolID)
	if advice == nil {
		c.JSON(http.StatusOK, gin.H{
			"stance":    "NONE",
			"reasoning": "no strategy advice cached yet",
		})
		return
	}
	c.JSON(http.StatusOK, advice)
}
