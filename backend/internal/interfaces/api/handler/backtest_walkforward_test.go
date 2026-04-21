package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

// The handler validations below do NOT require a real backtest run — they
// should reject the request before ExpandGrid/ComputeWindows/runner are
// reached. A minimal BacktestHandler is enough.

func newWalkForwardHandler(t *testing.T) *BacktestHandler {
	t.Helper()
	return NewBacktestHandler(
		bt.NewBacktestRunner(),
		&mockBacktestResultRepo{},
	)
}

// TestWalkForward_RejectsInvalidObjective locks in the Codex PR-13
// follow-up: a typo in the objective name must surface as 400, not
// silently fall back to TotalReturn inside SelectByObjective.
func TestWalkForward_RejectsInvalidObjective(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newWalkForwardHandler(t)
	r := gin.New()
	r.POST("/backtest/walk-forward", h.RunWalkForward)

	body := `{
		"data": "x.csv",
		"from": "2024-01-01",
		"to":   "2024-12-01",
		"baseProfile": "production",
		"objective":   "returns"
	}`
	req := httptest.NewRequest(http.MethodPost, "/backtest/walk-forward", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid objective") {
		t.Fatalf("body should mention invalid objective, got: %s", w.Body.String())
	}
}

// TestWalkForward_RejectsUnknownOverridePath locks in the follow-up: an
// unknown override path must be a 400 (caller error), not a 500 from
// ApplyOverrides failing mid-run.
func TestWalkForward_RejectsUnknownOverridePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newWalkForwardHandler(t)
	r := gin.New()
	r.POST("/backtest/walk-forward", h.RunWalkForward)

	body := `{
		"data": "x.csv",
		"from": "2024-01-01",
		"to":   "2024-12-01",
		"baseProfile": "production",
		"objective":   "return",
		"parameterGrid": [{"path": "not.a.real.path", "values": [1, 2]}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/backtest/walk-forward", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unsupported override path") {
		t.Fatalf("body should mention unsupported path, got: %s", w.Body.String())
	}
}
