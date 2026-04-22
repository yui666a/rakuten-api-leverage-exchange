package handler

import (
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
)

// ProfileHandler exposes the on-disk strategy profile collection to the
// frontend so users can pick a preset and read / edit its fields before
// kicking off a backtest. The handler is read-only; writes (promotion,
// saving a variant) remain a CLI / git-tracked workflow.
type ProfileHandler struct {
	baseDir string
}

// NewProfileHandler returns a handler rooted at baseDir. Callers typically
// pass the same baseDir used by the BacktestHandler (see
// handler.defaultProfilesBaseDir) so profile picker and backtest run share
// the same view of the filesystem.
func NewProfileHandler(baseDir string) *ProfileHandler {
	if baseDir == "" {
		baseDir = defaultProfilesBaseDir
	}
	return &ProfileHandler{baseDir: baseDir}
}

// List handles GET /api/v1/profiles. Returns an array of
// strategyprofile.ProfileSummary sorted by name. A directory read error is
// mapped to 500; per-file load errors are surfaced *inside* the summary so
// a single bad profile does not hide the rest.
func (h *ProfileHandler) List(c *gin.Context) {
	loader := strategyprofile.NewLoader(h.baseDir)
	summaries, err := loader.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Return a non-nil empty slice so the FE can iterate unconditionally.
	if summaries == nil {
		summaries = []strategyprofile.ProfileSummary{}
	}
	c.JSON(http.StatusOK, gin.H{"profiles": summaries})
}

// Get handles GET /api/v1/profiles/:name. Returns the full StrategyProfile
// JSON; the FE uses this to populate the inline edit form.
//
// 400 on an invalid name shape (regex-rejected by ResolveProfilePath).
// 404 when the profile file is missing.
// 422 when the file is present but fails schema / Validate — same split as
//
//	the existing loader behaviour, so the FE can distinguish "typo'd name"
//	from "profile broken".
func (h *ProfileHandler) Get(c *gin.Context) {
	name := c.Param("name")
	loader := strategyprofile.NewLoader(h.baseDir)
	profile, err := loader.Load(name)
	if err != nil {
		switch {
		case errors.Is(err, strategyprofile.ErrInvalidProfileName):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case isNotExist(err):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			// Decode / validate failures. 422 conveys "your request is
			// understandable but the referenced resource is unprocessable".
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, profile)
}

// isNotExist distinguishes "file missing" (404) from decode / validate
// failures (422). errors.Is works on wrapped errors because
// strategyprofile.Load wraps via fmt.Errorf("%w", ...).
func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
