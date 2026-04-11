package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
)

type SymbolHandler struct {
	restClient *rakuten.RESTClient
}

func NewSymbolHandler(restClient *rakuten.RESTClient) *SymbolHandler {
	return &SymbolHandler{restClient: restClient}
}

// GetSymbols handles GET /api/v1/symbols.
func (h *SymbolHandler) GetSymbols(c *gin.Context) {
	symbols, err := h.restClient.GetSymbols(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, symbols)
}
