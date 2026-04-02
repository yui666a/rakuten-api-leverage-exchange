package rakuten

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func (c *RESTClient) GetSymbols(ctx context.Context) ([]entity.Symbol, error) {
	body, err := c.DoPublic(ctx, "GET", "/api/v1/cfd/symbol", "", nil)
	if err != nil {
		return nil, fmt.Errorf("GetSymbols: %w", err)
	}

	var symbols []entity.Symbol
	if err := json.Unmarshal(body, &symbols); err != nil {
		return nil, fmt.Errorf("GetSymbols unmarshal: %w", err)
	}
	return symbols, nil
}

func (c *RESTClient) GetTicker(ctx context.Context, symbolID int64) (*entity.Ticker, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPublic(ctx, "GET", "/api/v1/ticker", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetTicker: %w", err)
	}

	var ticker entity.Ticker
	if err := json.Unmarshal(body, &ticker); err != nil {
		return nil, fmt.Errorf("GetTicker unmarshal: %w", err)
	}
	return &ticker, nil
}

func (c *RESTClient) GetOrderbook(ctx context.Context, symbolID int64) (*entity.Orderbook, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPublic(ctx, "GET", "/api/v1/orderbook", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetOrderbook: %w", err)
	}

	var ob entity.Orderbook
	if err := json.Unmarshal(body, &ob); err != nil {
		return nil, fmt.Errorf("GetOrderbook unmarshal: %w", err)
	}
	return &ob, nil
}

func (c *RESTClient) GetCandlestick(ctx context.Context, symbolID int64, candlestickType string, dateFrom, dateTo *int64) (*entity.CandlestickResponse, error) {
	query := fmt.Sprintf("symbolId=%d&candlestickType=%s", symbolID, candlestickType)
	if dateFrom != nil {
		query += fmt.Sprintf("&dateFrom=%d", *dateFrom)
	}
	if dateTo != nil {
		query += fmt.Sprintf("&dateTo=%d", *dateTo)
	}

	body, err := c.DoPublic(ctx, "GET", "/api/v1/candlestick", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetCandlestick: %w", err)
	}

	var resp entity.CandlestickResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("GetCandlestick unmarshal: %w", err)
	}
	return &resp, nil
}

func (c *RESTClient) GetTrades(ctx context.Context, symbolID int64) (*entity.MarketTradesResponse, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPublic(ctx, "GET", "/api/v1/trades", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetTrades: %w", err)
	}

	var resp entity.MarketTradesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("GetTrades unmarshal: %w", err)
	}
	return &resp, nil
}
