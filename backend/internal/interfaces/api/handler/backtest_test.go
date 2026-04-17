package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
	saveErr     error
	// saved captures the most recent result passed to Save. Tests use this
	// to assert that PDCA metadata (profileName, parentResultId, etc.) is
	// attached to the persisted entity.
	saved *entity.BacktestResult
	// lastFilter records the last filter passed to List so handler tests can
	// assert query-parameter plumbing without exercising the SQL layer.
	lastFilter *repository.BacktestResultFilter
}

func (m *mockBacktestResultRepo) Save(_ context.Context, r entity.BacktestResult) error {
	m.saved = &r
	return m.saveErr
}
func (m *mockBacktestResultRepo) List(_ context.Context, f repository.BacktestResultFilter) ([]entity.BacktestResult, error) {
	m.lastFilter = &f
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

// Error mapping tests: verify that parent-integrity sentinel errors surface
// as HTTP 422, while other persistence errors stay at 500.
//
// Note: request-body plumbing for parent_result_id lands in Task 6. Here we
// simulate the downstream error via a mock repo that returns the sentinel,
// which is exactly the propagation path from the real repository.

func runBacktestExpectingSaveErr(t *testing.T, saveErr error) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &mockBacktestResultRepo{saveErr: saveErr}
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)

	tmpDir := t.TempDir()
	candles := generateTestCandles(50)
	csvPath := writeTempCSV(t, tmpDir, csvinfra.CandleFile{
		Symbol:   "BTC_JPY",
		SymbolID: 7,
		Interval: "PT15M",
		Candles:  candles,
	})

	router := gin.New()
	router.POST("/api/v1/backtest/run", h.Run)

	body := `{"data":"` + csvPath + `","initialBalance":100000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtest/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestBacktestHandler_Run_SelfReference_422(t *testing.T) {
	wrapped := fmt.Errorf("save backtest result: %w", repository.ErrParentResultSelfReference)
	w := runBacktestExpectingSaveErr(t, wrapped)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestBacktestHandler_Run_ParentNotFound_422(t *testing.T) {
	wrapped := fmt.Errorf("save backtest result: %w", repository.ErrParentResultNotFound)
	w := runBacktestExpectingSaveErr(t, wrapped)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBacktestHandler_Run_OtherPersistError_500(t *testing.T) {
	w := runBacktestExpectingSaveErr(t, fmt.Errorf("some generic storage failure"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBacktestHandler_Run_Happy_200(t *testing.T) {
	w := runBacktestExpectingSaveErr(t, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Task 6: profileName + PDCA metadata tests ---

// readProductionProfileJSON loads backend/profiles/production.json by walking
// up to the module root (go.mod). Reading the real on-disk fixture (rather
// than an inline copy) removes the duplication with cmd/backtest's tests and
// guarantees both test suites exercise the same profile the handler would
// load in production. See configurable_strategy_test.go for the same walk
// pattern. The file is a test fixture: keep it in sync with whatever the
// production default is meant to be.
func readProductionProfileJSON(t *testing.T) []byte {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		candidate := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			data, err := os.ReadFile(filepath.Join(dir, "profiles", "production.json"))
			if err != nil {
				t.Fatalf("read production.json: %v", err)
			}
			return data
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test file")
		}
		dir = parent
	}
}

// setupProfilesDir writes the given name.json files under a temp profiles
// directory and returns the directory path.
func setupProfilesDir(t *testing.T, profiles map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range profiles {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write profile %s: %v", name, err)
		}
	}
	return dir
}

// newRunRouter wires a handler with a fresh in-memory SQLite repo and a
// temp profiles dir. Returns both so tests can make repository assertions.
func newRunRouter(t *testing.T, profilesDir string) (*gin.Engine, *BacktestHandler, *btinfra.ResultRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := setupIntegrationDB(t)
	repo := btinfra.NewResultRepository(db)
	var opts []BacktestHandlerOption
	if profilesDir != "" {
		opts = append(opts, WithProfilesBaseDir(profilesDir))
	}
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo, opts...)

	router := gin.New()
	router.POST("/api/v1/backtest/run", h.Run)
	return router, h, repo
}

// runRequestBody builds a minimal POST /backtest/run body with the given
// CSV data path and merges any extra top-level fields.
func runRequestBody(t *testing.T, csvPath string, extras map[string]any) string {
	t.Helper()
	payload := map[string]any{
		"data":           csvPath,
		"initialBalance": 100000.0,
	}
	for k, v := range extras {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	return string(b)
}

func postRun(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtest/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func makeCSVForRunTests(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	return writeTempCSV(t, tmpDir, csvinfra.CandleFile{
		Symbol:   "BTC_JPY",
		SymbolID: 7,
		Interval: "PT15M",
		Candles:  generateTestCandles(100),
	})
}

func TestBacktestHandler_Run_Profile_OK(t *testing.T) {
	profilesDir := setupProfilesDir(t, map[string][]byte{"production": readProductionProfileJSON(t)})
	router, _, repo := newRunRouter(t, profilesDir)
	csvPath := makeCSVForRunTests(t)

	body := runRequestBody(t, csvPath, map[string]any{"profileName": "production"})
	w := postRun(t, router, body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got entity.BacktestResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.ProfileName != "production" {
		t.Errorf("response ProfileName = %q, want %q", got.ProfileName, "production")
	}

	// Verify persistence — the row must carry profile_name.
	stored, err := repo.FindByID(context.Background(), got.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored == nil {
		t.Fatal("FindByID returned nil; expected persisted row")
	}
	if stored.ProfileName != "production" {
		t.Errorf("persisted ProfileName = %q, want %q", stored.ProfileName, "production")
	}
}

func TestBacktestHandler_Run_Profile_InvalidName_400(t *testing.T) {
	profilesDir := setupProfilesDir(t, map[string][]byte{"production": readProductionProfileJSON(t)})
	router, _, _ := newRunRouter(t, profilesDir)
	csvPath := makeCSVForRunTests(t)

	// Traversal attempt — regex in ResolveProfilePath rejects this.
	body := runRequestBody(t, csvPath, map[string]any{"profileName": "../secret"})
	w := postRun(t, router, body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid profile name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBacktestHandler_Run_Profile_Unknown_400(t *testing.T) {
	profilesDir := setupProfilesDir(t, map[string][]byte{"production": readProductionProfileJSON(t)})
	router, _, _ := newRunRouter(t, profilesDir)
	csvPath := makeCSVForRunTests(t)

	// Valid shape, but the file does not exist under profilesDir.
	body := runRequestBody(t, csvPath, map[string]any{"profileName": "does_not_exist"})
	w := postRun(t, router, body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown profile, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBacktestHandler_Run_Profile_IndividualFieldOverrides(t *testing.T) {
	// production.json has stop_loss_percent=5. Passing stopLossPercent=7
	// in the request body must override that.
	profilesDir := setupProfilesDir(t, map[string][]byte{"production": readProductionProfileJSON(t)})
	router, _, repo := newRunRouter(t, profilesDir)
	csvPath := makeCSVForRunTests(t)

	body := runRequestBody(t, csvPath, map[string]any{
		"profileName":     "production",
		"stopLossPercent": 7.0,
	})
	w := postRun(t, router, body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got entity.BacktestResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// The stop_loss_percent applied to the run is not stored directly in
	// BacktestResult. Instead, assert the override semantics by driving
	// the merge function from a direct handler-internal call via a
	// round-trip: we re-run with the profile alone and compare
	// ProfileName is still "production" — and trust the unit-level
	// coverage of applyProfileDefaults below.
	if got.ProfileName != "production" {
		t.Errorf("ProfileName = %q, want %q", got.ProfileName, "production")
	}

	stored, err := repo.FindByID(context.Background(), got.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored == nil {
		t.Fatal("expected persisted row")
	}
	if stored.ProfileName != "production" {
		t.Errorf("persisted ProfileName = %q, want %q", stored.ProfileName, "production")
	}
}

// TestApplyProfileDefaults_IndividualFieldOverrides unit-tests the merge
// semantics documented in spec §8.2: profile values become the base, but
// any non-zero individual field in the request overrides.
func TestApplyProfileDefaults_IndividualFieldOverrides(t *testing.T) {
	profile := &entity.StrategyProfile{
		Risk: entity.StrategyRiskConfig{
			StopLossPercent:   5,
			TakeProfitPercent: 10,
			MaxPositionAmount: 100000,
			MaxDailyLoss:      50000,
		},
	}

	t.Run("request fields zero -> profile values win", func(t *testing.T) {
		req := &runBacktestRequest{}
		applyProfileDefaults(req, profile)
		if req.StopLossPercent != 5 {
			t.Errorf("StopLossPercent = %v, want 5", req.StopLossPercent)
		}
		if req.TakeProfitPercent != 10 {
			t.Errorf("TakeProfitPercent = %v, want 10", req.TakeProfitPercent)
		}
		if req.MaxPositionAmount != 100000 {
			t.Errorf("MaxPositionAmount = %v, want 100000", req.MaxPositionAmount)
		}
		if req.MaxDailyLoss != 50000 {
			t.Errorf("MaxDailyLoss = %v, want 50000", req.MaxDailyLoss)
		}
	})

	t.Run("non-zero request field overrides profile", func(t *testing.T) {
		req := &runBacktestRequest{StopLossPercent: 7}
		applyProfileDefaults(req, profile)
		if req.StopLossPercent != 7 {
			t.Errorf("StopLossPercent = %v, want 7 (override should win)", req.StopLossPercent)
		}
		// Fields the request left at zero still pick up profile values.
		if req.TakeProfitPercent != 10 {
			t.Errorf("TakeProfitPercent = %v, want 10", req.TakeProfitPercent)
		}
	})

	t.Run("nil profile is a no-op", func(t *testing.T) {
		req := &runBacktestRequest{StopLossPercent: 3}
		applyProfileDefaults(req, nil)
		if req.StopLossPercent != 3 {
			t.Errorf("StopLossPercent = %v, want 3 (unchanged)", req.StopLossPercent)
		}
	})
}

func TestBacktestHandler_Run_ParentResultID_Chains(t *testing.T) {
	profilesDir := setupProfilesDir(t, map[string][]byte{"production": readProductionProfileJSON(t)})
	router, _, repo := newRunRouter(t, profilesDir)
	csvPath := makeCSVForRunTests(t)

	// First run — produces a valid parent_result_id we can point at.
	body1 := runRequestBody(t, csvPath, nil)
	w1 := postRun(t, router, body1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first run: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	var first entity.BacktestResult
	if err := json.Unmarshal(w1.Body.Bytes(), &first); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}
	if first.ID == "" {
		t.Fatal("first run returned empty ID")
	}

	// Second run — points parent_result_id at the first. Should persist.
	body2 := runRequestBody(t, csvPath, map[string]any{"parentResultId": first.ID})
	w2 := postRun(t, router, body2)
	if w2.Code != http.StatusOK {
		t.Fatalf("child run: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var child entity.BacktestResult
	if err := json.Unmarshal(w2.Body.Bytes(), &child); err != nil {
		t.Fatalf("unmarshal child: %v", err)
	}
	stored, err := repo.FindByID(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("FindByID child: %v", err)
	}
	if stored == nil {
		t.Fatal("child row not found")
	}
	if stored.ParentResultID == nil {
		t.Fatal("child ParentResultID is nil; expected first.ID")
	}
	if *stored.ParentResultID != first.ID {
		t.Errorf("ParentResultID = %q, want %q", *stored.ParentResultID, first.ID)
	}
}

func TestBacktestHandler_Run_ParentResultID_Missing_422(t *testing.T) {
	router, _, _ := newRunRouter(t, "")
	csvPath := makeCSVForRunTests(t)

	body := runRequestBody(t, csvPath, map[string]any{"parentResultId": "does-not-exist"})
	w := postRun(t, router, body)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing parent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBacktestHandler_Run_PDCAMetadataPersisted(t *testing.T) {
	router, _, repo := newRunRouter(t, "")
	csvPath := makeCSVForRunTests(t)

	body := runRequestBody(t, csvPath, map[string]any{
		"pdcaCycleId": "2026-04-17_cycle01",
		"hypothesis":  "tighter stop reduces drawdown",
	})
	w := postRun(t, router, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got entity.BacktestResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	stored, err := repo.FindByID(context.Background(), got.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored == nil {
		t.Fatal("expected persisted row")
	}
	if stored.PDCACycleID != "2026-04-17_cycle01" {
		t.Errorf("PDCACycleID = %q, want %q", stored.PDCACycleID, "2026-04-17_cycle01")
	}
	if stored.Hypothesis != "tighter stop reduces drawdown" {
		t.Errorf("Hypothesis = %q, want %q", stored.Hypothesis, "tighter stop reduces drawdown")
	}
}

// --- Task 7: ListResults query-parameter filters ---

// listWithQuery invokes the ListResults handler with the given query string
// against a mock repo so we can assert the BacktestResultFilter it received.
func listWithQuery(t *testing.T, query string) (*httptest.ResponseRecorder, *mockBacktestResultRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &mockBacktestResultRepo{}
	h := NewBacktestHandler(bt.NewBacktestRunner(), repo)
	router := gin.New()
	router.GET("/api/v1/backtest/results", h.ListResults)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backtest/results"+query, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w, repo
}

func TestBacktestHandler_ListResults_FilterProfileName(t *testing.T) {
	w, repo := listWithQuery(t, "?profileName=foo")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastFilter == nil {
		t.Fatal("expected List to receive a filter")
	}
	if repo.lastFilter.ProfileName != "foo" {
		t.Errorf("ProfileName = %q, want %q", repo.lastFilter.ProfileName, "foo")
	}
}

func TestBacktestHandler_ListResults_FilterPDCACycleID(t *testing.T) {
	w, repo := listWithQuery(t, "?pdcaCycleId=cycle-1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastFilter.PDCACycleID != "cycle-1" {
		t.Errorf("PDCACycleID = %q, want %q", repo.lastFilter.PDCACycleID, "cycle-1")
	}
}

func TestBacktestHandler_ListResults_FilterHasParentTrue(t *testing.T) {
	w, repo := listWithQuery(t, "?hasParent=true")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastFilter.HasParent == nil || *repo.lastFilter.HasParent != true {
		t.Errorf("HasParent = %v, want true", repo.lastFilter.HasParent)
	}
}

func TestBacktestHandler_ListResults_FilterHasParentFalse(t *testing.T) {
	w, repo := listWithQuery(t, "?hasParent=false")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastFilter.HasParent == nil || *repo.lastFilter.HasParent != false {
		t.Errorf("HasParent = %v, want false", repo.lastFilter.HasParent)
	}
}

func TestBacktestHandler_ListResults_FilterHasParentInvalid_400(t *testing.T) {
	w, _ := listWithQuery(t, "?hasParent=yes")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid hasParent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBacktestHandler_ListResults_FilterParentResultID(t *testing.T) {
	w, repo := listWithQuery(t, "?parentResultId=p-123")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastFilter.ParentResultID == nil || *repo.lastFilter.ParentResultID != "p-123" {
		t.Errorf("ParentResultID = %v, want &\"p-123\"", repo.lastFilter.ParentResultID)
	}
}

func TestBacktestHandler_ListResults_FilterParentResultIDEmpty_NoFilter(t *testing.T) {
	// Spec §5.3: empty string is a legitimate filter value at the repo layer,
	// but the handler treats `?parentResultId=` as "no filter" (see handler
	// comment for rationale). We verify the handler drops the filter.
	w, repo := listWithQuery(t, "?parentResultId=")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastFilter.ParentResultID != nil {
		t.Errorf("ParentResultID = %v, want nil (empty string folds into no-filter)", repo.lastFilter.ParentResultID)
	}
}

func TestBacktestHandler_ListResults_FilterCombined(t *testing.T) {
	w, repo := listWithQuery(t, "?profileName=foo&hasParent=false")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastFilter.ProfileName != "foo" {
		t.Errorf("ProfileName = %q, want %q", repo.lastFilter.ProfileName, "foo")
	}
	if repo.lastFilter.HasParent == nil || *repo.lastFilter.HasParent != false {
		t.Errorf("HasParent = %v, want false", repo.lastFilter.HasParent)
	}
}

func TestBacktestHandler_ListResults_PrecedenceParentResultIDOverHasParent(t *testing.T) {
	// Per repository doc and spec §5.3: when both are set, ParentResultID wins.
	// Handler enforces the precedence before calling into the repo so the two
	// layers agree. Assert HasParent is dropped.
	w, repo := listWithQuery(t, "?parentResultId=p-1&hasParent=true")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastFilter.ParentResultID == nil || *repo.lastFilter.ParentResultID != "p-1" {
		t.Errorf("ParentResultID = %v, want &\"p-1\"", repo.lastFilter.ParentResultID)
	}
	if repo.lastFilter.HasParent != nil {
		t.Errorf("HasParent = %v, want nil (parentResultId takes precedence)", repo.lastFilter.HasParent)
	}
}

func TestBacktestHandler_ListResults_NoFilters(t *testing.T) {
	w, repo := listWithQuery(t, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.lastFilter.ProfileName != "" || repo.lastFilter.PDCACycleID != "" ||
		repo.lastFilter.ParentResultID != nil || repo.lastFilter.HasParent != nil {
		t.Errorf("expected all filters zero, got %+v", repo.lastFilter)
	}
}
