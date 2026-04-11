package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
)

// TradingConfigHandler は取引設定の取得・切替を行う。
// switchSymbol は router 側で pipeline.SwitchSymbol + onSwitch をクロージャで包んだもの。
// handler は onSwitch を知らず、「切替する」ことだけが責務。
type TradingConfigHandler struct {
	getSymbolID    func() int64
	getTradeAmount func() float64
	switchSymbol   func(symbolID int64, tradeAmount float64)
	restClient     *rakuten.RESTClient
}

func NewTradingConfigHandler(
	getSymbolID func() int64,
	getTradeAmount func() float64,
	switchSymbol func(symbolID int64, tradeAmount float64),
	restClient *rakuten.RESTClient,
) *TradingConfigHandler {
	return &TradingConfigHandler{
		getSymbolID:    getSymbolID,
		getTradeAmount: getTradeAmount,
		switchSymbol:   switchSymbol,
		restClient:     restClient,
	}
}

type tradingConfigResponse struct {
	SymbolID    int64   `json:"symbolId"`
	TradeAmount float64 `json:"tradeAmount"`
}

type updateTradingConfigRequest struct {
	SymbolID    int64   `json:"symbolId"`
	TradeAmount float64 `json:"tradeAmount"`
}

// GetTradingConfig handles GET /api/v1/trading-config.
func (h *TradingConfigHandler) GetTradingConfig(c *gin.Context) {
	c.JSON(http.StatusOK, tradingConfigResponse{
		SymbolID:    h.getSymbolID(),
		TradeAmount: h.getTradeAmount(),
	})
}

// UpdateTradingConfig handles PUT /api/v1/trading-config.
// interval はランタイム変更をサポートしない（リクエストで受け付けない）。
func (h *TradingConfigHandler) UpdateTradingConfig(c *gin.Context) {
	var req updateTradingConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.SymbolID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbolId must be positive"})
		return
	}

	if req.TradeAmount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tradeAmount must be positive"})
		return
	}

	// シンボルの存在確認と取引可否の検証
	symbols, err := h.restClient.GetSymbols(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch symbols"})
		return
	}

	var found bool
	for _, s := range symbols {
		if s.ID != req.SymbolID {
			continue
		}
		if !s.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is disabled"})
			return
		}
		if s.ViewOnly {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is view-only"})
			return
		}
		if s.CloseOnly {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is close-only"})
			return
		}
		found = true
		break
	}
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown symbolId"})
		return
	}

	h.switchSymbol(req.SymbolID, req.TradeAmount)

	c.JSON(http.StatusOK, tradingConfigResponse{
		SymbolID:    h.getSymbolID(),
		TradeAmount: h.getTradeAmount(),
	})
}
