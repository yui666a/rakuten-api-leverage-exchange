package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

// fakeWalkForwardRepo is an in-memory WalkForwardResultRepository for the
// handler-level tests. Real persistence is covered by the repo unit test;
// here we only need to assert the handler routes to the right methods and
// serialises request/response correctly.
type fakeWalkForwardRepo struct {
	mu   sync.Mutex
	rows map[string]entity.WalkForwardPersisted
	err  error
}

func newFakeWFRepo() *fakeWalkForwardRepo {
	return &fakeWalkForwardRepo{rows: make(map[string]entity.WalkForwardPersisted)}
}

func (r *fakeWalkForwardRepo) Save(_ context.Context, rec entity.WalkForwardPersisted) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.rows[rec.ID] = rec
	return nil
}

func (r *fakeWalkForwardRepo) List(_ context.Context, filter repository.WalkForwardResultFilter) ([]entity.WalkForwardPersisted, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []entity.WalkForwardPersisted
	for _, rec := range r.rows {
		if filter.BaseProfile != "" && rec.BaseProfile != filter.BaseProfile {
			continue
		}
		if filter.PDCACycleID != "" && rec.PDCACycleID != filter.PDCACycleID {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func (r *fakeWalkForwardRepo) FindByID(_ context.Context, id string) (*entity.WalkForwardPersisted, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.rows[id]; ok {
		return &rec, nil
	}
	return nil, nil
}

func TestGetWalkForward_NotFoundReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWFRepo()
	h := NewBacktestHandler(bt.NewBacktestRunner(), &mockBacktestResultRepo{}, WithWalkForwardRepo(repo))
	r := gin.New()
	r.GET("/backtest/walk-forward/:id", h.GetWalkForward)

	req := httptest.NewRequest(http.MethodGet, "/backtest/walk-forward/missing", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestGetWalkForward_NoRepoReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// No WithWalkForwardRepo -> wfRepo is nil.
	h := NewBacktestHandler(bt.NewBacktestRunner(), &mockBacktestResultRepo{})
	r := gin.New()
	r.GET("/backtest/walk-forward/:id", h.GetWalkForward)

	req := httptest.NewRequest(http.MethodGet, "/backtest/walk-forward/anything", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestGetWalkForward_ReturnsRowAsRawJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWFRepo()
	// Seed a row with non-trivial JSON blobs to make sure the handler
	// surfaces them as parsed objects (RawMessage), not double-escaped
	// strings.
	_ = repo.Save(context.Background(), entity.WalkForwardPersisted{
		ID:               "wf-42",
		CreatedAt:        1700000000,
		BaseProfile:      "production",
		Objective:        "return",
		PDCACycleID:      "cycle22",
		RequestJSON:      `{"baseProfile":"production"}`,
		ResultJSON:       `{"id":"wf-42","windows":[{"index":0}]}`,
		AggregateOOSJSON: `{"robustnessScore":0.5}`,
	})

	h := NewBacktestHandler(bt.NewBacktestRunner(), &mockBacktestResultRepo{}, WithWalkForwardRepo(repo))
	r := gin.New()
	r.GET("/backtest/walk-forward/:id", h.GetWalkForward)

	req := httptest.NewRequest(http.MethodGet, "/backtest/walk-forward/wf-42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var resp walkForwardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ID != "wf-42" {
		t.Fatalf("wrong id: %q", resp.ID)
	}
	// Result should round-trip as a JSON object with nested windows.
	var parsedResult struct {
		ID      string `json:"id"`
		Windows []struct {
			Index int `json:"index"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(resp.Result, &parsedResult); err != nil {
		t.Fatalf("parse result json: %v", err)
	}
	if parsedResult.ID != "wf-42" || len(parsedResult.Windows) != 1 {
		t.Fatalf("result json not structured: %+v", parsedResult)
	}
}

func TestListWalkForward_AppliesFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWFRepo()
	_ = repo.Save(context.Background(), entity.WalkForwardPersisted{
		ID: "a", BaseProfile: "production", PDCACycleID: "cycle01",
		RequestJSON: "{}", ResultJSON: "{}", AggregateOOSJSON: "{}",
	})
	_ = repo.Save(context.Background(), entity.WalkForwardPersisted{
		ID: "b", BaseProfile: "experiment", PDCACycleID: "cycle01",
		RequestJSON: "{}", ResultJSON: "{}", AggregateOOSJSON: "{}",
	})

	h := NewBacktestHandler(bt.NewBacktestRunner(), &mockBacktestResultRepo{}, WithWalkForwardRepo(repo))
	r := gin.New()
	r.GET("/backtest/walk-forward", h.ListWalkForward)

	req := httptest.NewRequest(http.MethodGet, "/backtest/walk-forward?baseProfile=production", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"id":"a"`) {
		t.Fatalf("filter=production should include a; got: %s", body)
	}
	if strings.Contains(body, `"id":"b"`) {
		t.Fatalf("filter=production should exclude b; got: %s", body)
	}
}
