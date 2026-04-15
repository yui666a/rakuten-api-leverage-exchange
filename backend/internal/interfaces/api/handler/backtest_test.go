package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	btinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/backtest"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
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

func TestBacktestHandler_CSVMeta_MissingData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &mockBacktestResultRepo{}
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)

	w := httptestGet(h.CSVMeta, "/backtest/csv-meta", "/backtest/csv-meta")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestBacktestHandler_CSVMeta_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &mockBacktestResultRepo{}
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)

	tmpDir := t.TempDir()
	csvPath := writeTempCSV(t, tmpDir, csvinfra.CandleFile{
		Symbol:   "LTC_JPY",
		SymbolID: 10,
		Interval: "PT15M",
		Candles: []entity.Candle{
			{Time: 3000, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 1},
			{Time: 1000, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 1},
			{Time: 2000, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 1},
		},
	})

	w := httptestGet(h.CSVMeta, "/backtest/csv-meta", "/backtest/csv-meta?data="+csvPath)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data          string `json:"data"`
		Symbol        string `json:"symbol"`
		SymbolID      int64  `json:"symbolId"`
		Interval      string `json:"interval"`
		RowCount      int    `json:"rowCount"`
		FromTimestamp int64  `json:"fromTimestamp"`
		ToTimestamp   int64  `json:"toTimestamp"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal csv meta: %v", err)
	}
	if resp.Data != csvPath {
		t.Fatalf("expected data path %s, got %s", csvPath, resp.Data)
	}
	if resp.Symbol != "LTC_JPY" || resp.SymbolID != 10 || resp.Interval != "PT15M" {
		t.Fatalf("unexpected symbol meta: %+v", resp)
	}
	if resp.RowCount != 3 {
		t.Fatalf("expected rowCount=3, got %d", resp.RowCount)
	}
	if resp.FromTimestamp != 1000 || resp.ToTimestamp != 3000 {
		t.Fatalf("unexpected range: %d..%d", resp.FromTimestamp, resp.ToTimestamp)
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

// --- Integration test: Run -> List -> Get ---

func setupIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys%3don")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return db
}

func writeTempCSV(t *testing.T, dir string, cf csvinfra.CandleFile) string {
	t.Helper()
	path := filepath.Join(dir, "candles.csv")
	if err := csvinfra.SaveCandles(path, cf); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return path
}

func generateTestCandles(n int) []entity.Candle {
	candles := make([]entity.Candle, 0, n)
	baseTime := int64(1_770_000_000_000)
	price := 15_000_000.0
	for i := 0; i < n; i++ {
		price += math.Sin(float64(i)/5.0) * 20000
		ts := baseTime + int64(i)*15*60*1000
		candles = append(candles, entity.Candle{
			Open:   price - 5000,
			High:   price + 10000,
			Low:    price - 10000,
			Close:  price,
			Volume: 1.5,
			Time:   ts,
		})
	}
	return candles
}

func TestBacktestHandler_Integration_RunListGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupIntegrationDB(t)
	repo := btinfra.NewResultRepository(db)
	runner := bt.NewBacktestRunner()
	h := NewBacktestHandler(runner, repo)

	tmpDir := t.TempDir()
	candles := generateTestCandles(100)
	csvPath := writeTempCSV(t, tmpDir, csvinfra.CandleFile{
		Symbol:   "BTC_JPY",
		SymbolID: 7,
		Interval: "PT15M",
		Candles:  candles,
	})

	router := gin.New()
	router.POST("/api/v1/backtest/run", h.Run)
	router.GET("/api/v1/backtest/results", h.ListResults)
	router.GET("/api/v1/backtest/results/:id", h.GetResult)

	// Step 1: Run backtest
	body := `{"data":"` + csvPath + `","initialBalance":100000}`
	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/backtest/run", strings.NewReader(body))
	runReq.Header.Set("Content-Type", "application/json")
	runW := httptest.NewRecorder()
	router.ServeHTTP(runW, runReq)

	if runW.Code != http.StatusOK {
		t.Fatalf("Run: expected 200, got %d: %s", runW.Code, runW.Body.String())
	}

	var runResult entity.BacktestResult
	if err := json.Unmarshal(runW.Body.Bytes(), &runResult); err != nil {
		t.Fatalf("unmarshal run result: %v", err)
	}
	if runResult.ID == "" {
		t.Fatal("Run: expected non-empty result ID")
	}
	if runResult.Summary.InitialBalance != 100000 {
		t.Fatalf("Run: expected initial balance 100000, got %v", runResult.Summary.InitialBalance)
	}

	// Step 2: List results
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/backtest/results?limit=10&offset=0", nil)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("List: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}

	var listResp struct {
		Results []entity.BacktestResult `json:"results"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if len(listResp.Results) != 1 {
		t.Fatalf("List: expected 1 result, got %d", len(listResp.Results))
	}
	if listResp.Results[0].ID != runResult.ID {
		t.Fatalf("List: ID mismatch: %s != %s", listResp.Results[0].ID, runResult.ID)
	}

	// Step 3: Get result by ID
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/backtest/results/"+runResult.ID, nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("Get: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}

	var getResult entity.BacktestResult
	if err := json.Unmarshal(getW.Body.Bytes(), &getResult); err != nil {
		t.Fatalf("unmarshal get result: %v", err)
	}
	if getResult.ID != runResult.ID {
		t.Fatalf("Get: ID mismatch: %s != %s", getResult.ID, runResult.ID)
	}
	if getResult.Summary.TotalReturn != runResult.Summary.TotalReturn {
		t.Fatalf("Get: TotalReturn mismatch: %v != %v", getResult.Summary.TotalReturn, runResult.Summary.TotalReturn)
	}
}

func TestBacktestHandler_Integration_RunWithRiskParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupIntegrationDB(t)
	repo := btinfra.NewResultRepository(db)
	runner := bt.NewBacktestRunner()
	h := NewBacktestHandler(runner, repo)

	tmpDir := t.TempDir()
	candles := generateTestCandles(100)
	csvPath := writeTempCSV(t, tmpDir, csvinfra.CandleFile{
		Symbol:   "BTC_JPY",
		SymbolID: 7,
		Interval: "PT15M",
		Candles:  candles,
	})

	router := gin.New()
	router.POST("/api/v1/backtest/run", h.Run)

	body := `{"data":"` + csvPath + `","initialBalance":100000,"stopLossPercent":3,"takeProfitPercent":8}`
	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/backtest/run", strings.NewReader(body))
	runReq.Header.Set("Content-Type", "application/json")
	runW := httptest.NewRecorder()
	router.ServeHTTP(runW, runReq)

	if runW.Code != http.StatusOK {
		t.Fatalf("Run with risk params: expected 200, got %d: %s", runW.Code, runW.Body.String())
	}

	var result entity.BacktestResult
	if err := json.Unmarshal(runW.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ID == "" {
		t.Fatal("expected non-empty result ID")
	}
}

func TestBacktestHandler_Integration_GetNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupIntegrationDB(t)
	repo := btinfra.NewResultRepository(db)
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)

	router := gin.New()
	router.GET("/api/v1/backtest/results/:id", h.GetResult)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/backtest/results/nonexistent-id", nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusNotFound {
		t.Fatalf("Get nonexistent: expected 404, got %d", getW.Code)
	}
}

func TestBacktestHandler_Integration_RunInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupIntegrationDB(t)
	repo := btinfra.NewResultRepository(db)
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)

	router := gin.New()
	router.POST("/api/v1/backtest/run", h.Run)

	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/backtest/run", strings.NewReader(`{invalid`))
	runReq.Header.Set("Content-Type", "application/json")
	runW := httptest.NewRecorder()
	router.ServeHTTP(runW, runReq)

	if runW.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON: expected 400, got %d", runW.Code)
	}
}

func TestBacktestHandler_Integration_RunMissingData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupIntegrationDB(t)
	repo := btinfra.NewResultRepository(db)
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)

	router := gin.New()
	router.POST("/api/v1/backtest/run", h.Run)

	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/backtest/run", strings.NewReader(`{"initialBalance":100000}`))
	runReq.Header.Set("Content-Type", "application/json")
	runW := httptest.NewRecorder()
	router.ServeHTTP(runW, runReq)

	if runW.Code != http.StatusBadRequest {
		t.Fatalf("missing data: expected 400, got %d", runW.Code)
	}
}
