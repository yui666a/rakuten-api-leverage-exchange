package usecase

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func newRiskNotifyFixture(cfg entity.RiskConfig) (*RiskManager, chan RealtimeEvent, func()) {
	hub := NewRealtimeHub()
	sub := hub.Subscribe()
	rm := NewRiskManager(cfg)
	rm.SetRealtimeHub(hub)
	cleanup := func() { hub.Unsubscribe(sub) }
	return rm, sub, cleanup
}

func collectKind(t *testing.T, ch <-chan RealtimeEvent, want RiskEventKind, timeout time.Duration) RiskEventPayload {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case ev := <-ch:
			if ev.Type != "risk_event" {
				continue
			}
			var p RiskEventPayload
			if err := json.Unmarshal(ev.Data, &p); err != nil {
				t.Fatalf("unmarshal risk_event: %v", err)
			}
			if p.Kind == want {
				return p
			}
		case <-deadline.C:
			t.Fatalf("did not see risk_event %q within timeout", want)
		}
	}
}

func expectNoEvent(t *testing.T, ch <-chan RealtimeEvent, timeout time.Duration) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("expected no risk_event but got %s", ev.Type)
	case <-time.After(timeout):
	}
}

func TestRiskManager_DDWarningFiresAt15Pct(t *testing.T) {
	rm, sub, cleanup := newRiskNotifyFixture(entity.RiskConfig{InitialCapital: 100_000})
	defer cleanup()

	// DD = 14% — under threshold; no event.
	rm.UpdateBalance(86_000)
	expectNoEvent(t, sub, 50*time.Millisecond)

	// DD = 16% — should publish dd_warning once.
	rm.UpdateBalance(84_000)
	p := collectKind(t, sub, RiskEventDDWarning, 200*time.Millisecond)
	if p.Severity != RiskSeverityWarning {
		t.Fatalf("severity = %q, want warning", p.Severity)
	}
	if p.DDPct < 15 || p.DDPct > 17 {
		t.Fatalf("DDPct = %v, want ~16", p.DDPct)
	}

	// Re-cross at 16% should NOT re-fire (latch holds).
	rm.UpdateBalance(83_500)
	expectNoEvent(t, sub, 50*time.Millisecond)
}

func TestRiskManager_DDCriticalFiresAt18Pct(t *testing.T) {
	rm, sub, cleanup := newRiskNotifyFixture(entity.RiskConfig{InitialCapital: 100_000})
	defer cleanup()

	// Single jump straight past 18% — only critical (warn skipped because
	// the implementation marks both latches when crossing critical first).
	rm.UpdateBalance(80_000)
	p := collectKind(t, sub, RiskEventDDCritical, 200*time.Millisecond)
	if p.Severity != RiskSeverityCritical {
		t.Fatalf("severity = %q, want critical", p.Severity)
	}
}

func TestRiskManager_DDLatchClearsOnRecovery(t *testing.T) {
	rm, sub, cleanup := newRiskNotifyFixture(entity.RiskConfig{InitialCapital: 100_000})
	defer cleanup()

	rm.UpdateBalance(84_000) // 16% → warn fires
	collectKind(t, sub, RiskEventDDWarning, 200*time.Millisecond)

	// Recover above peak/2 threshold (DD < 7.5%) should release latch.
	rm.UpdateBalance(95_000)

	// Drain anything residual.
	expectNoEvent(t, sub, 50*time.Millisecond)

	// New crossing should fire again.
	rm.UpdateBalance(84_000)
	collectKind(t, sub, RiskEventDDWarning, 200*time.Millisecond)
}

func TestRiskManager_DailyLossWarningAtHalfMax(t *testing.T) {
	rm, sub, cleanup := newRiskNotifyFixture(entity.RiskConfig{InitialCapital: 100_000, MaxDailyLoss: 50_000})
	defer cleanup()
	rm.RecordLoss(20_000) // under 50%
	expectNoEvent(t, sub, 50*time.Millisecond)
	rm.RecordLoss(8_000) // total 28k > 25k = half
	p := collectKind(t, sub, RiskEventDailyLossWarning, 200*time.Millisecond)
	if p.MaxDaily != 50_000 {
		t.Fatalf("MaxDaily = %v, want 50000", p.MaxDaily)
	}
}

func TestRiskManager_ConsecutiveLossWarningAt3(t *testing.T) {
	rm, sub, cleanup := newRiskNotifyFixture(entity.RiskConfig{InitialCapital: 100_000})
	defer cleanup()
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()
	expectNoEvent(t, sub, 50*time.Millisecond)
	rm.RecordConsecutiveLoss()
	p := collectKind(t, sub, RiskEventConsecutiveLoss, 200*time.Millisecond)
	if p.StreakLen != 3 {
		t.Fatalf("StreakLen = %d, want 3", p.StreakLen)
	}
}

func TestRiskManager_NoHubMeansNoPanic(t *testing.T) {
	rm := NewRiskManager(entity.RiskConfig{InitialCapital: 100_000})
	// SetRealtimeHub(nil) explicitly to confirm the no-op path.
	rm.SetRealtimeHub(nil)
	rm.UpdateBalance(80_000)
	rm.RecordLoss(50_000)
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()
}
