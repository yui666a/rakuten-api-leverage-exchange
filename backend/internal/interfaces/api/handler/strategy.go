package handler

import (
	"net/http"

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
	advice := h.llmService.GetCachedAdvice(0)
	if advice == nil {
		c.JSON(http.StatusOK, gin.H{
			"stance":    "NONE",
			"reasoning": "no strategy advice cached yet",
		})
		return
	}
	c.JSON(http.StatusOK, advice)
}
