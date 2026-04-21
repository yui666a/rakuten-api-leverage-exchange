package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

type mockMultiRepo struct {
	saved   *entity.MultiPeriodResult
	findOut *entity.MultiPeriodResult
	listOut []entity.MultiPeriodResult
	filter  *repository.MultiPeriodResultFilter
}

func (m *mockMultiRepo) Save(_ context.Context, r entity.MultiPeriodResult) error {
	m.saved = &r
	return nil
}
func (m *mockMultiRepo) List(_ context.Context, f repository.MultiPeriodResultFilter) ([]entity.MultiPeriodResult, error) {
	m.filter = &f
	return m.listOut, nil
}
func (m *mockMultiRepo) FindByID(_ context.Context, _ string) (*entity.MultiPeriodResult, error) {
	return m.findOut, nil
}

func TestBacktestHandler_RunMulti_503WhenMultiRepoMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBacktestHandler(bt.NewBacktestRunner(), &mockBacktestResultRepo{})

	r := gin.New()
	r.POST("/backtest/run-multi", h.RunMulti)
	body := `{"data":"nowhere.csv","periods":[{"label":"1yr","from":"2025-01-01","to":"2026-01-01"}]}`
	req := httptest.NewRequest(http.MethodPost, "/backtest/run-multi", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (multi repo missing)", w.Code)
	}
}

func TestBacktestHandler_RunMulti_400OnEmptyPeriods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBacktestHandler(
		bt.NewBacktestRunner(),
		&mockBacktestResultRepo{},
		WithMultiPeriodRepo(&mockMultiRepo{}),
	)

	r := gin.New()
	r.POST("/backtest/run-multi", h.RunMulti)
	body := `{"data":"nowhere.csv","periods":[]}`
	req := httptest.NewRequest(http.MethodPost, "/backtest/run-multi", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// gin's binding:"required" rejects the zero-length slice at bind time
	// with 400 before our explicit len check; either path is OK here.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestBacktestHandler_ListMultiResults_PassesFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mpRepo := &mockMultiRepo{listOut: []entity.MultiPeriodResult{{ID: "mp-1"}}}
	h := NewBacktestHandler(
		bt.NewBacktestRunner(),
		&mockBacktestResultRepo{},
		WithMultiPeriodRepo(mpRepo),
	)

	r := gin.New()
	r.GET("/backtest/multi-results", h.ListMultiResults)
	req := httptest.NewRequest(http.MethodGet, "/backtest/multi-results?profileName=production&pdcaCycleId=cycle01&limit=5", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if mpRepo.filter == nil {
		t.Fatalf("filter was not recorded")
	}
	if mpRepo.filter.ProfileName != "production" {
		t.Fatalf("ProfileName filter = %q", mpRepo.filter.ProfileName)
	}
	if mpRepo.filter.PDCACycleID != "cycle01" {
		t.Fatalf("PDCACycleID filter = %q", mpRepo.filter.PDCACycleID)
	}
	if mpRepo.filter.Limit != 5 {
		t.Fatalf("Limit = %d, want 5", mpRepo.filter.Limit)
	}
	var resp struct {
		Results []entity.MultiPeriodResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].ID != "mp-1" {
		t.Fatalf("unexpected results: %+v", resp.Results)
	}
}

func TestBacktestHandler_GetMultiResult_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mpRepo := &mockMultiRepo{findOut: nil}
	h := NewBacktestHandler(
		bt.NewBacktestRunner(),
		&mockBacktestResultRepo{},
		WithMultiPeriodRepo(mpRepo),
	)

	r := gin.New()
	r.GET("/backtest/multi-results/:id", h.GetMultiResult)
	req := httptest.NewRequest(http.MethodGet, "/backtest/multi-results/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestBacktestHandler_GetMultiResult_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	want := &entity.MultiPeriodResult{
		ID:          "mp-1",
		ProfileName: "production",
		Aggregate:   entity.MultiPeriodAggregate{AllPositive: true, GeomMeanReturn: 0.05},
	}
	mpRepo := &mockMultiRepo{findOut: want}
	h := NewBacktestHandler(
		bt.NewBacktestRunner(),
		&mockBacktestResultRepo{},
		WithMultiPeriodRepo(mpRepo),
	)

	r := gin.New()
	r.GET("/backtest/multi-results/:id", h.GetMultiResult)
	req := httptest.NewRequest(http.MethodGet, "/backtest/multi-results/mp-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got entity.MultiPeriodResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "mp-1" || !got.Aggregate.AllPositive {
		t.Fatalf("unexpected: %+v", got)
	}
}
