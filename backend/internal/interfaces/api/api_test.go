package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func setupRouter() *httptest.Server {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	llmSvc := usecase.NewLLMService(nil, 15*time.Minute)

	deps := Dependencies{
		RiskManager:         riskMgr,
		LLMService:          llmSvc,
		IndicatorCalculator: nil,
		OrderClient:         &mockOrderClient{},
	}

	router := NewRouter(deps)
	return httptest.NewServer(router)
}

func TestGetStatus(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "running" {
		t.Fatalf("expected status 'running', got %v", body["status"])
	}
}

func TestGetConfig(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/config")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var config entity.RiskConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if config.MaxPositionAmount != 5000 {
		t.Fatalf("expected maxPositionAmount 5000, got %f", config.MaxPositionAmount)
	}
}

func TestUpdateConfig(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	newConfig := entity.RiskConfig{
		MaxPositionAmount: 8000,
		MaxDailyLoss:      3000,
		StopLossPercent:   3,
		InitialCapital:    10000,
	}
	body, _ := json.Marshal(newConfig)

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	resp2, err := http.Get(ts.URL + "/api/v1/config")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()

	var updated entity.RiskConfig
	json.NewDecoder(resp2.Body).Decode(&updated)
	if updated.MaxPositionAmount != 8000 {
		t.Fatalf("expected updated maxPositionAmount 8000, got %f", updated.MaxPositionAmount)
	}
}

func TestGetPnL(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/pnl")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["balance"] != float64(10000) {
		t.Fatalf("expected balance 10000, got %v", body["balance"])
	}
}

func TestBotStartStop(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	stopReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/stop", nil)
	stopResp, err := http.DefaultClient.Do(stopReq)
	if err != nil {
		t.Fatalf("stop request failed: %v", err)
	}
	defer stopResp.Body.Close()

	if stopResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from stop, got %d", stopResp.StatusCode)
	}

	statusResp, err := http.Get(ts.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("status request failed: %v", err)
	}
	defer statusResp.Body.Close()

	var statusBody map[string]interface{}
	if err := json.NewDecoder(statusResp.Body).Decode(&statusBody); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}
	if statusBody["status"] != "stopped" {
		t.Fatalf("expected stopped status, got %v", statusBody["status"])
	}

	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/start", nil)
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start request failed: %v", err)
	}
	defer startResp.Body.Close()

	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from start, got %d", startResp.StatusCode)
	}
}

func TestGetTrades(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/trades?symbolId=7")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var trades []entity.MyTrade
	if err := json.NewDecoder(resp.Body).Decode(&trades); err != nil {
		t.Fatalf("failed to decode trades: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
}

func TestGetStrategy_NoCached(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/strategy")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["stance"] != "NONE" {
		t.Fatalf("expected stance NONE, got %v", body["stance"])
	}
}

func TestGetIndicators_InvalidSymbol(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/indicators/abc")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
