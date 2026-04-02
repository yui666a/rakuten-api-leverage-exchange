package rakuten

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSymbols(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":7,"currencyPair":"BTC_JPY","baseCurrency":"BTC","quoteCurrency":"JPY","minOrderAmount":0.0001,"maxOrderAmount":5,"enabled":true}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	symbols, err := client.GetSymbols(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].ID != 7 {
		t.Fatalf("expected symbol ID 7, got %d", symbols[0].ID)
	}
	if symbols[0].CurrencyPair != "BTC_JPY" {
		t.Fatalf("expected BTC_JPY, got %s", symbols[0].CurrencyPair)
	}
}

func TestGetTicker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbolId") != "7" {
			t.Fatalf("expected symbolId=7, got %s", r.URL.Query().Get("symbolId"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"bestAsk":5000100,"bestBid":5000000,"open":4900000,"high":5100000,"low":4800000,"last":5000050,"volume":123.45,"timestamp":1700000000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ticker, err := client.GetTicker(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ticker.SymbolID != 7 {
		t.Fatalf("expected symbolId 7, got %d", ticker.SymbolID)
	}
	if ticker.Last != 5000050 {
		t.Fatalf("expected last 5000050, got %f", ticker.Last)
	}
}

func TestGetOrderbook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"asks":[{"price":5000100,"amount":0.5}],"bids":[{"price":5000000,"amount":1.0}],"bestAsk":5000100,"bestBid":5000000,"midPrice":5000050,"spread":100,"timestamp":1700000000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ob, err := client.GetOrderbook(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ob.Asks) != 1 {
		t.Fatalf("expected 1 ask, got %d", len(ob.Asks))
	}
	if ob.Spread != 100 {
		t.Fatalf("expected spread 100, got %f", ob.Spread)
	}
}

func TestGetCandlestick(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("candlestickType") != "PT1M" {
			t.Fatalf("expected candlestickType=PT1M, got %s", r.URL.Query().Get("candlestickType"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"candlesticks":[{"open":5000000,"high":5010000,"low":4990000,"close":5005000,"volume":10.5,"time":1700000000000}],"timestamp":1700000000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	resp, err := client.GetCandlestick(context.Background(), 7, "PT1M", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Candlesticks) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(resp.Candlesticks))
	}
	if resp.Candlesticks[0].Close != 5005000 {
		t.Fatalf("expected close 5005000, got %f", resp.Candlesticks[0].Close)
	}
}

func TestGetTrades(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"trades":[{"id":1,"orderSide":"BUY","price":5000000,"amount":0.1,"assetAmount":500000,"tradedAt":1700000000000}],"timestamp":1700000000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	resp, err := client.GetTrades(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(resp.Trades))
	}
}
