package rakuten

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"nhooyr.io/websocket"
)

type DataType string

const (
	DataTypeTicker    DataType = "TICKER"
	DataTypeOrderbook DataType = "ORDERBOOK"
	DataTypeTrades    DataType = "TRADES"
)

type wsMessage struct {
	SymbolID int64    `json:"symbolId"`
	Type     string   `json:"type"`
	Data     DataType `json:"data"`
}

type WSClient struct {
	url  string
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewWSClient(url string) *WSClient {
	return &WSClient{url: url}
}

func (c *WSClient) Connect(ctx context.Context) (<-chan []byte, error) {
	conn, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	c.conn = conn

	msgCh := make(chan []byte, 100)

	go func() {
		defer close(msgCh)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			select {
			case msgCh <- data:
			case <-ctx.Done():
				return
			}
		}
	}()

	return msgCh, nil
}

func (c *WSClient) Subscribe(ctx context.Context, symbolID int64, dataType DataType) error {
	return c.sendMessage(ctx, symbolID, "subscribe", dataType)
}

func (c *WSClient) Unsubscribe(ctx context.Context, symbolID int64, dataType DataType) error {
	return c.sendMessage(ctx, symbolID, "unsubscribe", dataType)
}

func (c *WSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

func (c *WSClient) sendMessage(ctx context.Context, symbolID int64, msgType string, dataType DataType) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := wsMessage{
		SymbolID: symbolID,
		Type:     msgType,
		Data:     dataType,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	return c.conn.Write(ctx, websocket.MessageText, data)
}
