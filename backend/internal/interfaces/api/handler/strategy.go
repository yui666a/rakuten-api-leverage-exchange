package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// LiveMarketSnapshot is the narrow port StrategyHandler uses to compute the
// stance from the *current* market state. A nil snapshot preserves the legacy
// behaviour of resolving with an empty IndicatorSet (rule-based → HOLD with
// "insufficient indicator data"), which is what unit tests for the resolver
// assume.
type LiveMarketSnapshot interface {
	// Snapshot returns the latest indicator set and last traded price for the
	// currently active trading symbol. Returning an empty IndicatorSet or
	// price 0 is allowed and is treated the same as "warmup" — the resolver
	// will fall back to HOLD.
	Snapshot(ctx context.Context) (entity.IndicatorSet, float64)
}

type StrategyHandler struct {
	stanceResolver *usecase.RuleBasedStanceResolver
	// snapshot is optional; when nil GetStrategy falls back to the legacy
	// empty-IndicatorSet path. Wired by the router at startup from the live
	// IndicatorCalculator + MarketDataService + Pipeline.
	snapshot LiveMarketSnapshot
}

func NewStrategyHandler(stanceResolver *usecase.RuleBasedStanceResolver) *StrategyHandler {
	return &StrategyHandler{stanceResolver: stanceResolver}
}

// WithLiveSnapshot returns a StrategyHandler that computes the stance from the
// live market snapshot on every GET /strategy call. Kept as a functional
// option so existing tests (which construct the handler with only a resolver)
// keep compiling.
func (h *StrategyHandler) WithLiveSnapshot(snap LiveMarketSnapshot) *StrategyHandler {
	h.snapshot = snap
	return h
}

func (h *StrategyHandler) GetStrategy(c *gin.Context) {
	indicators := entity.IndicatorSet{}
	var lastPrice float64
	if h.snapshot != nil {
		ind, price := h.snapshot.Snapshot(c.Request.Context())
		indicators = ind
		lastPrice = price
		if price == 0 {
			slog.Debug("strategy: live snapshot has zero price; resolver will fall back to HOLD")
		}
	}
	result := h.stanceResolver.Resolve(c.Request.Context(), indicators, lastPrice)
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
	if stance != entity.MarketStanceTrendFollow && stance != entity.MarketStanceContrarian && stance != entity.MarketStanceHold && stance != entity.MarketStanceBreakout {
		c.JSON(http.StatusBadRequest, gin.H{"error": "stance must be TREND_FOLLOW, CONTRARIAN, HOLD, or BREAKOUT"})
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
