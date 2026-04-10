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

type rawOrderbookEntry struct {
	Price  entity.StringFloat64 `json:"price"`
	Amount entity.StringFloat64 `json:"amount"`
}

type rawOrderbook struct {
	SymbolID  int64                `json:"symbolId"`
	Asks      []rawOrderbookEntry  `json:"asks"`
	Bids      []rawOrderbookEntry  `json:"bids"`
	BestAsk   entity.StringFloat64 `json:"bestAsk"`
	BestBid   entity.StringFloat64 `json:"bestBid"`
	MidPrice  entity.StringFloat64 `json:"midPrice"`
	Spread    entity.StringFloat64 `json:"spread"`
	Timestamp int64                `json:"timestamp"`
}

func (c *RESTClient) GetOrderbook(ctx context.Context, symbolID int64) (*entity.Orderbook, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPublic(ctx, "GET", "/api/v1/orderbook", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetOrderbook: %w", err)
	}

	var raw rawOrderbook
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("GetOrderbook unmarshal: %w", err)
	}

	asks := make([]entity.OrderbookEntry, len(raw.Asks))
	for i, a := range raw.Asks {
		asks[i] = entity.OrderbookEntry{Price: a.Price.Float64(), Amount: a.Amount.Float64()}
	}
	bids := make([]entity.OrderbookEntry, len(raw.Bids))
	for i, b := range raw.Bids {
		bids[i] = entity.OrderbookEntry{Price: b.Price.Float64(), Amount: b.Amount.Float64()}
	}

	return &entity.Orderbook{
		SymbolID:  raw.SymbolID,
		Asks:      asks,
		Bids:      bids,
		BestAsk:   raw.BestAsk.Float64(),
		BestBid:   raw.BestBid.Float64(),
		MidPrice:  raw.MidPrice.Float64(),
		Spread:    raw.Spread.Float64(),
		Timestamp: raw.Timestamp,
	}, nil
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
