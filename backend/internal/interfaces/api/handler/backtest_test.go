package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

type mockBacktestResultRepo struct {
	listResults []entity.BacktestResult
	findResult  *entity.BacktestResult
}

func (m *mockBacktestResultRepo) Save(_ context.Context, _ entity.BacktestResult) error { return nil }
func (m *mockBacktestResultRepo) List(_ context.Context, _ repository.BacktestResultFilter) ([]entity.BacktestResult, error) {
	return m.listResults, nil
}
func (m *mockBacktestResultRepo) FindByID(_ context.Context, _ string) (*entity.BacktestResult, error) {
	return m.findResult, nil
}
func (m *mockBacktestResultRepo) DeleteOlderThan(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func TestBacktestHandler_ListResults_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &mockBacktestResultRepo{
		listResults: []entity.BacktestResult{{ID: "bt-1"}},
	}
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)
	w := httptestGet(h.ListResults, "/backtest/results", "/backtest/results?limit=10&offset=0")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestBacktestHandler_GetResult_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &mockBacktestResultRepo{}
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)

	w := httptestRequestWithParam(h.GetResult, "/backtest/results/:id", "id", "missing")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestBacktestHandler_ListResults_InvalidSort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &mockBacktestResultRepo{}
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)

	w := httptestGet(h.ListResults, "/backtest/results", "/backtest/results?sort=created_at:asc")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func httptestRequestWithParam(handler gin.HandlerFunc, route, paramKey, paramValue string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := gin.New()
	r.GET(route, handler)
	req := httptest.NewRequest(http.MethodGet, "/backtest/results/"+paramValue, nil)
	r.ServeHTTP(w, req)
	return w
}

func httptestGet(handler gin.HandlerFunc, route, reqPath string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := gin.New()
	r.GET(route, handler)
	req := httptest.NewRequest(http.MethodGet, reqPath, nil)
	r.ServeHTTP(w, req)
	return w
}
