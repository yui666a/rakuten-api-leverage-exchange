package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
)

func newDecisionHandlerForTest(t *testing.T) (*DecisionHandler, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	repo := database.NewDecisionLogRepository(db)
	cleanup := func() { db.Close() }
	return NewDecisionHandler(repo), cleanup
}

func seedDecision(t *testing.T, repo repository.DecisionLogRepository, ts int64, action string) {
	t.Helper()
	rec := entity.DecisionRecord{
		BarCloseAt:      ts,
		TriggerKind:     entity.DecisionTriggerBarClose,
		SymbolID:        7,
		CurrencyPair:    "LTC_JPY",
		PrimaryInterval: "PT15M",
		Stance:          "TREND_FOLLOW",
		LastPrice:       30210,
		SignalAction:    action,
		RiskOutcome:     entity.DecisionRiskApproved,
		BookGateOutcome: entity.DecisionBookAllowed,
		OrderOutcome:    entity.DecisionOrderFilled,
		IndicatorsJSON:  `{"rsi":48.2}`,
		CreatedAt:       time.Now().UnixMilli(),
	}
	if err := repo.Insert(context.Background(), rec); err != nil {
		t.Fatalf("seed Insert: %v", err)
	}
}

func TestDecisionHandler_List_ReturnsRowsNewestFirst(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, cleanup := newDecisionHandlerForTest(t)
	defer cleanup()

	repo := h.repoForTest()
	seedDecision(t, repo, 1_000, "BUY")
	seedDecision(t, repo, 2_000, "HOLD")

	r := gin.New()
	r.GET("/decisions", h.List)
	req := httptest.NewRequest(http.MethodGet, "/decisions?symbolId=7&limit=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Decisions []map[string]any `json:"decisions"`
		HasMore   bool             `json:"hasMore"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Decisions) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Decisions))
	}
	first := resp.Decisions[0]
	signal := first["signal"].(map[string]any)
	if got := signal["action"].(string); got != "HOLD" {
		t.Errorf("first row signal.action = %q, want HOLD (newest)", got)
	}
}

func TestDecisionHandler_List_RejectsBadSymbolID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, cleanup := newDecisionHandlerForTest(t)
	defer cleanup()

	r := gin.New()
	r.GET("/decisions", h.List)
	req := httptest.NewRequest(http.MethodGet, "/decisions?symbolId=not-a-number", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "symbolId") {
		t.Errorf("body should mention symbolId; got %s", w.Body.String())
	}
}

func TestDecisionHandler_List_PreservesIndicatorsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, cleanup := newDecisionHandlerForTest(t)
	defer cleanup()

	seedDecision(t, h.repoForTest(), 1_000, "BUY")

	r := gin.New()
	r.GET("/decisions", h.List)
	req := httptest.NewRequest(http.MethodGet, "/decisions?symbolId=7", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"rsi":48.2`) {
		t.Errorf("indicators_json must be passed through verbatim; body = %s", w.Body.String())
	}
}
