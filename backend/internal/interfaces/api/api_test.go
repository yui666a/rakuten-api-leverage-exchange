package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

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

	resp2, _ := http.Get(ts.URL + "/api/v1/config")
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
