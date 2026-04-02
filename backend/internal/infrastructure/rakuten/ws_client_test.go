package rakuten

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestWSClient_SubscribeAndReceive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("failed to accept websocket: %v", err)
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		_, msg, err := c.Read(context.Background())
		if err != nil {
			return
		}
		if !strings.Contains(string(msg), `"subscribe"`) {
			t.Fatalf("expected subscribe message, got %s", string(msg))
		}

		tickerData := `{"symbolId":7,"bestAsk":5000100,"bestBid":5000000,"last":5000050,"timestamp":1700000000000}`
		c.Write(context.Background(), websocket.MessageText, []byte(tickerData))
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewWSClient(wsURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgCh, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	err = client.Subscribe(ctx, 7, DataTypeTicker)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	select {
	case msg := <-msgCh:
		if !strings.Contains(string(msg), `"symbolId":7`) {
			t.Fatalf("unexpected message: %s", string(msg))
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for message")
	}

	client.Close()
}

func TestWSClient_Unsubscribe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		_, msg1, err := c.Read(context.Background())
		if err != nil {
			return
		}
		if !strings.Contains(string(msg1), `"subscribe"`) {
			t.Fatalf("expected subscribe, got %s", string(msg1))
		}

		_, msg2, err := c.Read(context.Background())
		if err != nil {
			return
		}
		if !strings.Contains(string(msg2), `"unsubscribe"`) {
			t.Fatalf("expected unsubscribe, got %s", string(msg2))
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewWSClient(wsURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	err = client.Subscribe(ctx, 7, DataTypeTicker)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	err = client.Unsubscribe(ctx, 7, DataTypeTicker)
	if err != nil {
		t.Fatalf("failed to unsubscribe: %v", err)
	}

	client.Close()
}
