package entity

import "testing"

func TestRejectedSignalEvent_ImplementsEvent(t *testing.T) {
	e := RejectedSignalEvent{
		Signal:    Signal{SymbolID: 7, Action: SignalActionBuy, Reason: "ema cross"},
		Stage:     RejectedStageRisk,
		Reason:    "daily loss limit hit",
		Price:     30210,
		Timestamp: 1745654700000,
	}
	if e.EventType() != EventTypeRejected {
		t.Errorf("EventType = %q, want %q", e.EventType(), EventTypeRejected)
	}
	if e.EventTimestamp() != 1745654700000 {
		t.Errorf("EventTimestamp = %d", e.EventTimestamp())
	}
}

func TestOrderEvent_NewFieldsDefaultZero(t *testing.T) {
	var e OrderEvent
	if e.Trigger != "" || e.OpenedPositionID != 0 || e.ClosedPositionID != 0 {
		t.Errorf("zero value of new fields must be empty: %+v", e)
	}
}

func TestOrderEvent_NewFieldsCarryThrough(t *testing.T) {
	e := OrderEvent{
		OrderID:          42,
		Trigger:          DecisionTriggerBarClose,
		OpenedPositionID: 100,
		ClosedPositionID: 99,
	}
	if e.Trigger != DecisionTriggerBarClose || e.OpenedPositionID != 100 || e.ClosedPositionID != 99 {
		t.Errorf("fields not carried: %+v", e)
	}
}
