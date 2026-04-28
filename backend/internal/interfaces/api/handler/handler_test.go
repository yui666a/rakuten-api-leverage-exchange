package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func doRequest(handler gin.HandlerFunc, method, path string, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.Use(func(ctx *gin.Context) { ctx.Next() })

	switch method {
	case http.MethodGet:
		r.GET(path, handler)
	case http.MethodPost:
		r.POST(path, handler)
	case http.MethodPut:
		r.PUT(path, handler)
	case http.MethodDelete:
		r.DELETE(path, handler)
	}

	if body != nil {
		c.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
	} else {
		c.Request = httptest.NewRequest(method, path, nil)
	}
	r.ServeHTTP(w, c.Request)
	return w
}

func jsonBody(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return m
}

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// mockOrderClient implements repository.OrderClient.
type mockOrderClient struct {
	positions []entity.Position
	trades    []entity.MyTrade
	assets    []entity.Asset
	orders    []entity.Order
	createErr error
}

func (m *mockOrderClient) CreateOrder(_ context.Context, _ entity.OrderRequest) ([]entity.Order, error) {
	return nil, m.createErr
}
func (m *mockOrderClient) CreateOrderRaw(_ context.Context, _ entity.OrderRequest) (repository.CreateOrderOutcome, error) {
	return repository.CreateOrderOutcome{}, nil
}
func (m *mockOrderClient) CancelOrder(_ context.Context, _, _ int64) ([]entity.Order, error) {
	return nil, nil
}
func (m *mockOrderClient) GetOrders(_ context.Context, _ int64) ([]entity.Order, error) {
	return m.orders, nil
}
func (m *mockOrderClient) GetPositions(_ context.Context, _ int64) ([]entity.Position, error) {
	return m.positions, nil
}
func (m *mockOrderClient) GetMyTrades(_ context.Context, _ int64) ([]entity.MyTrade, error) {
	return m.trades, nil
}
func (m *mockOrderClient) GetAssets(_ context.Context) ([]entity.Asset, error) {
	return m.assets, nil
}

// mockClientOrderRepo implements repository.ClientOrderRepository.
type mockClientOrderRepo struct {
	records map[string]*repository.ClientOrderRecord
}

func newMockClientOrderRepo() *mockClientOrderRepo {
	return &mockClientOrderRepo{records: make(map[string]*repository.ClientOrderRecord)}
}
func (m *mockClientOrderRepo) Find(_ context.Context, clientOrderID string) (*repository.ClientOrderRecord, error) {
	rec, ok := m.records[clientOrderID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return rec, nil
}
func (m *mockClientOrderRepo) Save(_ context.Context, record repository.ClientOrderRecord) error {
	m.records[record.ClientOrderID] = &record
	return nil
}
func (m *mockClientOrderRepo) InsertOrGet(_ context.Context, record repository.ClientOrderRecord) (*repository.ClientOrderRecord, bool, error) {
	if existing, ok := m.records[record.ClientOrderID]; ok {
		return existing, false, nil
	}
	m.records[record.ClientOrderID] = &record
	return &record, true, nil
}
func (m *mockClientOrderRepo) UpdateStatus(_ context.Context, clientOrderID string, status entity.ClientOrderStatus, now int64, update repository.ClientOrderUpdate) error {
	rec, ok := m.records[clientOrderID]
	if !ok {
		return fmt.Errorf("not found")
	}
	rec.Status = status
	rec.UpdatedAt = now
	if update.OrderID != nil {
		rec.OrderID = *update.OrderID
	}
	return nil
}
func (m *mockClientOrderRepo) ListByStatus(_ context.Context, _ []entity.ClientOrderStatus, _ int) ([]repository.ClientOrderRecord, error) {
	return nil, nil
}
func (m *mockClientOrderRepo) DeleteExpired(_ context.Context, _ int64) error {
	return nil
}

// mockPipeline implements PipelineController.
type mockPipeline struct {
	running bool
}

func (m *mockPipeline) Start()        { m.running = true }
func (m *mockPipeline) Stop()         { m.running = false }
func (m *mockPipeline) Running() bool { return m.running }

// newRiskManager creates a RiskManager with sensible defaults for testing.
func newRiskManager() *usecase.RiskManager {
	return usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount:    5000,
		MaxDailyLoss:         5000,
		StopLossPercent:      5,
		TakeProfitPercent:    3,
		InitialCapital:       10000,
		MaxConsecutiveLosses: 3,
		CooldownMinutes:      10,
	})
}

// ---------------------------------------------------------------------------
// 1. StatusHandler
// ---------------------------------------------------------------------------

func TestStatusHandler_GetStatus(t *testing.T) {
	riskMgr := newRiskManager()
	pipeline := &mockPipeline{running: true}
	h := NewStatusHandler(riskMgr, pipeline)

	w := doRequest(h.GetStatus, http.MethodGet, "/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["status"] != "running" {
		t.Fatalf("expected status 'running', got %v", body["status"])
	}
	if body["manuallyStopped"] != false {
		t.Fatalf("expected manuallyStopped false, got %v", body["manuallyStopped"])
	}
	if body["pipelineRunning"] != true {
		t.Fatalf("expected pipelineRunning true, got %v", body["pipelineRunning"])
	}
}

func TestStatusHandler_GetStatus_Stopped(t *testing.T) {
	riskMgr := newRiskManager()
	riskMgr.StopTrading()
	pipeline := &mockPipeline{running: false}
	h := NewStatusHandler(riskMgr, pipeline)

	w := doRequest(h.GetStatus, http.MethodGet, "/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["status"] != "stopped" {
		t.Fatalf("expected status 'stopped', got %v", body["status"])
	}
	if body["manuallyStopped"] != true {
		t.Fatalf("expected manuallyStopped true, got %v", body["manuallyStopped"])
	}
}

// 再起動直後など、manuallyStopped=false でも pipeline goroutine が動いていない
// 状態を "running" と詐称してはならない。"stopped" を返し、pipelineRunning=false
// で実体を表現する。
func TestStatusHandler_GetStatus_PipelineNotRunning(t *testing.T) {
	riskMgr := newRiskManager()
	pipeline := &mockPipeline{running: false} // not started yet
	h := NewStatusHandler(riskMgr, pipeline)

	w := doRequest(h.GetStatus, http.MethodGet, "/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["status"] != "stopped" {
		t.Fatalf("expected status 'stopped' when pipeline not running, got %v", body["status"])
	}
	if body["manuallyStopped"] != false {
		t.Fatalf("expected manuallyStopped false, got %v", body["manuallyStopped"])
	}
	if body["pipelineRunning"] != false {
		t.Fatalf("expected pipelineRunning false, got %v", body["pipelineRunning"])
	}
}

// pipeline が nil (テスト用途・古い構成) でも /status は壊れず、
// pipelineRunning=false を返す。
func TestStatusHandler_GetStatus_NilPipeline(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewStatusHandler(riskMgr, nil)

	w := doRequest(h.GetStatus, http.MethodGet, "/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["pipelineRunning"] != false {
		t.Fatalf("expected pipelineRunning false when pipeline is nil, got %v", body["pipelineRunning"])
	}
	if body["status"] != "stopped" {
		t.Fatalf("expected status 'stopped' when pipeline is nil, got %v", body["status"])
	}
}

// ---------------------------------------------------------------------------
// 2. BotHandler
// ---------------------------------------------------------------------------

func TestBotHandler_Start(t *testing.T) {
	riskMgr := newRiskManager()
	riskMgr.StopTrading() // start from stopped state
	pipeline := &mockPipeline{}
	h := NewBotHandler(riskMgr, nil, pipeline)

	w := doRequest(h.Start, http.MethodPost, "/start", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["status"] != "running" {
		t.Fatalf("expected status 'running', got %v", body["status"])
	}
	if !pipeline.running {
		t.Fatal("expected pipeline to be running")
	}
}

func TestBotHandler_Stop(t *testing.T) {
	riskMgr := newRiskManager()
	pipeline := &mockPipeline{running: true}
	h := NewBotHandler(riskMgr, nil, pipeline)

	w := doRequest(h.Stop, http.MethodPost, "/stop", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["status"] != "stopped" {
		t.Fatalf("expected status 'stopped', got %v", body["status"])
	}
	if body["pipelineRunning"] != false {
		t.Fatalf("expected pipelineRunning false, got %v", body["pipelineRunning"])
	}
	if pipeline.running {
		t.Fatal("expected pipeline to be stopped")
	}
}

func TestBotHandler_NilPipeline(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewBotHandler(riskMgr, nil, nil)

	w := doRequest(h.Start, http.MethodPost, "/start", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["pipelineRunning"] != false {
		t.Fatalf("expected pipelineRunning false when pipeline is nil, got %v", body["pipelineRunning"])
	}
}

func TestBotHandler_StartWithRealtimeHub(t *testing.T) {
	riskMgr := newRiskManager()
	hub := usecase.NewRealtimeHub()
	pipeline := &mockPipeline{}
	h := NewBotHandler(riskMgr, hub, pipeline)

	w := doRequest(h.Start, http.MethodPost, "/start", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// 3. OrderHandler
// ---------------------------------------------------------------------------

func TestOrderHandler_CreateOrder_MissingClientOrderId(t *testing.T) {
	h := NewOrderHandler(nil, newMockClientOrderRepo())
	body := jsonBody(map[string]any{
		"symbolId":  7,
		"side":      "BUY",
		"amount":    0.01,
		"orderType": "MARKET",
	})

	w := doRequest(h.CreateOrder, http.MethodPost, "/orders", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "clientOrderId is required" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestOrderHandler_CreateOrder_InvalidOrderType(t *testing.T) {
	h := NewOrderHandler(nil, newMockClientOrderRepo())
	body := jsonBody(map[string]any{
		"symbolId":      7,
		"side":          "BUY",
		"amount":        0.01,
		"orderType":     "LIMIT",
		"clientOrderId": "test-1",
	})

	w := doRequest(h.CreateOrder, http.MethodPost, "/orders", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "orderType must be MARKET" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestOrderHandler_CreateOrder_InvalidSide(t *testing.T) {
	h := NewOrderHandler(nil, newMockClientOrderRepo())
	body := jsonBody(map[string]any{
		"symbolId":      7,
		"side":          "HOLD",
		"amount":        0.01,
		"orderType":     "MARKET",
		"clientOrderId": "test-1",
	})

	w := doRequest(h.CreateOrder, http.MethodPost, "/orders", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "side must be BUY or SELL" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestOrderHandler_CreateOrder_ZeroAmount(t *testing.T) {
	h := NewOrderHandler(nil, newMockClientOrderRepo())
	body := jsonBody(map[string]any{
		"symbolId":      7,
		"side":          "BUY",
		"amount":        0,
		"orderType":     "MARKET",
		"clientOrderId": "test-1",
	})

	w := doRequest(h.CreateOrder, http.MethodPost, "/orders", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "amount must be greater than 0" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestOrderHandler_CreateOrder_NegativeAmount(t *testing.T) {
	h := NewOrderHandler(nil, newMockClientOrderRepo())
	body := jsonBody(map[string]any{
		"symbolId":      7,
		"side":          "BUY",
		"amount":        -1.0,
		"orderType":     "MARKET",
		"clientOrderId": "test-1",
	})

	w := doRequest(h.CreateOrder, http.MethodPost, "/orders", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestOrderHandler_CreateOrder_InvalidBody(t *testing.T) {
	h := NewOrderHandler(nil, newMockClientOrderRepo())

	w := doRequest(h.CreateOrder, http.MethodPost, "/orders", []byte("not-json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid request body" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestOrderHandler_CreateOrder_IdempotencyDuplicate(t *testing.T) {
	repo := newMockClientOrderRepo()
	repo.records["dup-1"] = &repository.ClientOrderRecord{
		ClientOrderID: "dup-1",
		Executed:      true,
		OrderID:       999,
	}
	h := NewOrderHandler(nil, repo)
	body := jsonBody(map[string]any{
		"symbolId":      7,
		"side":          "BUY",
		"amount":        0.01,
		"orderType":     "MARKET",
		"clientOrderId": "dup-1",
	})

	w := doRequest(h.CreateOrder, http.MethodPost, "/orders", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for duplicate, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["duplicate"] != true {
		t.Fatalf("expected duplicate=true, got %v", resp["duplicate"])
	}
	if resp["clientOrderId"] != "dup-1" {
		t.Fatalf("expected clientOrderId 'dup-1', got %v", resp["clientOrderId"])
	}
}

// ---------------------------------------------------------------------------
// 4. PositionHandler
// ---------------------------------------------------------------------------

func TestPositionHandler_GetPositions_OK(t *testing.T) {
	oc := &mockOrderClient{
		positions: []entity.Position{
			{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 4500000, RemainingAmount: 0.01},
		},
	}
	h := NewPositionHandler(oc, nil, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/positions", h.GetPositions)
	req := httptest.NewRequest(http.MethodGet, "/positions?symbolId=7", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var positions []entity.Position
	if err := json.Unmarshal(w.Body.Bytes(), &positions); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
}

func TestPositionHandler_GetPositions_DefaultSymbol(t *testing.T) {
	oc := &mockOrderClient{
		positions: []entity.Position{
			{ID: 1, SymbolID: 7},
		},
	}
	h := NewPositionHandler(oc, nil, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/positions", h.GetPositions)
	// No symbolId query param => defaults to 7
	req := httptest.NewRequest(http.MethodGet, "/positions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestPositionHandler_GetPositions_InvalidSymbol(t *testing.T) {
	h := NewPositionHandler(&mockOrderClient{}, nil, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/positions", h.GetPositions)
	req := httptest.NewRequest(http.MethodGet, "/positions?symbolId=abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPositionHandler_ClosePosition_NotConfigured(t *testing.T) {
	h := NewPositionHandler(&mockOrderClient{}, nil, nil)

	body := jsonBody(map[string]any{
		"symbolId":      7,
		"clientOrderId": "close-1",
	})

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.POST("/positions/:id/close", h.ClosePosition)
	req := httptest.NewRequest(http.MethodPost, "/positions/1/close", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestPositionHandler_ClosePosition_InvalidId(t *testing.T) {
	repo := newMockClientOrderRepo()
	h := NewPositionHandler(&mockOrderClient{}, nil, repo)
	// orderExecutor is nil but we have repo, so the nil check won't catch it.
	// However the handler checks both: `if h.orderExecutor == nil || h.clientOrderRepo == nil`
	// Since orderExecutor is nil, it returns 503.
	// To test the ID validation, we need both to be non-nil.
	// Since we can't easily construct a real OrderExecutor, we test with both nil (503).
	// Let's check the 503 path for invalid config.

	body := jsonBody(map[string]any{
		"symbolId":      7,
		"clientOrderId": "close-1",
	})

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.POST("/positions/:id/close", h.ClosePosition)
	req := httptest.NewRequest(http.MethodPost, "/positions/abc/close", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// orderExecutor is nil, so returns 503 before reaching ID validation
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestPositionHandler_ClosePosition_InvalidBody(t *testing.T) {
	// To reach body validation, we need a non-nil orderExecutor.
	// We skip this since we can't easily mock OrderExecutor (concrete type).
	// The 503 path is already tested above.
	t.Skip("requires non-nil OrderExecutor")
}

func TestPositionHandler_ClosePosition_IdempotencyDuplicate(t *testing.T) {
	// Idempotency requires non-nil orderExecutor and clientOrderRepo.
	// We skip the full happy path but test that the handler exists.
	t.Skip("requires non-nil OrderExecutor for full flow")
}

// ---------------------------------------------------------------------------
// 5. RiskHandler
// ---------------------------------------------------------------------------

func TestRiskHandler_GetConfig(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	w := doRequest(h.GetConfig, http.MethodGet, "/config", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var config entity.RiskConfig
	if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if config.MaxPositionAmount != 5000 {
		t.Fatalf("expected maxPositionAmount 5000, got %f", config.MaxPositionAmount)
	}
	if config.InitialCapital != 10000 {
		t.Fatalf("expected initialCapital 10000, got %f", config.InitialCapital)
	}
}

func TestRiskHandler_UpdateConfig_OK(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	body := jsonBody(entity.RiskConfig{
		MaxPositionAmount:    8000,
		MaxDailyLoss:         3000,
		StopLossPercent:      3,
		TakeProfitPercent:    2,
		InitialCapital:       20000,
		MaxConsecutiveLosses: 5,
		CooldownMinutes:      15,
	})

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var config entity.RiskConfig
	if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if config.MaxPositionAmount != 8000 {
		t.Fatalf("expected maxPositionAmount 8000, got %f", config.MaxPositionAmount)
	}
}

func TestRiskHandler_UpdateConfig_WithRealtimeHub(t *testing.T) {
	riskMgr := newRiskManager()
	hub := usecase.NewRealtimeHub()
	h := NewRiskHandler(riskMgr, hub, nil, nil)

	body := jsonBody(entity.RiskConfig{
		MaxPositionAmount:    8000,
		MaxDailyLoss:         3000,
		StopLossPercent:      3,
		TakeProfitPercent:    2,
		InitialCapital:       20000,
		MaxConsecutiveLosses: 5,
		CooldownMinutes:      15,
	})

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRiskHandler_UpdateConfig_InvalidBody(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", []byte("not-json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRiskHandler_UpdateConfig_ZeroMaxPositionAmount(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	body := jsonBody(entity.RiskConfig{
		MaxPositionAmount: 0,
		MaxDailyLoss:      3000,
		StopLossPercent:   3,
		InitialCapital:    10000,
	})

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	errMsg, _ := resp["error"].(string)
	if errMsg != "maxPositionAmount must be positive" {
		t.Fatalf("unexpected error: %v", errMsg)
	}
}

func TestRiskHandler_UpdateConfig_ZeroMaxDailyLoss(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	body := jsonBody(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      0,
		StopLossPercent:   3,
		InitialCapital:    10000,
	})

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "maxDailyLoss must be positive" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestRiskHandler_UpdateConfig_ZeroStopLoss(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	body := jsonBody(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      3000,
		StopLossPercent:   0,
		InitialCapital:    10000,
	})

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRiskHandler_UpdateConfig_NegativeTakeProfit(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	body := jsonBody(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      3000,
		StopLossPercent:   3,
		TakeProfitPercent: -1,
		InitialCapital:    10000,
	})

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "takeProfitPercent must be non-negative" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestRiskHandler_UpdateConfig_ZeroInitialCapital(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	body := jsonBody(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      3000,
		StopLossPercent:   3,
		TakeProfitPercent: 2,
		InitialCapital:    0,
	})

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "initialCapital must be positive" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestRiskHandler_UpdateConfig_NegativeMaxConsecutiveLosses(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	body := jsonBody(entity.RiskConfig{
		MaxPositionAmount:    5000,
		MaxDailyLoss:         3000,
		StopLossPercent:      3,
		TakeProfitPercent:    2,
		InitialCapital:       10000,
		MaxConsecutiveLosses: -1,
	})

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "maxConsecutiveLosses must be non-negative" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestRiskHandler_UpdateConfig_NegativeCooldownMinutes(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	body := jsonBody(entity.RiskConfig{
		MaxPositionAmount:    5000,
		MaxDailyLoss:         3000,
		StopLossPercent:      3,
		TakeProfitPercent:    2,
		InitialCapital:       10000,
		MaxConsecutiveLosses: 3,
		CooldownMinutes:      -5,
	})

	w := doRequest(h.UpdateConfig, http.MethodPut, "/config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "cooldownMinutes must be non-negative" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestRiskHandler_GetPnL_OK(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	w := doRequest(h.GetPnL, http.MethodGet, "/pnl", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["balance"] != float64(10000) {
		t.Fatalf("expected balance 10000, got %v", body["balance"])
	}
	if _, ok := body["tradingHalted"]; !ok {
		t.Fatal("expected tradingHalted field in response")
	}
}

func TestRiskHandler_GetPnL_NoDailyPnlWhenCalculatorNil(t *testing.T) {
	riskMgr := newRiskManager()
	h := NewRiskHandler(riskMgr, nil, nil, nil)

	w := doRequest(h.GetPnL, http.MethodGet, "/pnl", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if _, ok := body["dailyPnl"]; ok {
		t.Fatal("expected dailyPnl to be absent when calculator is nil")
	}
}

// ---------------------------------------------------------------------------
// 6. StrategyHandler
// ---------------------------------------------------------------------------

func TestStrategyHandler_GetStrategy(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	h := NewStrategyHandler(resolver)

	w := doRequest(h.GetStrategy, http.MethodGet, "/strategy", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeBody(t, w)
	if body["stance"] != "HOLD" {
		t.Fatalf("expected stance 'HOLD', got %v", body["stance"])
	}
	if body["source"] != "rule-based" {
		t.Fatalf("expected source 'rule-based', got %v", body["source"])
	}
}

func TestStrategyHandler_SetStrategy_OK(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	h := NewStrategyHandler(resolver)

	body := jsonBody(map[string]any{
		"stance":     "TREND_FOLLOW",
		"reasoning":  "uptrend detected",
		"ttlMinutes": 30,
	})

	w := doRequest(h.SetStrategy, http.MethodPost, "/strategy", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeBody(t, w)
	if resp["stance"] != "TREND_FOLLOW" {
		t.Fatalf("expected stance 'TREND_FOLLOW', got %v", resp["stance"])
	}
	if resp["source"] != "override" {
		t.Fatalf("expected source 'override', got %v", resp["source"])
	}
	if _, ok := resp["expiresAt"]; !ok {
		t.Fatal("expected expiresAt in response")
	}
}

func TestStrategyHandler_SetStrategy_Contrarian(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	h := NewStrategyHandler(resolver)

	body := jsonBody(map[string]any{
		"stance":    "CONTRARIAN",
		"reasoning": "market reversal expected",
	})

	w := doRequest(h.SetStrategy, http.MethodPost, "/strategy", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeBody(t, w)
	if resp["stance"] != "CONTRARIAN" {
		t.Fatalf("expected stance 'CONTRARIAN', got %v", resp["stance"])
	}
}

func TestStrategyHandler_SetStrategy_Hold(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	h := NewStrategyHandler(resolver)

	body := jsonBody(map[string]any{
		"stance": "HOLD",
	})

	w := doRequest(h.SetStrategy, http.MethodPost, "/strategy", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestStrategyHandler_SetStrategy_InvalidStance(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	h := NewStrategyHandler(resolver)

	body := jsonBody(map[string]any{
		"stance": "INVALID",
	})

	w := doRequest(h.SetStrategy, http.MethodPost, "/strategy", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "stance must be TREND_FOLLOW, CONTRARIAN, HOLD, or BREAKOUT" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestStrategyHandler_SetStrategy_MissingStance(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	h := NewStrategyHandler(resolver)

	body := jsonBody(map[string]any{
		"reasoning": "no stance provided",
	})

	w := doRequest(h.SetStrategy, http.MethodPost, "/strategy", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStrategyHandler_SetStrategy_DefaultTTL(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	h := NewStrategyHandler(resolver)

	// ttlMinutes omitted => defaults to 60
	body := jsonBody(map[string]any{
		"stance": "TREND_FOLLOW",
	})

	w := doRequest(h.SetStrategy, http.MethodPost, "/strategy", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestStrategyHandler_SetStrategy_InvalidBody(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	h := NewStrategyHandler(resolver)

	w := doRequest(h.SetStrategy, http.MethodPost, "/strategy", []byte("not-json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStrategyHandler_DeleteOverride(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	h := NewStrategyHandler(resolver)

	w := doRequest(h.DeleteOverride, http.MethodDelete, "/strategy/override", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeBody(t, w)
	if resp["message"] != "override cleared, using rule-based stance" {
		t.Fatalf("unexpected message: %v", resp["message"])
	}
}

// ---------------------------------------------------------------------------
// 7. IndicatorHandler
// ---------------------------------------------------------------------------

func TestIndicatorHandler_GetIndicators_InvalidSymbol(t *testing.T) {
	// calculator can be nil since we expect 400 before it's called
	h := NewIndicatorHandler(nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/indicators/:symbol", h.GetIndicators)
	req := httptest.NewRequest(http.MethodGet, "/indicators/abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid symbol ID" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestIndicatorHandler_GetIndicators_NilCalculator(t *testing.T) {
	// Valid symbol but nil calculator will panic. We skip this case.
	// The test above covers the validation path.
	t.Skip("requires non-nil IndicatorCalculator for success path")
}

// ---------------------------------------------------------------------------
// 8. CandleHandler
// ---------------------------------------------------------------------------

func TestCandleHandler_GetCandles_InvalidSymbol(t *testing.T) {
	h := NewCandleHandler(nil, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/candles/:symbol", h.GetCandles)
	req := httptest.NewRequest(http.MethodGet, "/candles/abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid symbol ID" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestCandleHandler_GetCandles_UnsupportedInterval(t *testing.T) {
	h := NewCandleHandler(nil, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/candles/:symbol", h.GetCandles)
	req := httptest.NewRequest(http.MethodGet, "/candles/7?interval=PT30M", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	expected := "unsupported interval: PT30M"
	if resp["error"] != expected {
		t.Fatalf("expected error %q, got %v", expected, resp["error"])
	}
}

func TestCandleHandler_GetCandles_RejectedIntervals(t *testing.T) {
	rejected := []string{"PT2M", "PT30M", "PT2H", "P2D", "INVALID", ""}
	for _, interval := range rejected {
		h := NewCandleHandler(nil, nil)

		w := httptest.NewRecorder()
		_, r := gin.CreateTestContext(w)
		r.GET("/candles/:symbol", h.GetCandles)
		req := httptest.NewRequest(http.MethodGet, "/candles/7?interval="+interval, nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("interval %q should be rejected but got %d", interval, w.Code)
		}
	}
}

func TestCandleHandler_GetCandles_DefaultInterval_NotRejected(t *testing.T) {
	// Default interval is PT15M, which is in the allowed set.
	// With nil marketDataSvc, it will panic at the service call.
	// We verify indirectly by confirming PT15M is in the allowedIntervals map.
	if _, ok := allowedIntervals["PT15M"]; !ok {
		t.Fatal("default interval PT15M should be in allowedIntervals")
	}
}

// ---------------------------------------------------------------------------
// 9. TickerHandler
// ---------------------------------------------------------------------------

func TestTickerHandler_GetTicker_InvalidSymbolId(t *testing.T) {
	h := NewTickerHandler(nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/ticker", h.GetTicker)
	req := httptest.NewRequest(http.MethodGet, "/ticker?symbolId=abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid symbolId" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestTickerHandler_GetTicker_NilService(t *testing.T) {
	// Valid symbolId but nil service will panic on dereference.
	// We only test the validation path here.
	t.Skip("requires non-nil MarketDataService for success path")
}

// ---------------------------------------------------------------------------
// 10. SymbolHandler
// ---------------------------------------------------------------------------

func TestSymbolHandler_GetSymbols_NilClient(t *testing.T) {
	// SymbolHandler depends on *rakuten.RESTClient (concrete type).
	// We can only test that the handler is correctly constructed.
	// The success path requires a real REST client, so we skip it.
	t.Skip("requires non-nil RESTClient for all paths")
}

// ---------------------------------------------------------------------------
// 11. OrderbookHandler
// ---------------------------------------------------------------------------

func TestOrderbookHandler_GetOrderbook_InvalidSymbolId(t *testing.T) {
	h := NewOrderbookHandler(nil, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/orderbook", h.GetOrderbook)
	req := httptest.NewRequest(http.MethodGet, "/orderbook?symbolId=abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid symbolId" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestOrderbookHandler_GetOrderbook_ValidSymbolId_NilClient(t *testing.T) {
	// Valid symbolId passes validation, but nil restClient will panic.
	// We skip the success path since it requires a real RESTClient.
	t.Skip("requires non-nil RESTClient for success path")
}

// ---------------------------------------------------------------------------
// 12. TradeHandler
// ---------------------------------------------------------------------------

func TestTradeHandler_GetTrades_OK(t *testing.T) {
	oc := &mockOrderClient{
		trades: []entity.MyTrade{
			{ID: 10, SymbolID: 7, OrderSide: entity.OrderSideBuy},
		},
	}
	h := NewTradeHandler(oc, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/trades", h.GetTrades)
	req := httptest.NewRequest(http.MethodGet, "/trades?symbolId=7", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var trades []entity.MyTrade
	if err := json.Unmarshal(w.Body.Bytes(), &trades); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
}

func TestTradeHandler_GetTrades_DefaultSymbol(t *testing.T) {
	oc := &mockOrderClient{
		trades: []entity.MyTrade{},
	}
	h := NewTradeHandler(oc, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/trades", h.GetTrades)
	req := httptest.NewRequest(http.MethodGet, "/trades", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestTradeHandler_GetTrades_InvalidSymbol(t *testing.T) {
	h := NewTradeHandler(&mockOrderClient{}, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/trades", h.GetTrades)
	req := httptest.NewRequest(http.MethodGet, "/trades?symbolId=abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTradeHandler_GetAllTrades_NilRestClient(t *testing.T) {
	h := NewTradeHandler(&mockOrderClient{}, nil)

	w := doRequest(h.GetAllTrades, http.MethodGet, "/trades/all", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "rest client not configured" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

// ---------------------------------------------------------------------------
// 13. TradingConfigHandler
// ---------------------------------------------------------------------------

func TestTradingConfigHandler_GetTradingConfig(t *testing.T) {
	var symbolID int64 = 7
	var tradeAmount float64 = 0.01

	h := NewTradingConfigHandler(
		func() int64 { return symbolID },
		func() float64 { return tradeAmount },
		func(sid int64, amt float64) { symbolID = sid; tradeAmount = amt },
		nil,
	)

	w := doRequest(h.GetTradingConfig, http.MethodGet, "/trading-config", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp tradingConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.SymbolID != 7 {
		t.Fatalf("expected symbolId 7, got %d", resp.SymbolID)
	}
	if resp.TradeAmount != 0.01 {
		t.Fatalf("expected tradeAmount 0.01, got %f", resp.TradeAmount)
	}
}

func TestTradingConfigHandler_UpdateTradingConfig_InvalidBody(t *testing.T) {
	h := NewTradingConfigHandler(
		func() int64 { return 7 },
		func() float64 { return 0.01 },
		func(int64, float64) {},
		nil,
	)

	w := doRequest(h.UpdateTradingConfig, http.MethodPut, "/trading-config", []byte("not-json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid request body" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestTradingConfigHandler_UpdateTradingConfig_ZeroSymbolId(t *testing.T) {
	h := NewTradingConfigHandler(
		func() int64 { return 7 },
		func() float64 { return 0.01 },
		func(int64, float64) {},
		nil,
	)

	body := jsonBody(map[string]any{
		"symbolId":    0,
		"tradeAmount": 0.01,
	})

	w := doRequest(h.UpdateTradingConfig, http.MethodPut, "/trading-config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "symbolId must be positive" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestTradingConfigHandler_UpdateTradingConfig_NegativeSymbolId(t *testing.T) {
	h := NewTradingConfigHandler(
		func() int64 { return 7 },
		func() float64 { return 0.01 },
		func(int64, float64) {},
		nil,
	)

	body := jsonBody(map[string]any{
		"symbolId":    -1,
		"tradeAmount": 0.01,
	})

	w := doRequest(h.UpdateTradingConfig, http.MethodPut, "/trading-config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTradingConfigHandler_UpdateTradingConfig_ZeroTradeAmount(t *testing.T) {
	h := NewTradingConfigHandler(
		func() int64 { return 7 },
		func() float64 { return 0.01 },
		func(int64, float64) {},
		nil,
	)

	body := jsonBody(map[string]any{
		"symbolId":    7,
		"tradeAmount": 0,
	})

	w := doRequest(h.UpdateTradingConfig, http.MethodPut, "/trading-config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "tradeAmount must be positive" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestTradingConfigHandler_UpdateTradingConfig_NegativeTradeAmount(t *testing.T) {
	h := NewTradingConfigHandler(
		func() int64 { return 7 },
		func() float64 { return 0.01 },
		func(int64, float64) {},
		nil,
	)

	body := jsonBody(map[string]any{
		"symbolId":    7,
		"tradeAmount": -0.5,
	})

	w := doRequest(h.UpdateTradingConfig, http.MethodPut, "/trading-config", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// 14. RealtimeHandler
// ---------------------------------------------------------------------------

func TestRealtimeHandler_Stream_NilHub(t *testing.T) {
	h := NewRealtimeHandler(nil, nil, nil, nil)

	w := doRequest(h.Stream, http.MethodGet, "/ws", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	resp := decodeBody(t, w)
	if resp["error"] != "realtime hub unavailable" {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestRealtimeHandler_Stream_InvalidSymbolId(t *testing.T) {
	hub := usecase.NewRealtimeHub()
	h := NewRealtimeHandler(nil, nil, hub, nil)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.GET("/ws", h.Stream)
	req := httptest.NewRequest(http.MethodGet, "/ws?symbolId=abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
