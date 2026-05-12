package eventengine

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// PositionsContract is the invariant set that every OrderExecutor's
// Positions() must satisfy. It exists as a reusable assertion so the
// live executor and the backtest simulator are held to the same shape
// without duplicating boilerplate in each package's test file.
//
// The contract is captured in docs/design/2026-05-12-position-confirmed-only.md
// §3.2; this helper mechanises it. Both live (real_executor_test.go) and
// backtest (simulator_test.go) call AssertPositionsContract so that any
// future executor — or a regression in the existing ones — surfaces
// here as a fast unit test failure rather than as a live incident.
//
// The helper accepts the bare list rather than the executor itself so
// callers can also exercise the contract on a synthesised snapshot
// (e.g. immediately after SyncPositions).
//
// Failures are reported via t.Errorf so a single bad position does not
// short-circuit subsequent assertions; callers that want hard-stop
// semantics should pass a TB that wraps t.Fatalf.

// AssertPositionsContract validates that every Position satisfies the
// confirmed-only invariants. Pass an empty slice (no open positions)
// is fine — the contract is trivially true and the helper reports
// nothing. Returns the number of contract violations observed so
// callers can branch (e.g. skip further assertions when the executor
// is in an unexpected state).
func AssertPositionsContract(t testing.TB, positions []Position) int {
	t.Helper()
	violations := 0
	for i, p := range positions {
		if p.PositionID == 0 {
			t.Errorf("positions[%d]: PositionID = 0; confirmed-only contract requires venue-assigned id", i)
			violations++
		}
		if p.EntryPrice <= 0 {
			t.Errorf("positions[%d]: EntryPrice = %v; confirmed-only contract requires > 0 (no signalPrice fallback)", i, p.EntryPrice)
			violations++
		}
		if p.Amount <= 0 {
			t.Errorf("positions[%d]: Amount = %v; an open position must carry positive size", i, p.Amount)
			violations++
		}
		if p.Side != entity.OrderSideBuy && p.Side != entity.OrderSideSell {
			t.Errorf("positions[%d]: Side = %q; only BUY/SELL are valid", i, p.Side)
			violations++
		}
	}
	return violations
}
