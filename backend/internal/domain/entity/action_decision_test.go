package entity

import "testing"

func TestDecisionIntent_Constants(t *testing.T) {
	cases := []struct {
		intent DecisionIntent
		want   string
	}{
		{IntentNewEntry, "NEW_ENTRY"},
		{IntentExitCandidate, "EXIT_CANDIDATE"},
		{IntentHold, "HOLD"},
		{IntentCooldownBlocked, "COOLDOWN_BLOCKED"},
	}
	for _, c := range cases {
		if string(c.intent) != c.want {
			t.Errorf("got %q, want %q", c.intent, c.want)
		}
	}
}

func TestActionDecision_IsActionable(t *testing.T) {
	cases := []struct {
		intent DecisionIntent
		want   bool
	}{
		{IntentNewEntry, true},
		{IntentExitCandidate, true},
		{IntentHold, false},
		{IntentCooldownBlocked, false},
	}
	for _, c := range cases {
		d := ActionDecision{Intent: c.intent}
		if got := d.IsActionable(); got != c.want {
			t.Errorf("Intent=%q IsActionable=%v, want %v", c.intent, got, c.want)
		}
	}
}

func TestActionDecisionEvent_EventInterface(t *testing.T) {
	ev := ActionDecisionEvent{
		Decision: ActionDecision{
			Intent: IntentNewEntry,
			Side:   OrderSideBuy,
		},
		Price:     8900.0,
		Timestamp: 1700000000000,
	}
	if got := ev.EventType(); got != EventTypeDecision {
		t.Errorf("EventType=%q, want %q", got, EventTypeDecision)
	}
	if got := ev.EventTimestamp(); got != 1700000000000 {
		t.Errorf("EventTimestamp=%d, want 1700000000000", got)
	}
}

func TestEventTypeDecision_Value(t *testing.T) {
	if EventTypeDecision != "decision" {
		t.Errorf("EventTypeDecision=%q, want %q", EventTypeDecision, "decision")
	}
}
