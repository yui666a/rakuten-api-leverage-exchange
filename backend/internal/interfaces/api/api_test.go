package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type mockOrderClient struct{}

func (m *mockOrderClient) CreateOrder(_ context.Context, _ entity.OrderRequest) ([]entity.Order, error) {
	return nil, nil
}

func (m *mockOrderClient) CancelOrder(_ context.Context, _, _ int64) ([]entity.Order, error) {
	return nil, nil
}

func (m *mockOrderClient) GetOrders(_ context.Context, _ int64) ([]entity.Order, error) {
	return nil, nil
}

func (m *mockOrderClient) GetPositions(_ context.Context, _ int64) ([]entity.Position, error) {
	return []entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 4500000, RemainingAmount: 0.01, FloatingProfit: 1200},
	}, nil
}

func (m *mockOrderClient) GetMyTrades(_ context.Context, _ int64) ([]entity.MyTrade, error) {
	return []entity.MyTrade{
		{ID: 10, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 4400000, Amount: 0.01, Profit: 900, Fee: 10, CreatedAt: 1700000000000},
	}, nil
}

func (m *mockOrderClient) GetAssets(_ context.Context) ([]entity.Asset, error) {
	return []entity.Asset{
		{Currency: "JPY", OnhandAmount: "10000"},
	}, nil
}

func init() {
	gin.SetMode(gin.TestMode)
}

func setupRouter() *gin.Engine {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	stanceResolver := usecase.NewRuleBasedStanceResolver(nil)

	deps := Dependencies{
		RiskManager:         riskMgr,
		StanceResolver:      stanceResolver,
		IndicatorCalculator: nil,
		OrderClient:         &mockOrderClient{},
	}

	return NewRouter(deps)
}

func doRequest(router *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestGetStatus(t *testing.T) {
	router := setupRouter()
	w := doRequest(router, "GET", "/api/v1/status", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "running" {
		t.Fatalf("expected status 'running', got %v", body["status"])
	}
}

func TestGetConfig(t *testing.T) {
	router := setupRouter()
	w := doRequest(router, "GET", "/api/v1/config", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var config entity.RiskConfig
	if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if config.MaxPositionAmount != 5000 {
		t.Fatalf("expected maxPositionAmount 5000, got %f", config.MaxPositionAmount)
	}
}

func TestUpdateConfig(t *testing.T) {
	router := setupRouter()

	newConfig := entity.RiskConfig{
		MaxPositionAmount: 8000,
		MaxDailyLoss:      3000,
		StopLossPercent:   3,
		InitialCapital:    10000,
	}
	body, _ := json.Marshal(newConfig)

	w := doRequest(router, "PUT", "/api/v1/config", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	w2 := doRequest(router, "GET", "/api/v1/config", nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}

	var updated entity.RiskConfig
	json.Unmarshal(w2.Body.Bytes(), &updated)
	if updated.MaxPositionAmount != 8000 {
		t.Fatalf("expected updated maxPositionAmount 8000, got %f", updated.MaxPositionAmount)
	}
}

func TestGetPnL(t *testing.T) {
	router := setupRouter()
	w := doRequest(router, "GET", "/api/v1/pnl", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["balance"] != float64(10000) {
		t.Fatalf("expected balance 10000, got %v", body["balance"])
	}
}

func TestBotStartStop(t *testing.T) {
	router := setupRouter()

	w := doRequest(router, "POST", "/api/v1/stop", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from stop, got %d", w.Code)
	}

	w2 := doRequest(router, "GET", "/api/v1/status", nil)
	var statusBody map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &statusBody); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}
	if statusBody["status"] != "stopped" {
		t.Fatalf("expected stopped status, got %v", statusBody["status"])
	}

	w3 := doRequest(router, "POST", "/api/v1/start", nil)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 from start, got %d", w3.Code)
	}
}

func TestGetTrades(t *testing.T) {
	router := setupRouter()
	w := doRequest(router, "GET", "/api/v1/trades?symbolId=7", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var trades []entity.MyTrade
	if err := json.Unmarshal(w.Body.Bytes(), &trades); err != nil {
		t.Fatalf("failed to decode trades: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
}

func TestGetStrategy_RuleBased(t *testing.T) {
	router := setupRouter()
	w := doRequest(router, "GET", "/api/v1/strategy", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["stance"] != "HOLD" {
		t.Fatalf("expected stance HOLD (insufficient indicators), got %v", body["stance"])
	}
	if body["source"] != "rule-based" {
		t.Fatalf("expected source 'rule-based', got %v", body["source"])
	}
}

func TestGetIndicators_InvalidSymbol(t *testing.T) {
	router := setupRouter()
	w := doRequest(router, "GET", "/api/v1/indicators/abc", nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
