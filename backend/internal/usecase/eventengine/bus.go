package eventengine

import (
	"context"
	"fmt"
	"sort"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// EventHandler handles one event and can emit chained events.
type EventHandler interface {
	Handle(ctx context.Context, event entity.Event) ([]entity.Event, error)
}

type RegisteredHandler struct {
	Priority int
	Handler  EventHandler
}

// EventBus executes handlers synchronously with deterministic ordering.
type EventBus struct {
	handlers map[string][]RegisteredHandler
}

func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]RegisteredHandler),
	}
}

func (b *EventBus) Register(eventType string, priority int, handler EventHandler) {
	b.handlers[eventType] = append(b.handlers[eventType], RegisteredHandler{
		Priority: priority,
		Handler:  handler,
	})
	sort.SliceStable(b.handlers[eventType], func(i, j int) bool {
		return b.handlers[eventType][i].Priority < b.handlers[eventType][j].Priority
	})
}

// Dispatch runs events with FIFO queue and appends chained events in return order.
func (b *EventBus) Dispatch(ctx context.Context, initial []entity.Event) error {
	queue := append([]entity.Event(nil), initial...)

	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		ev := queue[0]
		queue = queue[1:]
		if ev == nil {
			continue
		}

		for _, reg := range b.handlers[ev.EventType()] {
			chained, err := reg.Handler.Handle(ctx, ev)
			if err != nil {
				return fmt.Errorf("eventType=%s priority=%d: %w", ev.EventType(), reg.Priority, err)
			}
			if len(chained) > 0 {
				queue = append(queue, chained...)
			}
		}
	}

	return nil
}
