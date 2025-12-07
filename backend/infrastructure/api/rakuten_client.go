package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/domain"
)

// RakutenClient handles communication with Rakuten API
type RakutenClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewRakutenClient creates a new Rakuten API client
func NewRakutenClient(baseURL, apiKey string) *RakutenClient {
	return &RakutenClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetMarket implements MarketRepository interface
func (c *RakutenClient) GetMarket(ctx context.Context, symbol string) (*domain.Market, error) {
	// Mock implementation for demonstration
	// In production, this would make actual API calls to Rakuten
	return &domain.Market{
		Symbol:    symbol,
		LastPrice: 150.50,
		Volume:    1000000,
		Change24h: 2.5,
		High24h:   152.00,
		Low24h:    148.00,
		UpdatedAt: time.Now(),
	}, nil
}

// GetAllMarkets implements MarketRepository interface
func (c *RakutenClient) GetAllMarkets(ctx context.Context) ([]domain.Market, error) {
	// Mock response for demonstration
	// In production, this would make actual API calls to Rakuten
	markets := []domain.Market{
		{
			Symbol:    "BTC/JPY",
			LastPrice: 5000000,
			Volume:    100,
			Change24h: 3.2,
			High24h:   5100000,
			Low24h:    4900000,
			UpdatedAt: time.Now(),
		},
		{
			Symbol:    "ETH/JPY",
			LastPrice: 300000,
			Volume:    500,
			Change24h: -1.5,
			High24h:   310000,
			Low24h:    295000,
			UpdatedAt: time.Now(),
		},
	}

	return markets, nil
}

// makeRequest is a helper method for making HTTP requests
func (c *RakutenClient) makeRequest(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	url := fmt.Sprintf("%s%s", c.baseURL, path)
	
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	return respBody, nil
}

// parseJSON is a helper to parse JSON responses
func parseJSON(data []byte, v interface{}) error {
	if err := json.Unmarshal(data, v); err != nil {
		return errors.New("failed to parse JSON response")
	}
	return nil
}
