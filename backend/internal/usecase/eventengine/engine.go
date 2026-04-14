package eventengine

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// EventEngine drives deterministic event dispatch.
type EventEngine struct {
	bus *EventBus
}

func NewEventEngine(bus *EventBus) *EventEngine {
	return &EventEngine{bus: bus}
}

func (e *EventEngine) Run(ctx context.Context, events []entity.Event) error {
	if e.bus == nil {
		return nil
	}
	return e.bus.Dispatch(ctx, events)
}
