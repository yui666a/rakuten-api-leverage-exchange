package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type StrategyHandler struct {
	stanceResolver *usecase.RuleBasedStanceResolver
}

func NewStrategyHandler(stanceResolver *usecase.RuleBasedStanceResolver) *StrategyHandler {
	return &StrategyHandler{stanceResolver: stanceResolver}
}

func (h *StrategyHandler) GetStrategy(c *gin.Context) {
	indicators := entity.IndicatorSet{}
	result := h.stanceResolver.Resolve(c.Request.Context(), indicators, 0)
	c.JSON(http.StatusOK, result)
}

type setStrategyRequest struct {
	Stance     string `json:"stance" binding:"required"`
	Reasoning  string `json:"reasoning"`
	TTLMinutes int    `json:"ttlMinutes"`
}

func (h *StrategyHandler) SetStrategy(c *gin.Context) {
	var req setStrategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stance := entity.MarketStance(req.Stance)
	if stance != entity.MarketStanceTrendFollow && stance != entity.MarketStanceContrarian && stance != entity.MarketStanceHold {
		c.JSON(http.StatusBadRequest, gin.H{"error": "stance must be TREND_FOLLOW, CONTRARIAN, or HOLD"})
		return
	}

	ttl := req.TTLMinutes
	if ttl <= 0 {
		ttl = 60
	}
	if ttl > 1440 {
		ttl = 1440
	}

	ttlDuration := time.Duration(ttl) * time.Minute
	h.stanceResolver.SetOverride(stance, req.Reasoning, ttlDuration)

	expiresAt := time.Now().Add(ttlDuration)
	c.JSON(http.StatusOK, gin.H{
		"stance":    stance,
		"reasoning": req.Reasoning,
		"source":    "override",
		"expiresAt": expiresAt.Format(time.RFC3339),
	})
}

func (h *StrategyHandler) DeleteOverride(c *gin.Context) {
	h.stanceResolver.ClearOverride()
	c.JSON(http.StatusOK, gin.H{
		"message": "override cleared, using rule-based stance",
	})
}
