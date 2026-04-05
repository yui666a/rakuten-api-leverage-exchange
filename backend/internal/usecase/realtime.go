package usecase

import (
	"encoding/json"
	"sync"
)

type RealtimeEvent struct {
	Type     string          `json:"type"`
	SymbolID int64           `json:"symbolId,omitempty"`
	Data     json.RawMessage `json:"data"`
}

type RealtimeHub struct {
	mu   sync.RWMutex
	subs map[chan RealtimeEvent]struct{}
}

func NewRealtimeHub() *RealtimeHub {
	return &RealtimeHub{
		subs: make(map[chan RealtimeEvent]struct{}),
	}
}

func (h *RealtimeHub) Subscribe() chan RealtimeEvent {
	ch := make(chan RealtimeEvent, 128)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *RealtimeHub) Unsubscribe(ch chan RealtimeEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; !ok {
		return
	}
	delete(h.subs, ch)
	close(ch)
}

func (h *RealtimeHub) Publish(event RealtimeEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *RealtimeHub) PublishData(eventType string, symbolID int64, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	h.Publish(RealtimeEvent{
		Type:     eventType,
		SymbolID: symbolID,
		Data:     data,
	})
	return nil
}
