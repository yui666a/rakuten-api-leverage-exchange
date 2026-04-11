package rakuten

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestGetAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"currency":"JPY","onhandAmount":"10000"}]`))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	assets, err := client.GetAssets(context.Background())
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	if len(assets) != 1 { t.Fatalf("expected 1 asset, got %d", len(assets)) }
	if assets[0].OnhandAmount != "10000" { t.Fatalf("expected 10000, got %s", assets[0].OnhandAmount) }
}

func TestGetPositions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":1,"symbolId":7,"positionStatus":"OPEN","orderSide":"BUY","price":5000000,"amount":0.001,"remainingAmount":0.001,"leverage":2,"floatingProfit":100,"profit":0,"bestPrice":5000100,"orderId":10,"createdAt":1700000000000}]`))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	positions, err := client.GetPositions(context.Background(), 7)
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	if len(positions) != 1 { t.Fatalf("expected 1 position, got %d", len(positions)) }
	if positions[0].PositionStatus != entity.PositionStatusOpen { t.Fatalf("expected OPEN, got %s", positions[0].PositionStatus) }
}

func TestCreateOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		if r.Method != "POST" { t.Fatalf("expected POST, got %s", r.Method) }
		body, _ := io.ReadAll(r.Body)
		var req entity.OrderRequest
		if err := json.Unmarshal(body, &req); err != nil { t.Fatalf("failed to unmarshal request: %v", err) }
		if req.SymbolID != 7 { t.Fatalf("expected symbolId 7, got %d", req.SymbolID) }
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":100,"symbolId":7,"orderBehavior":"OPEN","orderSide":"BUY","orderPattern":"NORMAL","orderType":"MARKET","price":0,"amount":0.001,"remainingAmount":0.001,"orderStatus":"WORKING_ORDER","leverage":2,"orderCreatedAt":1700000000000}]`))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	orders, err := client.CreateOrder(context.Background(), entity.OrderRequest{
		SymbolID: 7, OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{OrderBehavior: entity.OrderBehaviorOpen, OrderSide: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket, Amount: 0.001},
	})
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	if len(orders) != 1 { t.Fatalf("expected 1 order, got %d", len(orders)) }
	if orders[0].ID != 100 { t.Fatalf("expected order ID 100, got %d", orders[0].ID) }
}

func TestCreateOrderRaw_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":100,"symbolId":7,"orderBehavior":"OPEN","orderSide":"BUY","orderPattern":"NORMAL","orderType":"MARKET","price":0,"amount":0.001,"remainingAmount":0.001,"orderStatus":"WORKING_ORDER","leverage":2,"orderCreatedAt":1700000000000}]`))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	out, err := client.CreateOrderRaw(context.Background(), entity.OrderRequest{
		SymbolID: 7, OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{OrderBehavior: entity.OrderBehaviorOpen, OrderSide: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket, Amount: 0.001},
	})
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	if out.HTTPStatus != 200 { t.Fatalf("expected status 200, got %d", out.HTTPStatus) }
	if out.TransportError != nil || out.ParseError != nil || out.HTTPError != nil {
		t.Fatalf("unexpected errors: %+v", out)
	}
	if len(out.Orders) != 1 || out.Orders[0].ID != 100 { t.Fatalf("expected 1 order with ID 100, got %+v", out.Orders) }
	if len(out.RawResponse) == 0 { t.Fatal("RawResponse should be populated") }
}

func TestCreateOrderRaw_ParseFailureKeepsRawBody(t *testing.T) {
	// 200 OK だが本文が解釈不能 (= submitted 候補) を再現する。
	garbage := `not a json array`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(garbage))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	out, err := client.CreateOrderRaw(context.Background(), entity.OrderRequest{
		SymbolID: 7, OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{OrderBehavior: entity.OrderBehaviorOpen, OrderSide: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket, Amount: 0.001},
	})
	if err != nil { t.Fatalf("unexpected transport-level error: %v", err) }
	if out.HTTPStatus != 200 { t.Fatalf("expected status 200, got %d", out.HTTPStatus) }
	if out.ParseError == nil { t.Fatal("expected ParseError for unparseable body") }
	if string(out.RawResponse) != garbage { t.Fatalf("expected raw body preserved, got %q", string(out.RawResponse)) }
	if len(out.Orders) != 0 { t.Fatalf("expected no parsed orders, got %d", len(out.Orders)) }
}

func TestCreateOrderRaw_HTTP4xxWithJSONBody(t *testing.T) {
	// 4xx + 構造化エラーボディ (= failed)。ParseError が nil になり、HTTPError がセットされる。
	body := `{"code":40001,"message":"insufficient balance"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(body))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	out, err := client.CreateOrderRaw(context.Background(), entity.OrderRequest{
		SymbolID: 7, OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{OrderBehavior: entity.OrderBehaviorOpen, OrderSide: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket, Amount: 0.001},
	})
	if err != nil { t.Fatalf("unexpected transport-level error: %v", err) }
	if out.HTTPStatus != 400 { t.Fatalf("expected status 400, got %d", out.HTTPStatus) }
	if out.HTTPError == nil { t.Fatal("expected HTTPError for 4xx") }
	if out.ParseError != nil { t.Fatalf("4xx with parseable JSON body should not set ParseError, got %v", out.ParseError) }
	if string(out.RawResponse) != body { t.Fatalf("raw body mismatch: %s", string(out.RawResponse)) }
}

func TestCreateOrderRaw_HTTP5xxWithGarbage(t *testing.T) {
	// 5xx + 解釈不能ボディ (= submitted 候補)。ParseError と HTTPError の両方がセットされる。
	body := `<html>internal server error</html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(body))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	out, err := client.CreateOrderRaw(context.Background(), entity.OrderRequest{
		SymbolID: 7, OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{OrderBehavior: entity.OrderBehaviorOpen, OrderSide: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket, Amount: 0.001},
	})
	if err != nil { t.Fatalf("unexpected transport-level error: %v", err) }
	if out.HTTPStatus != 500 { t.Fatalf("expected status 500, got %d", out.HTTPStatus) }
	if out.HTTPError == nil { t.Fatal("expected HTTPError for 5xx") }
	if out.ParseError == nil { t.Fatal("expected ParseError for unparseable body") }
	if string(out.RawResponse) != body { t.Fatalf("raw body mismatch") }
}

func TestCancelOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		if r.Method != "DELETE" { t.Fatalf("expected DELETE, got %s", r.Method) }
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":100,"symbolId":7,"orderStatus":"WORKING_ORDER"}]`))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	orders, err := client.CancelOrder(context.Background(), 7, 100)
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	if len(orders) != 1 { t.Fatalf("expected 1 order, got %d", len(orders)) }
}

func TestGetMyTrades(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":1,"symbolId":7,"orderSide":"BUY","price":5000000,"amount":0.001,"profit":0,"fee":5,"positionFee":0,"closeTradeProfit":0,"orderId":100,"positionId":1,"createdAt":1700000000000}]`))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	trades, err := client.GetMyTrades(context.Background(), 7)
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	if len(trades) != 1 { t.Fatalf("expected 1 trade, got %d", len(trades)) }
	if trades[0].Price.Float64() != 5000000 { t.Fatalf("expected price 5000000, got %v", trades[0].Price.Float64()) }
}

// TestGetMyTrades_StringNumericFields は楽天 LTC_JPY 等で観測されている
// 数値フィールドが string で返ってくるケースで unmarshal が通ることを保証する。
func TestGetMyTrades_StringNumericFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":2,"symbolId":10,"orderSide":"BUY","price":"12345.6","amount":"0.5","profit":"10.5","fee":"1.2","positionFee":"0","closeTradeProfit":"0","orderId":200,"positionId":2,"createdAt":1700000000000}]`))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	trades, err := client.GetMyTrades(context.Background(), 10)
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	if len(trades) != 1 { t.Fatalf("expected 1 trade, got %d", len(trades)) }
	if trades[0].Price.Float64() != 12345.6 { t.Fatalf("expected price 12345.6, got %v", trades[0].Price.Float64()) }
	if trades[0].Amount.Float64() != 0.5 { t.Fatalf("expected amount 0.5, got %v", trades[0].Amount.Float64()) }
	if trades[0].Profit.Float64() != 10.5 { t.Fatalf("expected profit 10.5, got %v", trades[0].Profit.Float64()) }
	if trades[0].Fee.Float64() != 1.2 { t.Fatalf("expected fee 1.2, got %v", trades[0].Fee.Float64()) }
}

func assertAuthHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("API-KEY") == "" { t.Fatal("API-KEY header missing") }
	if r.Header.Get("NONCE") == "" { t.Fatal("NONCE header missing") }
	if r.Header.Get("SIGNATURE") == "" { t.Fatal("SIGNATURE header missing") }
}
