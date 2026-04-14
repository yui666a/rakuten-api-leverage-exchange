package eventengine

import (
	"context"
	"fmt"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type testEvent struct {
	typ string
	id  string
	ts  int64
}

func (e testEvent) EventType() string     { return e.typ }
func (e testEvent) EventTimestamp() int64 { return e.ts }

type testHandler struct {
	name   string
	logs   *[]string
	emit   func(event entity.Event) []entity.Event
	handle func(event entity.Event) error
}

func (h *testHandler) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
	*h.logs = append(*h.logs, fmt.Sprintf("%s:%s", h.name, event.(testEvent).id))
	if h.handle != nil {
		if err := h.handle(event); err != nil {
			return nil, err
		}
	}
	if h.emit != nil {
		return h.emit(event), nil
	}
	return nil, nil
}

func TestEventBus_FIFOAndPriority(t *testing.T) {
	bus := NewEventBus()
	var logs []string

	bus.Register("a", 20, &testHandler{name: "p20", logs: &logs})
	bus.Register("a", 10, &testHandler{name: "p10", logs: &logs})
	bus.Register("b", 10, &testHandler{name: "b10", logs: &logs})

	bus.Register("a", 15, &testHandler{
		name: "p15",
		logs: &logs,
		emit: func(event entity.Event) []entity.Event {
			id := event.(testEvent).id
			return []entity.Event{
				testEvent{typ: "b", id: id + "-b1", ts: 1},
				testEvent{typ: "b", id: id + "-b2", ts: 2},
			}
		},
	})

	err := bus.Dispatch(context.Background(), []entity.Event{
		testEvent{typ: "a", id: "a1", ts: 1},
		testEvent{typ: "a", id: "a2", ts: 2},
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	want := []string{
		"p10:a1", "p15:a1", "p20:a1",
		"p10:a2", "p15:a2", "p20:a2",
		"b10:a1-b1", "b10:a1-b2",
		"b10:a2-b1", "b10:a2-b2",
	}
	if len(logs) != len(want) {
		t.Fatalf("log length mismatch: got=%d want=%d logs=%v", len(logs), len(want), logs)
	}
	for i := range want {
		if logs[i] != want[i] {
			t.Fatalf("log[%d] mismatch: got=%s want=%s full=%v", i, logs[i], want[i], logs)
		}
	}
}

func TestEventEngine_NilBus(t *testing.T) {
	engine := NewEventEngine(nil)
	err := engine.Run(context.Background(), []entity.Event{
		testEvent{typ: "a", id: "x", ts: 1},
	})
	if err != nil {
		t.Fatalf("expected nil error for nil bus, got: %v", err)
	}
}
