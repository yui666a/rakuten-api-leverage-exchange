package rakuten

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func (c *RESTClient) GetAssets(ctx context.Context) ([]entity.Asset, error) {
	body, err := c.DoPrivate(ctx, "GET", "/api/v1/asset", "", nil)
	if err != nil { return nil, fmt.Errorf("GetAssets: %w", err) }
	var assets []entity.Asset
	if err := json.Unmarshal(body, &assets); err != nil { return nil, fmt.Errorf("GetAssets unmarshal: %w", err) }
	return assets, nil
}

func (c *RESTClient) GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPrivate(ctx, "GET", "/api/v1/cfd/position", query, nil)
	if err != nil { return nil, fmt.Errorf("GetPositions: %w", err) }
	var positions []entity.Position
	if err := json.Unmarshal(body, &positions); err != nil { return nil, fmt.Errorf("GetPositions unmarshal: %w", err) }
	return positions, nil
}

func (c *RESTClient) GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPrivate(ctx, "GET", "/api/v1/cfd/order", query, nil)
	if err != nil { return nil, fmt.Errorf("GetOrders: %w", err) }
	var orders []entity.Order
	if err := json.Unmarshal(body, &orders); err != nil { return nil, fmt.Errorf("GetOrders unmarshal: %w", err) }
	return orders, nil
}

func (c *RESTClient) CreateOrder(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
	reqBody, err := json.Marshal(req)
	if err != nil { return nil, fmt.Errorf("CreateOrder marshal: %w", err) }
	body, err := c.DoPrivate(ctx, "POST", "/api/v1/cfd/order", "", reqBody)
	if err != nil { return nil, fmt.Errorf("CreateOrder: %w", err) }
	var orders []entity.Order
	if err := json.Unmarshal(body, &orders); err != nil { return nil, fmt.Errorf("CreateOrder unmarshal: %w", err) }
	return orders, nil
}

func (c *RESTClient) CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error) {
	query := fmt.Sprintf("symbolId=%d&id=%d", symbolID, orderID)
	body, err := c.DoPrivate(ctx, "DELETE", "/api/v1/cfd/order", query, nil)
	if err != nil { return nil, fmt.Errorf("CancelOrder: %w", err) }
	var orders []entity.Order
	if err := json.Unmarshal(body, &orders); err != nil { return nil, fmt.Errorf("CancelOrder unmarshal: %w", err) }
	return orders, nil
}

func (c *RESTClient) GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPrivate(ctx, "GET", "/api/v1/cfd/trade", query, nil)
	if err != nil { return nil, fmt.Errorf("GetMyTrades: %w", err) }
	var trades []entity.MyTrade
	if err := json.Unmarshal(body, &trades); err != nil { return nil, fmt.Errorf("GetMyTrades unmarshal: %w", err) }
	return trades, nil
}
