package rakuten

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRESTClient_RateLimiting(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "key", "secret")

	ctx := context.Background()

	start := time.Now()
	_, _ = client.DoPublic(ctx, "GET", "/test1", "", nil)
	_, _ = client.DoPublic(ctx, "GET", "/test2", "", nil)
	elapsed := time.Since(start)

	if elapsed < 200*time.Millisecond {
		t.Fatalf("rate limiting not working: 2 requests completed in %v, expected >= 200ms", elapsed)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
}

func TestRESTClient_DoPublic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/ticker" {
			t.Fatalf("expected /api/v1/ticker, got %s", r.URL.Path)
		}
		if r.URL.RawQuery != "symbolId=7" {
			t.Fatalf("expected symbolId=7, got %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"last":5000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ctx := context.Background()

	body, err := client.DoPublic(ctx, "GET", "/api/v1/ticker", "symbolId=7", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("body should not be empty")
	}
}

func TestRESTClient_DoPrivate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("API-KEY")
		nonce := r.Header.Get("NONCE")
		signature := r.Header.Get("SIGNATURE")

		if apiKey != "test-key" {
			t.Fatalf("expected API-KEY 'test-key', got '%s'", apiKey)
		}
		if nonce == "" {
			t.Fatal("NONCE header should not be empty")
		}
		if signature == "" {
			t.Fatal("SIGNATURE header should not be empty")
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"currency":"JPY","onhandAmount":10000}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "test-key", "test-secret")
	ctx := context.Background()

	body, err := client.DoPrivate(ctx, "GET", "/api/v1/asset", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("body should not be empty")
	}
}

func TestRESTClient_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":"INVALID_PARAMETER","message":"invalid symbolId"}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ctx := context.Background()

	_, err := client.DoPublic(ctx, "GET", "/api/v1/ticker", "symbolId=999", nil)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}
