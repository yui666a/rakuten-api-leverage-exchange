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
		w.Write([]byte(`[{"currency":"JPY","onhandAmount":10000}]`))
	}))
	defer server.Close()
	client := NewRESTClient(server.URL, "key", "secret")
	assets, err := client.GetAssets(context.Background())
	if err != nil { t.Fatalf("unexpected error: %v", err) }
	if len(assets) != 1 { t.Fatalf("expected 1 asset, got %d", len(assets)) }
	if assets[0].OnhandAmount != 10000 { t.Fatalf("expected 10000, got %f", assets[0].OnhandAmount) }
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
}

func assertAuthHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("API-KEY") == "" { t.Fatal("API-KEY header missing") }
	if r.Header.Get("NONCE") == "" { t.Fatal("NONCE header missing") }
	if r.Header.Get("SIGNATURE") == "" { t.Fatal("SIGNATURE header missing") }
}
