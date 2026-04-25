package usecase

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// drainOne reads up to one event from the hub channel without blocking the
// test forever — tests fail fast when the executor forgets to publish.
func drainOne(t *testing.T, ch <-chan RealtimeEvent) (RealtimeEvent, bool) {
	t.Helper()
	select {
	case ev := <-ch:
		return ev, true
	case <-time.After(200 * time.Millisecond):
		return RealtimeEvent{}, false
	}
}

func TestOrderExecutor_ExecuteSignal_PublishesTradeEventOnSuccess(t *testing.T) {
	hub := NewRealtimeHub()
	sub := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(sub) })

	orderClient := &mockOrderClient{
		createdOrders: []entity.Order{
			{ID: 100, SymbolID: 7, OrderSide: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket, Amount: 0.001},
		},
	}
	riskMgr := NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 1_000_000_000,
		MaxDailyLoss:      1_000_000_000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})
	executor := NewOrderExecutor(orderClient, riskMgr)
	executor.SetRealtimeHub(hub)

	signal := entity.Signal{
		SymbolID:  7,
		Action:    entity.SignalActionBuy,
		Reason:    "trend follow",
		Timestamp: time.Now().Unix(),
	}
	if _, err := executor.ExecuteSignal(context.Background(), "co-test", signal, 4_000_000, 0.001); err != nil {
		t.Fatalf("ExecuteSignal: %v", err)
	}

	ev, ok := drainOne(t, sub)
	if !ok {
		t.Fatal("expected a trade_event after successful execution")
	}
	if ev.Type != "trade_event" {
		t.Fatalf("event type = %q, want trade_event", ev.Type)
	}

	var payload TradeEventPayload
	if err := json.Unmarshal(ev.Data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Kind != TradeEventOpen {
		t.Fatalf("Kind = %q, want %q", payload.Kind, TradeEventOpen)
	}
	if payload.OrderID != 100 || payload.SymbolID != 7 {
		t.Fatalf("payload mismatch: %+v", payload)
	}
	if payload.Side != string(entity.OrderSideBuy) {
		t.Fatalf("Side = %q, want %s", payload.Side, entity.OrderSideBuy)
	}
}

func TestOrderExecutor_ExecuteSignal_NoEventOnHold(t *testing.T) {
	hub := NewRealtimeHub()
	sub := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(sub) })

	executor := NewOrderExecutor(&mockOrderClient{}, NewRiskManager(entity.RiskConfig{}))
	executor.SetRealtimeHub(hub)

	if _, err := executor.ExecuteSignal(context.Background(), "co-hold", entity.Signal{Action: entity.SignalActionHold}, 0, 0); err != nil {
		t.Fatalf("ExecuteSignal: %v", err)
	}
	if _, ok := drainOne(t, sub); ok {
		t.Fatal("HOLD signal should not publish a trade_event")
	}
}

func TestOrderExecutor_ExecuteSignal_NoEventOnRiskRejection(t *testing.T) {
	hub := NewRealtimeHub()
	sub := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(sub) })

	// MaxPositionAmount=0 forces every order through risk rejection.
	riskMgr := NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 0,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})
	executor := NewOrderExecutor(&mockOrderClient{}, riskMgr)
	executor.SetRealtimeHub(hub)

	signal := entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy}
	if _, err := executor.ExecuteSignal(context.Background(), "co-rej", signal, 4_000_000, 0.001); err != nil {
		t.Fatalf("ExecuteSignal: %v", err)
	}
	if _, ok := drainOne(t, sub); ok {
		t.Fatal("risk-rejected order must not publish a trade_event")
	}
}

func TestOrderExecutor_ClosePosition_PublishesTradeEvent(t *testing.T) {
	hub := NewRealtimeHub()
	sub := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(sub) })

	orderClient := &mockOrderClient{
		createdOrders: []entity.Order{
			{ID: 200, SymbolID: 10, OrderSide: entity.OrderSideSell, OrderType: entity.OrderTypeMarket, Amount: 0.1},
		},
	}
	riskMgr := NewRiskManager(entity.RiskConfig{MaxPositionAmount: 1_000_000_000, StopLossPercent: 5, InitialCapital: 10000})
	executor := NewOrderExecutor(orderClient, riskMgr)
	executor.SetRealtimeHub(hub)

	pos := entity.Position{
		ID: 271282, SymbolID: 10, OrderSide: entity.OrderSideBuy,
		Amount: 0.1, RemainingAmount: 0.1, Price: 8587.6,
	}
	if _, err := executor.ClosePosition(context.Background(), "co-close", pos, 9000); err != nil {
		t.Fatalf("ClosePosition: %v", err)
	}
	ev, ok := drainOne(t, sub)
	if !ok {
		t.Fatal("expected trade_event on close")
	}
	var payload TradeEventPayload
	if err := json.Unmarshal(ev.Data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Kind != TradeEventClose {
		t.Fatalf("Kind = %q, want %q", payload.Kind, TradeEventClose)
	}
	if payload.PositionID != 271282 {
		t.Fatalf("PositionID = %d, want 271282", payload.PositionID)
	}
	if payload.Side != string(entity.OrderSideSell) {
		t.Fatalf("close side should be opposite of position side, got %q", payload.Side)
	}
}

func TestOrderExecutor_HubOptional_NoPanicWhenNotWired(t *testing.T) {
	executor := NewOrderExecutor(&mockOrderClient{
		createdOrders: []entity.Order{{ID: 9, OrderSide: entity.OrderSideBuy}},
	}, NewRiskManager(entity.RiskConfig{MaxPositionAmount: 1_000_000_000, StopLossPercent: 5, InitialCapital: 10000}))
	// No SetRealtimeHub — publishTradeEvent must be a no-op.
	signal := entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy}
	if _, err := executor.ExecuteSignal(context.Background(), "co-nohub", signal, 4_000_000, 0.001); err != nil {
		t.Fatalf("ExecuteSignal: %v", err)
	}
}
