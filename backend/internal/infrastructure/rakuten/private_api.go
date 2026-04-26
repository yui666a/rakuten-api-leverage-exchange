package rakuten

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
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
	// 発注は PriorityHigh で参照系のキューを追い越す。
	body, err := c.DoPrivateWithPriority(ctx, "POST", "/api/v1/cfd/order", "", reqBody, PriorityHigh)
	if err != nil { return nil, fmt.Errorf("CreateOrder: %w", err) }
	var orders []entity.Order
	if err := json.Unmarshal(body, &orders); err != nil { return nil, fmt.Errorf("CreateOrder unmarshal: %w", err) }
	return orders, nil
}

// CreateOrderRaw は CreateOrder の構造化版。
// 送信前 / トランスポート失敗 / 非 2xx / パース失敗 / 成功 を区別して返す。
//
// CreateOrderOutcome の各フィールドの意味:
//   - TransportError != nil: 送信前 or HTTP コネクション系の失敗 (RawResponse 空, HTTPStatus=0)
//   - HTTPStatus < 200 or >= 300: 楽天が非 2xx を返した。Orders は nil。
//     ParseError != nil なら本文が JSON でない (= submitted 候補)
//     ParseError == nil なら 4xx/5xx の構造化エラーボディが取れている (= failed 候補)
//   - HTTPStatus 2xx かつ ParseError == nil: 成功 (Orders 非 nil)
//   - HTTPStatus 2xx かつ ParseError != nil: 200 だが本文が解釈不能 (= submitted)
func (c *RESTClient) CreateOrderRaw(ctx context.Context, req entity.OrderRequest) (repository.CreateOrderOutcome, error) {
	out := repository.CreateOrderOutcome{}

	reqBody, err := json.Marshal(req)
	if err != nil {
		out.TransportError = fmt.Errorf("CreateOrderRaw marshal: %w", err)
		return out, out.TransportError
	}

	// 発注は PriorityHigh で参照系のキューを追い越す。
	statusCode, body, transportErr := c.DoPrivateRawWithPriority(ctx, "POST", "/api/v1/cfd/order", "", reqBody, PriorityHigh)
	out.HTTPStatus = statusCode
	out.RawResponse = body
	if transportErr != nil {
		out.TransportError = transportErr
		return out, nil
	}

	if statusCode < 200 || statusCode >= 300 {
		// 4xx/5xx。本文を JSON としてパース可能か試す (エラー構造体である可能性あり)。
		var probe any
		if err := json.Unmarshal(body, &probe); err != nil {
			out.ParseError = fmt.Errorf("CreateOrderRaw error body parse failed (status %d): %w", statusCode, err)
		}
		out.HTTPError = fmt.Errorf("API error (status %d): %s", statusCode, string(body))
		return out, nil
	}

	var orders []entity.Order
	if err := json.Unmarshal(body, &orders); err != nil {
		out.ParseError = fmt.Errorf("CreateOrderRaw unmarshal: %w", err)
		return out, nil
	}
	out.Orders = orders
	return out, nil
}

func (c *RESTClient) CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error) {
	query := fmt.Sprintf("symbolId=%d&id=%d", symbolID, orderID)
	// キャンセルも発注経路 (post-only エスカレ等) で時間に敏感なので high。
	body, err := c.DoPrivateWithPriority(ctx, "DELETE", "/api/v1/cfd/order", query, nil, PriorityHigh)
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
