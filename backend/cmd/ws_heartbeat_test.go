package main

import (
	"testing"
	"time"
)

// TestWsIsSilentlyDead pins the watchdog's stale-detection contract: only
// gaps strictly greater than the threshold trigger a reconnect, so a venue
// that pushes one frame per minute exactly at the boundary does not
// thrash the connection.
func TestWsIsSilentlyDead(t *testing.T) {
	threshold := 60 * time.Second
	base := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		gap   time.Duration
		stale bool
	}{
		{name: "fresh frame", gap: 0, stale: false},
		{name: "well within threshold", gap: 30 * time.Second, stale: false},
		{name: "exactly at threshold", gap: 60 * time.Second, stale: false},
		{name: "1ms past threshold", gap: 60*time.Second + time.Millisecond, stale: true},
		{name: "long silence", gap: 5 * time.Minute, stale: true},
		{name: "incident-scale silence (7h)", gap: 7 * time.Hour, stale: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := wsIsSilentlyDead(base.Add(tc.gap), base, threshold)
			if got != tc.stale {
				t.Errorf("wsIsSilentlyDead(gap=%v, threshold=%v) = %v, want %v", tc.gap, threshold, got, tc.stale)
			}
		})
	}
}

// TestWsHeartbeatThresholdSanity guards the constants from regression to
// values that would either thrash on legitimate quiet markets or fail to
// catch the kind of multi-hour silent gap the 2026-04-26 incident
// exhibited.
func TestWsHeartbeatThresholdSanity(t *testing.T) {
	if wsHeartbeatStaleAfter < 30*time.Second {
		t.Errorf("wsHeartbeatStaleAfter = %v, want >= 30s (legitimate market lulls can exceed shorter windows)", wsHeartbeatStaleAfter)
	}
	if wsHeartbeatStaleAfter > 5*time.Minute {
		t.Errorf("wsHeartbeatStaleAfter = %v, want <= 5m (longer windows lose entire PT15M decision bars)", wsHeartbeatStaleAfter)
	}
	if wsHeartbeatCheckInterval >= wsHeartbeatStaleAfter {
		t.Errorf("wsHeartbeatCheckInterval (%v) must be smaller than wsHeartbeatStaleAfter (%v) so the watchdog can actually fire before the gap doubles",
			wsHeartbeatCheckInterval, wsHeartbeatStaleAfter)
	}
}
