package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
)

type TradeHandler struct {
	orderClient repository.OrderClient
	restClient  *rakuten.RESTClient
}

func NewTradeHandler(orderClient repository.OrderClient, restClient *rakuten.RESTClient) *TradeHandler {
	return &TradeHandler{orderClient: orderClient, restClient: restClient}
}

func (h *TradeHandler) GetTrades(c *gin.Context) {
	symbolStr := c.DefaultQuery("symbolId", "7")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbol ID"})
		return
	}

	trades, err := h.orderClient.GetMyTrades(c.Request.Context(), symbolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, trades)
}

// allTradesEntry はシンボル単位の約定取得結果。
// 部分失敗を許容するため、取得成功と失敗を 1 シンボル単位で返す。
type allTradesEntry struct {
	SymbolID     int64             `json:"symbolId"`
	CurrencyPair string            `json:"currencyPair"`
	Trades       []entity.MyTrade  `json:"trades,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// GetAllTrades は楽天の取引可能な全シンボルについて約定履歴をまとめて返す。
// 楽天 API 側に bulk エンドポイントが無いため内部でシンボルごとに直列ループするが、
// RESTClient 側の 220ms スロットラーが直列化を保証するため code 20010 は構造的に発生しない。
func (h *TradeHandler) GetAllTrades(c *gin.Context) {
	if h.restClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rest client not configured"})
		return
	}

	ctx := c.Request.Context()
	symbols, err := h.restClient.GetSymbols(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	results := make([]allTradesEntry, 0, len(symbols))
	for _, sym := range symbols {
		entry := allTradesEntry{SymbolID: sym.ID, CurrencyPair: sym.CurrencyPair}
		trades, err := h.orderClient.GetMyTrades(ctx, sym.ID)
		if err != nil {
			entry.Error = err.Error()
		} else {
			entry.Trades = trades
		}
		results = append(results, entry)
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}
