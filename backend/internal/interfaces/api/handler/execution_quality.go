package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/quality"
)

// ExecutionQualityHandler exposes GET /api/v1/execution-quality.
type ExecutionQualityHandler struct {
	reporter *quality.Reporter
	// snapshotRepo, when set, lets the handler serve the most recent worker-
	// captured snapshot instead of hitting the venue. Falls through to the
	// reporter when the cache is empty / `?fresh=true` is supplied.
	snapshotRepo repository.ExecutionQualityRepository
	// defaultSymbolID is consulted when the caller omits ?symbolId. The
	// handler does not own the running pipeline so the composition root
	// passes a getter that always reflects the current symbol.
	defaultSymbol func() int64
}

func NewExecutionQualityHandler(reporter *quality.Reporter, defaultSymbol func() int64) *ExecutionQualityHandler {
	return &ExecutionQualityHandler{reporter: reporter, defaultSymbol: defaultSymbol}
}

// WithSnapshotRepo enables the snapshot-cache path on an existing handler.
func (h *ExecutionQualityHandler) WithSnapshotRepo(repo repository.ExecutionQualityRepository) *ExecutionQualityHandler {
	h.snapshotRepo = repo
	return h
}

// Get handles GET /api/v1/execution-quality?windowSec=86400&symbolId=7&fresh=false.
//
// Default behaviour: serve the most recent worker-captured snapshot. The
// snapshot worker runs every 60 s so the served data is at most that stale.
// fresh=true bypasses the cache and recomputes synchronously (still bounded
// by the venue's 200 ms rate limit; intended for one-off diagnostics).
func (h *ExecutionQualityHandler) Get(c *gin.Context) {
	if h.reporter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution quality reporter unavailable"})
		return
	}
	windowSec, _ := strconv.ParseInt(c.DefaultQuery("windowSec", "86400"), 10, 64)
	if windowSec <= 0 {
		windowSec = 86400
	}

	var symbolID int64
	if v := c.Query("symbolId"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbolId"})
			return
		}
		symbolID = id
	} else if h.defaultSymbol != nil {
		symbolID = h.defaultSymbol()
	}
	if symbolID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbolId required"})
		return
	}

	fresh := c.DefaultQuery("fresh", "false") == "true"

	if !fresh && h.snapshotRepo != nil {
		if cached, err := h.snapshotRepo.Latest(c.Request.Context(), symbolID); err == nil && cached != nil {
			c.JSON(http.StatusOK, cached)
			return
		}
		// Fall through to live recompute when the cache is empty or read failed.
	}

	report, err := h.reporter.Build(c.Request.Context(), symbolID, windowSec)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}
