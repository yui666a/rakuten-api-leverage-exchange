package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

const (
	decisionAPIDefaultLimit = 200
	decisionAPIMaxLimit     = 1000
)

type DecisionHandler struct {
	repo repository.DecisionLogRepository
}

func NewDecisionHandler(repo repository.DecisionLogRepository) *DecisionHandler {
	return &DecisionHandler{repo: repo}
}

// repoForTest exposes the underlying repo to in-package tests so they can
// seed rows without a separate fixture path. Tests only.
func (h *DecisionHandler) repoForTest() repository.DecisionLogRepository { return h.repo }

func (h *DecisionHandler) List(c *gin.Context) {
	f, err := parseDecisionFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rows, next, err := h.repo.List(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, decisionRecordToJSON(r))
	}
	c.JSON(http.StatusOK, gin.H{
		"decisions":  out,
		"nextCursor": next,
		"hasMore":    next != 0,
	})
}

func parseDecisionFilter(c *gin.Context) (repository.DecisionLogFilter, error) {
	var f repository.DecisionLogFilter

	if s := c.Query("symbolId"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return f, fmt.Errorf("invalid symbolId: %w", err)
		}
		f.SymbolID = v
	}
	if s := c.Query("from"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return f, fmt.Errorf("invalid from: %w", err)
		}
		f.From = v
	}
	if s := c.Query("to"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return f, fmt.Errorf("invalid to: %w", err)
		}
		f.To = v
	}
	if s := c.Query("cursor"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return f, fmt.Errorf("invalid cursor: %w", err)
		}
		f.Cursor = v
	}
	f.Limit = decisionAPIDefaultLimit
	if s := c.Query("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			return f, fmt.Errorf("invalid limit: %w", err)
		}
		if v > decisionAPIMaxLimit {
			v = decisionAPIMaxLimit
		}
		if v > 0 {
			f.Limit = v
		}
	}
	return f, nil
}

// decisionRecordToJSON converts the domain record into the API payload
// shape. indicators_json / higher_tf_indicators_json are forwarded as
// json.RawMessage so the recorder's marshalled IndicatorSet is preserved
// without a re-serialization round trip.
func decisionRecordToJSON(r entity.DecisionRecord) gin.H {
	indicators := json.RawMessage(r.IndicatorsJSON)
	if len(indicators) == 0 {
		indicators = json.RawMessage("{}")
	}
	higher := json.RawMessage(r.HigherTFIndicatorsJSON)
	if len(higher) == 0 {
		higher = json.RawMessage("{}")
	}
	return gin.H{
		"id":              r.ID,
		"barCloseAt":      r.BarCloseAt,
		"sequenceInBar":   r.SequenceInBar,
		"triggerKind":     r.TriggerKind,
		"symbolId":        r.SymbolID,
		"currencyPair":    r.CurrencyPair,
		"primaryInterval": r.PrimaryInterval,
		"stance":          r.Stance,
		"lastPrice":       r.LastPrice,
		"signal": gin.H{
			"action":     r.SignalAction,
			"confidence": r.SignalConfidence,
			"reason":     r.SignalReason,
		},
		// Phase 1 PR5 (Signal/Decision/ExecutionPolicy): expose the new shadow
		// columns alongside the legacy `signal` block. The frontend renders
		// both — `signal` carries the legacy BUY/SELL/HOLD label, while
		// `marketSignal.direction` and `decision.intent` reveal the Phase 1
		// classification (e.g. EXIT_CANDIDATE on a bearish-against-long bar).
		// Empty strings / 0 indicate pre-PR2 rows; the frontend handles them
		// as "—" so old runs still render without crashing.
		"marketSignal": gin.H{
			"direction": r.SignalDirection,
			"strength":  r.SignalStrength,
		},
		"decision": gin.H{
			"intent": r.DecisionIntent,
			"side":   r.DecisionSide,
			"reason": r.DecisionReason,
		},
		"exitPolicyOutcome":  r.ExitPolicyOutcome,
		"risk":               gin.H{"outcome": r.RiskOutcome, "reason": r.RiskReason},
		"bookGate":           gin.H{"outcome": r.BookGateOutcome, "reason": r.BookGateReason},
		"order": gin.H{
			"outcome": r.OrderOutcome,
			"orderId": r.OrderID,
			"amount":  r.ExecutedAmount,
			"price":   r.ExecutedPrice,
			"error":   r.OrderError,
		},
		"closedPositionId":   r.ClosedPositionID,
		"openedPositionId":   r.OpenedPositionID,
		"indicators":         indicators,
		"higherTfIndicators": higher,
		"createdAt":          r.CreatedAt,
	}
}
