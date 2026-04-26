package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

func newBacktestHandlerForDecisionTest(t *testing.T) (*BacktestHandler, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	resultRepo := struct {
		// We don't exercise the result-save path here, only the decision
		// log endpoints, so we pass nil for the result repo. The handler
		// nil-guards on result-related code paths and the decision
		// endpoints don't touch h.repo at all.
	}{}
	_ = resultRepo
	decisionRepo := database.NewBacktestDecisionLogRepository(db)
	handler := NewBacktestHandler(
		bt.NewBacktestRunner(),
		nil,
		WithDecisionLogRepo(decisionRepo),
	)
	cleanup := func() { db.Close() }
	return handler, cleanup
}

func seedBacktestDecision(t *testing.T, h *BacktestHandler, runID string, ts int64) {
	t.Helper()
	rec := entity.DecisionRecord{
		BarCloseAt:      ts,
		TriggerKind:     entity.DecisionTriggerBarClose,
		SymbolID:        7,
		CurrencyPair:    "LTC_JPY",
		PrimaryInterval: "PT15M",
		Stance:          "TREND_FOLLOW",
		LastPrice:       30210,
		SignalAction:    "BUY",
		RiskOutcome:     entity.DecisionRiskApproved,
		BookGateOutcome: entity.DecisionBookAllowed,
		OrderOutcome:    entity.DecisionOrderFilled,
		IndicatorsJSON:  `{"rsi":48.2}`,
		CreatedAt:       time.Now().UnixMilli(),
	}
	if err := h.decisionLogRepo.Insert(context.Background(), rec, runID); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
}

func TestBacktestHandler_ListDecisions_FiltersByRunID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, cleanup := newBacktestHandlerForDecisionTest(t)
	defer cleanup()

	seedBacktestDecision(t, h, "run-aaa", 1_000)
	seedBacktestDecision(t, h, "run-aaa", 2_000)
	seedBacktestDecision(t, h, "run-bbb", 1_500)

	r := gin.New()
	r.GET("/backtest/results/:id/decisions", h.ListDecisions)
	req := httptest.NewRequest(http.MethodGet, "/backtest/results/run-aaa/decisions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Decisions []map[string]any `json:"decisions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Decisions) != 2 {
		t.Errorf("len = %d, want 2 (run-aaa only)", len(resp.Decisions))
	}
}

func TestBacktestHandler_DeleteDecisions_RemovesOnlyTargetRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, cleanup := newBacktestHandlerForDecisionTest(t)
	defer cleanup()

	seedBacktestDecision(t, h, "run-aaa", 1_000)
	seedBacktestDecision(t, h, "run-aaa", 2_000)
	seedBacktestDecision(t, h, "run-bbb", 1_500)

	r := gin.New()
	r.DELETE("/backtest/results/:id/decisions", h.DeleteDecisions)
	r.GET("/backtest/results/:id/decisions", h.ListDecisions)

	req := httptest.NewRequest(http.MethodDelete, "/backtest/results/run-aaa/decisions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", w.Code, w.Body.String())
	}
	var del struct {
		Deleted int `json:"deleted"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &del)
	if del.Deleted != 2 {
		t.Errorf("deleted = %d, want 2", del.Deleted)
	}

	// run-bbb must remain.
	req = httptest.NewRequest(http.MethodGet, "/backtest/results/run-bbb/decisions", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var listResp struct {
		Decisions []map[string]any `json:"decisions"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if len(listResp.Decisions) != 1 {
		t.Errorf("run-bbb rows after delete = %d, want 1 (untouched)", len(listResp.Decisions))
	}
}

func TestBacktestHandler_ListDecisions_Returns503WhenRepoMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewBacktestHandler(bt.NewBacktestRunner(), nil)
	r := gin.New()
	r.GET("/backtest/results/:id/decisions", handler.ListDecisions)
	req := httptest.NewRequest(http.MethodGet, "/backtest/results/run-xyz/decisions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
