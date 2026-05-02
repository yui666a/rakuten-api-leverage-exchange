package entity

import "testing"

func TestSignalDirection_Constants(t *testing.T) {
	cases := []struct {
		dir  SignalDirection
		want string
	}{
		{DirectionBullish, "BULLISH"},
		{DirectionBearish, "BEARISH"},
		{DirectionNeutral, "NEUTRAL"},
	}
	for _, c := range cases {
		if string(c.dir) != c.want {
			t.Errorf("got %q, want %q", c.dir, c.want)
		}
	}
}

func TestMarketSignalEvent_EventInterface(t *testing.T) {
	ev := MarketSignalEvent{
		Signal:    MarketSignal{Direction: DirectionBullish, Strength: 0.7},
		Price:     8900.0,
		Timestamp: 1700000000000,
	}
	if got := ev.EventType(); got != EventTypeMarketSignal {
		t.Errorf("EventType=%q, want %q", got, EventTypeMarketSignal)
	}
	if got := ev.EventTimestamp(); got != 1700000000000 {
		t.Errorf("EventTimestamp=%d, want 1700000000000", got)
	}
}

func TestEventTypeMarketSignal_Value(t *testing.T) {
	if EventTypeMarketSignal != "market_signal" {
		t.Errorf("EventTypeMarketSignal=%q, want %q", EventTypeMarketSignal, "market_signal")
	}
}
