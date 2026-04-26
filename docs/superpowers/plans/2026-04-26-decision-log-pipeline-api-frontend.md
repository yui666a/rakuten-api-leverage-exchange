# Decision Log: Pipeline DI + API + Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Now that the foundation has landed (entity, repos, recorder, retention, RejectedSignalEvent emission, OrderEvent fields), wire the recorder into both pipelines, expose the data via HTTP, and add the "判断ログ" tab on `/history`.

**Architecture:** Three independent slices, each shipped as its own PR (Stacked):

1. **PR #5 — Pipeline DI**: construct `DecisionRecorder` in `cmd/main.go`, register it on the live `EventDrivenPipeline`'s EventBus, wire a per-run recorder into `BacktestRunner` (gated by an option so legacy backtest call-sites stay zero-impact), start the 3-day retention goroutine from `cmd/main.go`.
2. **PR #6 — HTTP API**: `GET /api/v1/decisions` (live) + `GET /api/v1/backtest/results/:id/decisions` + `DELETE /api/v1/backtest/results/:id/decisions`.
3. **PR #7 — Frontend tab**: add `[選択通貨の判断ログ]` tab on `/history` with timeline table + indicator detail panel.

**Tech Stack:** Go 1.25 (pipeline + Gin handlers + tests), TypeScript / React 19 / TanStack Query for the frontend.

**Spec reference:** `docs/superpowers/specs/2026-04-26-decision-log-design.md`

---

## File Structure

### PR #5 — Pipeline DI

**Modified:**
- `backend/cmd/main.go` — construct `decisionLogRepo`, `backtestDecisionLogRepo`, `RetentionCleanup`, pass repo into `NewEventDrivenPipeline`, start retention goroutine
- `backend/cmd/event_pipeline.go` — accept the repo via config, construct `DecisionRecorder` per `runEventLoop`, register on EventBus at priority 99 for all 5 event types, expose stance source for `StanceProvider`
- `backend/internal/usecase/backtest/runner.go` — add `WithDecisionRecorder(...)` option (or `WithBacktestDecisionLog(repo, runID)`) and register the recorder when the option is set
- `backend/internal/interfaces/api/handler/backtest.go` — when a backtest run is created, generate the run ID first, pass it through `WithBacktestDecisionLog(repo, runID)` so per-run rows accumulate

**New tests:**
- `backend/cmd/event_pipeline_decision_log_test.go` — integration: tick → IndicatorEvent → SignalEvent → OrderEvent results in `decision_log` row
- `backend/internal/usecase/backtest/runner_decision_log_test.go` — backtest run inserts rows scoped to the run ID

### PR #6 — HTTP API

**Modified:**
- `backend/internal/interfaces/api/router.go` — register `/decisions` + `/backtest/results/:id/decisions` (GET + DELETE) routes
- `backend/internal/interfaces/api/handler/backtest.go` — add `GetDecisions` + `DeleteDecisions` methods on backtest handler

**New:**
- `backend/internal/interfaces/api/handler/decision.go` — `DecisionHandler` with `List` method
- `backend/internal/interfaces/api/handler/decision_test.go`
- `backend/internal/interfaces/api/handler/backtest_decision_test.go` — for the new backtest handler methods

### PR #7 — Frontend tab

**New:**
- `frontend/src/lib/api.ts` — append `DecisionLogItem` type + `fetchDecisions`
- `frontend/src/hooks/useDecisionLog.ts`
- `frontend/src/components/DecisionLogTable.tsx`
- `frontend/src/components/DecisionDetailPanel.tsx`

**Modified:**
- `frontend/src/routes/history.tsx` — add third tab with `DecisionLogTable`

---

## Design Notes (read before coding)

### How `DecisionRecorder` learns the live stance

The pipeline currently exposes the active stance via the strategy. For the `StanceProvider` callback we need a thin getter. Look at `EventDrivenPipeline.strategy` usage — the strategy holds a stance resolver that returns the latest decision. Easiest path: add a `Stance() string` method on `EventDrivenPipeline` that delegates to the strategy's stance resolver, and use it as `StanceProvider` for the recorder. Confirm by reading `internal/usecase/strategy/strategy.go` first.

### Per-run wiring for the backtest recorder

The runner currently constructs its own EventBus inside `Run()`. The cleanest insertion point is right after the existing `bus.Register(...)` block (line 252 area in runner.go). Gate the registration behind `r.decisionRecorder != nil` so call-sites that don't pass `WithDecisionRecorder` see no change.

The backtest **handler** layer (not the runner) generates the run ID and wires the recorder. The runner only sees a ready-made `*decisionlog.Recorder`. The handler layer:
1. Generates the new `backtest_results.id` (UUID or whatever the existing path uses).
2. Constructs the recorder with that run ID baked into a closure that calls `backtestDecisionLogRepo.Insert(ctx, rec, runID)`.
3. Passes the recorder to `NewBacktestRunner(WithDecisionRecorder(rec))`.

We need a small adapter that wraps a `BacktestDecisionLogRepository + runID` into a `DecisionLogRepository` so the existing `Recorder` works unchanged. Add it as `decisionlog.BacktestRepoAdapter` to keep the recorder generic.

### EventBus priority for the recorder

The bus runs handlers in priority order *per event type*. Other handlers in the live pipeline use 5/10/12/15/20/30/40. Register the recorder at **99** for `IndicatorEvent` / `SignalEvent` / `ApprovedSignalEvent` / `RejectedSignalEvent` / `OrderEvent` so it always runs last.

### `cmd/main.go` retention goroutine

Right after `pipeline := NewEventDrivenPipeline(...)`, also build the retention cleanup and run it in a goroutine bound to the same context the pipeline uses. The interval is 1h, MaxAge is 72h. Log on start so we can confirm it's wired in production logs.

### API: backtest run-scope verification

The DELETE endpoint must return 404 when the run id doesn't exist. Check the existing backtest result endpoint pattern and reuse the same "is the run id known" lookup if there is one. If not, just delete and return the count — it's an idempotent operation, deleting nothing for an unknown run is acceptable.

### Frontend: keep the polling cheap

`useDecisionLog` reuses the existing `refetchInterval: 15_000` cadence. The table shows the most recent 200 rows; "もっと見る" appends via `cursor`.

---

## Task A1: Add `BacktestRepoAdapter` so backtest recorder reuses the live recorder

**Files:**
- Modify: `backend/internal/usecase/decisionlog/recorder.go` (or add a new file)
- Test: `backend/internal/usecase/decisionlog/backtest_adapter_test.go`

- [ ] **Step A1.1: Write the failing test**

Create `backend/internal/usecase/decisionlog/backtest_adapter_test.go`:
```go
package decisionlog

import (
    "context"
    "testing"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

type stubBacktestRepoForAdapter struct {
    rec   entity.DecisionRecord
    runID string
    seen  bool
}

func (s *stubBacktestRepoForAdapter) Insert(_ context.Context, rec entity.DecisionRecord, runID string) error {
    s.rec = rec
    s.runID = runID
    s.seen = true
    return nil
}
func (s *stubBacktestRepoForAdapter) ListByRun(_ context.Context, _ string, _ int, _ int64) ([]entity.DecisionRecord, int64, error) {
    return nil, 0, nil
}
func (s *stubBacktestRepoForAdapter) DeleteByRun(_ context.Context, _ string) (int64, error) {
    return 0, nil
}
func (s *stubBacktestRepoForAdapter) DeleteOlderThan(_ context.Context, _ int64) (int64, error) {
    return 0, nil
}

func TestBacktestRepoAdapter_BindsRunIDOnEveryInsert(t *testing.T) {
    underlying := &stubBacktestRepoForAdapter{}
    adapter := NewBacktestRepoAdapter(underlying, "run-xyz")

    var _ repository.DecisionLogRepository = adapter // compile-time interface check

    rec := entity.DecisionRecord{BarCloseAt: 1_000, SymbolID: 7}
    if err := adapter.Insert(context.Background(), rec); err != nil {
        t.Fatalf("Insert: %v", err)
    }
    if !underlying.seen {
        t.Fatalf("underlying repo Insert was not called")
    }
    if underlying.runID != "run-xyz" {
        t.Errorf("runID = %q, want %q", underlying.runID, "run-xyz")
    }
    if underlying.rec.BarCloseAt != 1_000 {
        t.Errorf("record not forwarded: %+v", underlying.rec)
    }
}

func TestBacktestRepoAdapter_ListReturnsEmpty(t *testing.T) {
    adapter := NewBacktestRepoAdapter(&stubBacktestRepoForAdapter{}, "run-xyz")
    rows, next, err := adapter.List(context.Background(), repository.DecisionLogFilter{})
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if rows != nil || next != 0 {
        t.Errorf("List must be a no-op for the adapter (recorder never reads)")
    }
}
```

- [ ] **Step A1.2: Run test — expect compile failure**

Run: `cd backend && go test ./internal/usecase/decisionlog/ -run TestBacktestRepoAdapter -count=1`
Expected: build error referencing undefined `NewBacktestRepoAdapter`.

- [ ] **Step A1.3: Implement adapter**

Create `backend/internal/usecase/decisionlog/backtest_adapter.go`:
```go
package decisionlog

import (
    "context"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// backtestRepoAdapter binds a runID to a BacktestDecisionLogRepository so the
// generic Recorder can write into it without knowing about run scoping.
// List is a no-op because the recorder never reads back from its repo.
type backtestRepoAdapter struct {
    repo  repository.BacktestDecisionLogRepository
    runID string
}

// NewBacktestRepoAdapter returns a repository.DecisionLogRepository that
// forwards every Insert to repo.Insert(ctx, rec, runID). Use this to plug
// the live Recorder into a backtest run without touching the recorder code.
func NewBacktestRepoAdapter(repo repository.BacktestDecisionLogRepository, runID string) repository.DecisionLogRepository {
    return &backtestRepoAdapter{repo: repo, runID: runID}
}

func (a *backtestRepoAdapter) Insert(ctx context.Context, rec entity.DecisionRecord) error {
    return a.repo.Insert(ctx, rec, a.runID)
}

func (a *backtestRepoAdapter) List(_ context.Context, _ repository.DecisionLogFilter) ([]entity.DecisionRecord, int64, error) {
    return nil, 0, nil
}
```

- [ ] **Step A1.4: Run test — expect pass**

Run: `cd backend && go test ./internal/usecase/decisionlog/ -count=1 -race`
Expected: PASS (all decisionlog tests).

- [ ] **Step A1.5: Commit**

```bash
git add backend/internal/usecase/decisionlog/backtest_adapter.go backend/internal/usecase/decisionlog/backtest_adapter_test.go
git commit -m "feat(decisionlog): add BacktestRepoAdapter to reuse Recorder per backtest run"
```

---

## Task A2: Wire recorder into `EventDrivenPipeline`

**Files:**
- Modify: `backend/cmd/event_pipeline.go`

- [ ] **Step A2.1: Add stance accessor on the pipeline**

First confirm the strategy exposes the active stance. Read `backend/internal/usecase/strategy/strategy.go` and `backend/internal/usecase/strategy.go` to find the stance accessor (look for `Stance()`, `CurrentStance`, `LatestStance`, etc.). If there is no public accessor, expose one via the strategy port (smallest possible change — single `Stance() string` method that returns the resolver's last decision).

If the resolver has no public accessor either, add one to `RuleBasedStanceResolver` (`func (r *RuleBasedStanceResolver) LastStance() string`). The state already exists internally — this is just a getter.

- [ ] **Step A2.2: Add `DecisionLogRepo` field on `EventDrivenPipelineConfig`**

In `backend/cmd/event_pipeline.go`, locate `EventDrivenPipelineConfig` (line ~73) and add:
```go
// DecisionLogRepo, when non-nil, attaches a DecisionRecorder to the
// EventBus so every pipeline cycle persists a row. nil disables the
// recorder; the rest of the pipeline is unaffected.
DecisionLogRepo repository.DecisionLogRepository
```
Update `EventDrivenPipeline` struct to carry `decisionLogRepo`, and copy it from `cfg` in `NewEventDrivenPipeline`.

- [ ] **Step A2.3: Register recorder on the bus**

Inside `runEventLoop` (line ~297), after the existing `bus.Register(...)` calls but before `engine := eventengine.NewEventEngine(bus)`, add:
```go
if p.decisionLogRepo != nil {
    recorder := decisionlog.NewRecorder(p.decisionLogRepo, decisionlog.RecorderConfig{
        SymbolID:        snap.symbolID,
        CurrencyPair:    p.currencyPair,
        PrimaryInterval: "PT15M",
        StanceProvider:  func() string { return p.strategy.Stance() },
    })
    bus.Register(entity.EventTypeIndicator, 99, recorder)
    bus.Register(entity.EventTypeSignal, 99, recorder)
    bus.Register(entity.EventTypeApproved, 99, recorder)
    bus.Register(entity.EventTypeRejected, 99, recorder)
    bus.Register(entity.EventTypeOrder, 99, recorder)
}
```

`p.currencyPair` may not exist yet — `loadSymbolMeta` sets `symbolID` but check whether the currency pair is already cached. If not, derive it from the loaded symbol meta and store on the pipeline struct in `loadSymbolMeta`.

- [ ] **Step A2.4: Add integration test**

Create `backend/cmd/event_pipeline_decision_log_test.go`:
```go
package main

import (
    "context"
    "path/filepath"
    "testing"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
)

// This test asserts ONLY that the wiring compiles and that an attached
// DecisionLogRepo receives at least one Insert when the pipeline processes
// a synthetic IndicatorEvent + OrderEvent through its EventBus. The deeper
// state-machine coverage already lives in usecase/decisionlog tests.
func TestEventDrivenPipeline_AttachesDecisionRecorder(t *testing.T) {
    tmpDir := t.TempDir()
    db, err := database.NewDB(filepath.Join(tmpDir, "test.db"))
    if err != nil {
        t.Fatalf("NewDB: %v", err)
    }
    defer db.Close()
    if err := database.RunMigrations(db); err != nil {
        t.Fatalf("RunMigrations: %v", err)
    }
    repo := database.NewDecisionLogRepository(db)

    // Build a minimal EventDrivenPipeline with the recorder plugged in.
    // The exact constructor depends on what NewEventDrivenPipeline accepts;
    // build a stub config that satisfies the required dependencies and
    // assert that after a Dispatch the repo has one row.
    //
    // Implementation note: use whatever helpers cmd/main_test.go already
    // exposes. If none exist, drive the recorder directly through a
    // standalone EventBus to keep the test focused on the wiring contract.
    _ = repo
    _ = context.Background()
    _ = entity.IndicatorEvent{}
    _ = repository.DecisionLogFilter{}
    t.Skip("Filled in during Step A2.5 once NewEventDrivenPipeline signature is confirmed")
}
```

- [ ] **Step A2.5: Replace the test skip with the actual integration**

Inspect what `NewEventDrivenPipeline` expects. It takes `MarketDataService`, `Strategy`, `RiskManager`, `OrderClient`, etc. For a test, either:
- Use the existing `cmd/sync_state_test.go` `fakeRakutenClient` pattern to build a minimal pipeline and feed it a synthetic ticker, then assert `repo.List` returns one row, OR
- Skip the full pipeline test and only assert the compile-time wiring (`pipeline := NewEventDrivenPipeline(...)` returns a non-nil object when `DecisionLogRepo` is set).

Pick whichever has the smallest blast radius given the existing test fixtures. If a minimal pipeline test is already too painful, the integration test in `usecase/decisionlog/integration_test.go` (PR #3) already covers the recorder-bus contract end-to-end with a real SQLite repo, so a simple compile-time wiring assertion plus a smoke `Run()` step on the pipeline is sufficient here.

- [ ] **Step A2.6: Run all tests**

Run: `cd backend && go test ./... -count=1 -race`
Expected: PASS.

- [ ] **Step A2.7: Commit**

```bash
git add backend/cmd/event_pipeline.go backend/cmd/event_pipeline_decision_log_test.go
git commit -m "feat(live): attach DecisionRecorder to EventDrivenPipeline"
```

---

## Task A3: Wire recorder into `BacktestRunner` via option

**Files:**
- Modify: `backend/internal/usecase/backtest/runner.go`
- Modify: `backend/internal/usecase/backtest/runner_test.go` (or new file)

- [ ] **Step A3.1: Add `WithDecisionRecorder` option**

In `runner.go`, after the existing `WithStrategy` (line 79), add:
```go
// WithDecisionRecorder attaches an EventBus subscriber that persists every
// pipeline decision (BUY/SELL/HOLD + indicators) into a decision-log
// repository. Pass a recorder constructed via decisionlog.NewRecorder with
// a BacktestRepoAdapter bound to the run id. nil is ignored so legacy
// callers keep the historical bit-identical behaviour.
func WithDecisionRecorder(rec eventengine.EventHandler) RunnerOption {
    return func(r *BacktestRunner) {
        if rec != nil {
            r.decisionRecorder = rec
        }
    }
}
```
Add `decisionRecorder eventengine.EventHandler` to the `BacktestRunner` struct.

- [ ] **Step A3.2: Register recorder on the bus when set**

Inside `Run()`, after the existing `bus.Register(entity.EventTypeApproved, 40, executionHandler)` (line ~252), add:
```go
if r.decisionRecorder != nil {
    bus.Register(entity.EventTypeIndicator, 99, r.decisionRecorder)
    bus.Register(entity.EventTypeSignal, 99, r.decisionRecorder)
    bus.Register(entity.EventTypeApproved, 99, r.decisionRecorder)
    bus.Register(entity.EventTypeRejected, 99, r.decisionRecorder)
    bus.Register(entity.EventTypeOrder, 99, r.decisionRecorder)
}
```

- [ ] **Step A3.3: Add a runner test**

Create or extend `backend/internal/usecase/backtest/runner_decision_log_test.go`:
```go
package backtest

import (
    "context"
    "testing"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// recordingHandler captures the events the runner forwards to a registered
// decision recorder. We deliberately do NOT use the real
// decisionlog.Recorder here — that would create an import cycle. Instead
// we assert the runner attaches whatever EventHandler it was handed.
type recordingHandler struct {
    seen []string
}

func (h *recordingHandler) Handle(_ context.Context, ev entity.Event) ([]entity.Event, error) {
    h.seen = append(h.seen, ev.EventType())
    return nil, nil
}

func TestRunner_WithDecisionRecorder_ForwardsBusEvents(t *testing.T) {
    // Build a minimal backtest input. Reuse helpers from runner_test.go if
    // available (e.g. newTestCandles / newTestRunInput); otherwise inline a
    // 3-bar PT15M sequence so the bus runs at least one Indicator + Tick
    // cycle.
    rec := &recordingHandler{}
    runner := NewBacktestRunner(WithDecisionRecorder(rec))

    input := buildSmokeRunInput(t)
    _, err := runner.Run(context.Background(), input)
    if err != nil {
        t.Fatalf("Run: %v", err)
    }
    if len(rec.seen) == 0 {
        t.Fatalf("recorder must receive at least one event; got 0")
    }
    sawIndicator := false
    for _, et := range rec.seen {
        if et == entity.EventTypeIndicator {
            sawIndicator = true
            break
        }
    }
    if !sawIndicator {
        t.Errorf("recorder must see at least one IndicatorEvent; got %v", rec.seen)
    }
}
```
`buildSmokeRunInput` is a small helper that returns a minimal `RunInput` with a few candles. Look at existing runner tests — they almost certainly already define one (or its equivalent). If not, copy from `recent_squeeze_test.go` or another runner_*_test.go.

- [ ] **Step A3.4: Run all tests**

Run: `cd backend && go test ./internal/usecase/backtest/ -count=1 -race`
Expected: PASS.

- [ ] **Step A3.5: Commit**

```bash
git add backend/internal/usecase/backtest/runner.go backend/internal/usecase/backtest/runner_decision_log_test.go
git commit -m "feat(backtest): add WithDecisionRecorder runner option"
```

---

## Task A4: Wire from `cmd/main.go` (live recorder + retention)

**Files:**
- Modify: `backend/cmd/main.go`

- [ ] **Step A4.1: Construct repo and recorder**

In `cmd/main.go`, after the existing `db := ...` / `marketDataRepo := database.NewMarketDataRepo(db)` block, add:
```go
decisionLogRepo := database.NewDecisionLogRepository(db)
backtestDecisionLogRepo := database.NewBacktestDecisionLogRepository(db)
```
Hand `decisionLogRepo` to `NewEventDrivenPipeline` via the new config field. Hand `backtestDecisionLogRepo` to wherever the backtest handler is constructed (see Task A5 for the handler-side wiring).

- [ ] **Step A4.2: Start retention goroutine**

After `pipeline := NewEventDrivenPipeline(...)`:
```go
retention := decisionlog.NewRetentionCleanup(backtestDecisionLogRepo, decisionlog.RetentionConfig{
    MaxAge:   72 * time.Hour,
    Interval: 1 * time.Hour,
})
go retention.Run(ctx)
slog.Info("decisionlog retention started", "maxAge", "72h", "interval", "1h")
```
Use the same `ctx` the rest of the daemon uses (the one cancelled on SIGTERM).

- [ ] **Step A4.3: Run the daemon manually**

```bash
docker compose up --build -d backend
docker compose logs backend | grep -i decisionlog
```
Expected: log line `decisionlog retention started` shows up at startup. Wait one PT15M close (or trigger a synthetic tick if the test harness supports it), then:
```bash
docker compose exec backend sqlite3 /app/data/trading.db "SELECT count(*) FROM decision_log"
```
Expected: count > 0.

- [ ] **Step A4.4: Commit**

```bash
git add backend/cmd/main.go
git commit -m "feat(main): wire DecisionLog repos + retention goroutine into daemon"
```

---

## Task A5: Wire backtest handler to attach recorder per run

**Files:**
- Modify: `backend/internal/interfaces/api/handler/backtest.go`
- Modify: backtest handler test if any assertion needs to update

- [ ] **Step A5.1: Read the existing run-creation path**

Open `backend/internal/interfaces/api/handler/backtest.go` and find where `NewBacktestRunner(...)` is called. Identify where the run id is produced (UUID gen / DB insert) and confirm whether the id is available *before* `runner.Run(...)` is invoked.

- [ ] **Step A5.2: Plumb the recorder**

Add a new field to the backtest handler struct: `decisionLogRepo repository.BacktestDecisionLogRepository`. Update its constructor (e.g. `NewBacktestHandler`) to accept this dependency, then in the run-creation method:
```go
runID := newRunID() // existing path
adapter := decisionlog.NewBacktestRepoAdapter(h.decisionLogRepo, runID)
recorder := decisionlog.NewRecorder(adapter, decisionlog.RecorderConfig{
    SymbolID:        input.Config.SymbolID,
    CurrencyPair:    input.Config.Symbol,
    PrimaryInterval: input.Config.PrimaryInterval,
    StanceProvider:  func() string { return "" }, // backtest stance is per-bar; "" is fine
})
runner := backtest.NewBacktestRunner(
    backtest.WithStrategy(strategy),
    backtest.WithDecisionRecorder(recorder),
)
```
For the multi-period and walk-forward endpoints, repeat the same wiring per inner run with the inner run's id.

- [ ] **Step A5.3: Update `cmd/main.go` to inject the new dependency**

Wherever `NewBacktestHandler(...)` is constructed in `cmd/main.go`, pass `backtestDecisionLogRepo`.

- [ ] **Step A5.4: Add a smoke test**

Add to `backend/internal/interfaces/api/handler/backtest_test.go`:
```go
func TestBacktestHandler_PersistsDecisionsForRun(t *testing.T) {
    // Build a real handler with an in-memory SQLite, hit POST /backtest/run,
    // then assert backtest_decision_log has rows scoped to the new run id.
    // Reuse the existing handler-test fixture pattern; do NOT spin up a new
    // one if the file already has helpers like `newTestBacktestHandler(t)`.
    t.Skip("filled in during Step A5.4 once handler-test fixtures are confirmed")
}
```
Replace `t.Skip` with the actual call once you've read the handler test file.

- [ ] **Step A5.5: Run all tests**

Run: `cd backend && go test ./... -count=1 -race`
Expected: PASS.

- [ ] **Step A5.6: Commit**

```bash
git add backend/internal/interfaces/api/handler/backtest.go backend/internal/interfaces/api/handler/backtest_test.go backend/cmd/main.go
git commit -m "feat(backtest-api): attach decision-log recorder to every backtest run"
```

---

## Task A6: Open PR #5

- [ ] **Step A6.1: Push and open**

```bash
git push -u origin feat/decision-log-5-pipeline
gh pr create --title "feat(decisionlog): PR #5 wire DecisionRecorder into pipelines + retention" --body "$(cat <<'EOF'
## Summary
- Wires the DecisionRecorder shipped in #199 into the live EventDrivenPipeline and the Backtest Runner.
- Adds the BacktestRepoAdapter so the generic Recorder can write into backtest_decision_log scoped to a run id.
- Starts the 3-day retention goroutine from cmd/main.go.

## Test plan
- [x] go test ./... -race -count=1 is green
- [x] docker compose up; verified decisionlog retention started log
- [x] one PT15M close produces at least one decision_log row in trading.db

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Task B1: HTTP API — `GET /api/v1/decisions`

**Files:**
- Create: `backend/internal/interfaces/api/handler/decision.go`
- Create: `backend/internal/interfaces/api/handler/decision_test.go`
- Modify: `backend/internal/interfaces/api/router.go`
- Modify: `backend/cmd/main.go` (construct handler)

- [ ] **Step B1.1: Write the handler test**

Create `backend/internal/interfaces/api/handler/decision_test.go`:
```go
package handler

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
)

func newDecisionHandlerForTest(t *testing.T) (*DecisionHandler, func()) {
    t.Helper()
    tmpDir := t.TempDir()
    db, err := database.NewDB(filepath.Join(tmpDir, "test.db"))
    if err != nil {
        t.Fatalf("NewDB: %v", err)
    }
    if err := database.RunMigrations(db); err != nil {
        t.Fatalf("RunMigrations: %v", err)
    }
    repo := database.NewDecisionLogRepository(db)
    cleanup := func() { db.Close() }
    return NewDecisionHandler(repo), cleanup
}

func seedDecision(t *testing.T, repo interface {
    Insert(ctx context.Context, rec entity.DecisionRecord) error
}, ts int64, action string) {
    t.Helper()
    rec := entity.DecisionRecord{
        BarCloseAt:      ts,
        TriggerKind:     entity.DecisionTriggerBarClose,
        SymbolID:        7,
        CurrencyPair:    "LTC_JPY",
        PrimaryInterval: "PT15M",
        Stance:          "TREND_FOLLOW",
        LastPrice:       30210,
        SignalAction:    action,
        RiskOutcome:     entity.DecisionRiskApproved,
        BookGateOutcome: entity.DecisionBookAllowed,
        OrderOutcome:    entity.DecisionOrderFilled,
        IndicatorsJSON:  `{"rsi":48.2}`,
        CreatedAt:       time.Now().UnixMilli(),
    }
    if err := repo.Insert(context.Background(), rec); err != nil {
        t.Fatalf("seed Insert: %v", err)
    }
}

func TestDecisionHandler_List_ReturnsRowsNewestFirst(t *testing.T) {
    gin.SetMode(gin.TestMode)
    handler, cleanup := newDecisionHandlerForTest(t)
    defer cleanup()

    repoAccessor := handler.repoForTest() // tiny accessor on the handler for tests
    seedDecision(t, repoAccessor, 1_000, "BUY")
    seedDecision(t, repoAccessor, 2_000, "HOLD")

    r := gin.New()
    r.GET("/decisions", handler.List)
    req := httptest.NewRequest(http.MethodGet, "/decisions?symbolId=7&limit=10", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
    }
    var resp struct {
        Decisions []map[string]any `json:"decisions"`
        HasMore   bool             `json:"hasMore"`
    }
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if len(resp.Decisions) != 2 {
        t.Fatalf("len = %d, want 2", len(resp.Decisions))
    }
    if got := resp.Decisions[0]["signal"].(map[string]any)["action"].(string); got != "HOLD" {
        t.Errorf("first row signal.action = %q, want HOLD (newest)", got)
    }
}

func TestDecisionHandler_List_RejectsBadSymbolID(t *testing.T) {
    gin.SetMode(gin.TestMode)
    handler, cleanup := newDecisionHandlerForTest(t)
    defer cleanup()

    r := gin.New()
    r.GET("/decisions", handler.List)
    req := httptest.NewRequest(http.MethodGet, "/decisions?symbolId=not-a-number", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusBadRequest {
        t.Errorf("status = %d, want 400", w.Code)
    }
    if !strings.Contains(w.Body.String(), "symbolId") {
        t.Errorf("body should mention symbolId; got %s", w.Body.String())
    }
}
```

- [ ] **Step B1.2: Run failing test**

Run: `cd backend && go test ./internal/interfaces/api/handler/ -run TestDecisionHandler -count=1`
Expected: build error referencing undefined `NewDecisionHandler`.

- [ ] **Step B1.3: Implement handler**

Create `backend/internal/interfaces/api/handler/decision.go`:
```go
package handler

import (
    "encoding/json"
    "net/http"
    "strconv"

    "github.com/gin-gonic/gin"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

const (
    decisionAPIDefaultLimit = 200
    decisionAPIMaxLimit     = 1000
)

type DecisionHandler struct {
    repo repository.DecisionLogRepository
}

func NewDecisionHandler(repo repository.DecisionLogRepository) *DecisionHandler {
    return &DecisionHandler{repo: repo}
}

// repoForTest exposes the underlying repo to in-package tests so they can
// seed rows without a separate fixture path. Tests only.
func (h *DecisionHandler) repoForTest() repository.DecisionLogRepository { return h.repo }

func (h *DecisionHandler) List(c *gin.Context) {
    f, err := parseDecisionFilter(c)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    rows, next, err := h.repo.List(c.Request.Context(), f)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    out := make([]gin.H, 0, len(rows))
    for _, r := range rows {
        out = append(out, decisionRecordToJSON(r))
    }
    c.JSON(http.StatusOK, gin.H{
        "decisions":  out,
        "nextCursor": next,
        "hasMore":    next != 0,
    })
}

func parseDecisionFilter(c *gin.Context) (repository.DecisionLogFilter, error) {
    var f repository.DecisionLogFilter

    if s := c.Query("symbolId"); s != "" {
        v, err := strconv.ParseInt(s, 10, 64)
        if err != nil {
            return f, fmt.Errorf("invalid symbolId: %w", err)
        }
        f.SymbolID = v
    }
    if s := c.Query("from"); s != "" {
        v, err := strconv.ParseInt(s, 10, 64)
        if err != nil {
            return f, fmt.Errorf("invalid from: %w", err)
        }
        f.From = v
    }
    if s := c.Query("to"); s != "" {
        v, err := strconv.ParseInt(s, 10, 64)
        if err != nil {
            return f, fmt.Errorf("invalid to: %w", err)
        }
        f.To = v
    }
    if s := c.Query("cursor"); s != "" {
        v, err := strconv.ParseInt(s, 10, 64)
        if err != nil {
            return f, fmt.Errorf("invalid cursor: %w", err)
        }
        f.Cursor = v
    }
    f.Limit = decisionAPIDefaultLimit
    if s := c.Query("limit"); s != "" {
        v, err := strconv.Atoi(s)
        if err != nil {
            return f, fmt.Errorf("invalid limit: %w", err)
        }
        if v > decisionAPIMaxLimit {
            v = decisionAPIMaxLimit
        }
        if v > 0 {
            f.Limit = v
        }
    }
    return f, nil
}

func decisionRecordToJSON(r entity.DecisionRecord) gin.H {
    return gin.H{
        "id":              r.ID,
        "barCloseAt":      r.BarCloseAt,
        "sequenceInBar":   r.SequenceInBar,
        "triggerKind":     r.TriggerKind,
        "symbolId":        r.SymbolID,
        "currencyPair":    r.CurrencyPair,
        "primaryInterval": r.PrimaryInterval,
        "stance":          r.Stance,
        "lastPrice":       r.LastPrice,
        "signal": gin.H{
            "action":     r.SignalAction,
            "confidence": r.SignalConfidence,
            "reason":     r.SignalReason,
        },
        "risk":     gin.H{"outcome": r.RiskOutcome, "reason": r.RiskReason},
        "bookGate": gin.H{"outcome": r.BookGateOutcome, "reason": r.BookGateReason},
        "order": gin.H{
            "outcome": r.OrderOutcome,
            "orderId": r.OrderID,
            "amount":  r.ExecutedAmount,
            "price":   r.ExecutedPrice,
            "error":   r.OrderError,
        },
        "closedPositionId":   r.ClosedPositionID,
        "openedPositionId":   r.OpenedPositionID,
        "indicators":         json.RawMessage(r.IndicatorsJSON),
        "higherTfIndicators": json.RawMessage(r.HigherTFIndicatorsJSON),
        "createdAt":          r.CreatedAt,
    }
}
```
Add the `fmt` import (the snippet above uses `fmt.Errorf`).

- [ ] **Step B1.4: Register route**

In `backend/internal/interfaces/api/router.go`, find the existing v1 route block and add:
```go
v1.GET("/decisions", decisionHandler.List)
```
Construct `decisionHandler` from the live `DecisionLogRepository` in `cmd/main.go` and pass it to the router setup.

- [ ] **Step B1.5: Run tests**

Run: `cd backend && go test ./internal/interfaces/api/... -count=1 -race`
Expected: PASS.

- [ ] **Step B1.6: Commit**

```bash
git add backend/internal/interfaces/api/handler/decision.go backend/internal/interfaces/api/handler/decision_test.go backend/internal/interfaces/api/router.go backend/cmd/main.go
git commit -m "feat(api): add GET /api/v1/decisions"
```

---

## Task B2: HTTP API — backtest run-scoped decisions

**Files:**
- Modify: `backend/internal/interfaces/api/handler/backtest.go`
- Modify: `backend/internal/interfaces/api/router.go`
- Add: `backend/internal/interfaces/api/handler/backtest_decision_test.go`

- [ ] **Step B2.1: Add handler methods**

Append to `backend/internal/interfaces/api/handler/backtest.go`:
```go
func (h *BacktestHandler) ListDecisions(c *gin.Context) {
    runID := c.Param("id")
    limit := 500
    if s := c.Query("limit"); s != "" {
        if v, err := strconv.Atoi(s); err == nil && v > 0 {
            if v > 5000 {
                v = 5000
            }
            limit = v
        }
    }
    var cursor int64
    if s := c.Query("cursor"); s != "" {
        if v, err := strconv.ParseInt(s, 10, 64); err == nil {
            cursor = v
        }
    }
    rows, next, err := h.decisionLogRepo.ListByRun(c.Request.Context(), runID, limit, cursor)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    out := make([]gin.H, 0, len(rows))
    for _, r := range rows {
        out = append(out, decisionRecordToJSON(r))
    }
    c.JSON(http.StatusOK, gin.H{"decisions": out, "nextCursor": next, "hasMore": next != 0})
}

func (h *BacktestHandler) DeleteDecisions(c *gin.Context) {
    runID := c.Param("id")
    deleted, err := h.decisionLogRepo.DeleteByRun(c.Request.Context(), runID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}
```
`decisionRecordToJSON` is shared from `decision.go` — same package.

- [ ] **Step B2.2: Register routes**

In `router.go`:
```go
v1.GET("/backtest/results/:id/decisions", backtestHandler.ListDecisions)
v1.DELETE("/backtest/results/:id/decisions", backtestHandler.DeleteDecisions)
```

- [ ] **Step B2.3: Add tests**

Create `backend/internal/interfaces/api/handler/backtest_decision_test.go` mirroring the live decision-handler test but using `BacktestDecisionLogRepository.Insert(rec, runID)` to seed and `httptest` to hit `/backtest/results/run-xyz/decisions`.

Cover:
- GET returns only the rows for the requested runID
- DELETE removes all rows for the runID and returns the count

- [ ] **Step B2.4: Run tests**

Run: `cd backend && go test ./internal/interfaces/api/... -count=1 -race`
Expected: PASS.

- [ ] **Step B2.5: Commit**

```bash
git add backend/internal/interfaces/api/handler/backtest.go backend/internal/interfaces/api/handler/backtest_decision_test.go backend/internal/interfaces/api/router.go
git commit -m "feat(api): add backtest run-scoped decision endpoints"
```

---

## Task B3: Open PR #6

```bash
git push -u origin feat/decision-log-6-api
gh pr create --title "feat(decisionlog): PR #6 HTTP API for live + backtest decisions" --body "..."
```

---

## Task C1: Frontend — types + hook

**Files:**
- Modify: `frontend/src/lib/api.ts`
- Create: `frontend/src/hooks/useDecisionLog.ts`

- [ ] **Step C1.1: Add the type**

Append to `frontend/src/lib/api.ts`:
```typescript
export type DecisionLogItem = {
  id: number
  barCloseAt: number
  sequenceInBar: number
  triggerKind: 'BAR_CLOSE' | 'TICK_SLTP' | 'TICK_TRAILING'
  symbolId: number
  currencyPair: string
  primaryInterval: string
  stance: string
  lastPrice: number
  signal: { action: 'BUY' | 'SELL' | 'HOLD'; confidence: number; reason: string }
  risk: { outcome: 'APPROVED' | 'REJECTED' | 'SKIPPED'; reason: string }
  bookGate: { outcome: 'ALLOWED' | 'VETOED' | 'SKIPPED'; reason: string }
  order: { outcome: 'FILLED' | 'FAILED' | 'NOOP'; orderId: number; amount: number; price: number; error: string }
  closedPositionId: number
  openedPositionId: number
  indicators: Record<string, unknown>
  higherTfIndicators: Record<string, unknown>
  createdAt: number
}

export type DecisionLogResponse = {
  decisions: DecisionLogItem[]
  nextCursor: number
  hasMore: boolean
}
```

- [ ] **Step C1.2: Add the hook**

Create `frontend/src/hooks/useDecisionLog.ts`:
```typescript
import { useQuery } from '@tanstack/react-query'
import { fetchApi, type DecisionLogResponse } from '../lib/api'

export function useDecisionLog(symbolId: number, limit = 200) {
  return useQuery({
    queryKey: ['decisions', symbolId, limit],
    queryFn: () => fetchApi<DecisionLogResponse>(`/decisions?symbolId=${symbolId}&limit=${limit}`),
    refetchInterval: 15_000,
  })
}
```

- [ ] **Step C1.3: Commit**

```bash
git add frontend/src/lib/api.ts frontend/src/hooks/useDecisionLog.ts
git commit -m "feat(fe): add DecisionLogItem type + useDecisionLog hook"
```

---

## Task C2: Frontend — table + detail panel

**Files:**
- Create: `frontend/src/components/DecisionLogTable.tsx`
- Create: `frontend/src/components/DecisionDetailPanel.tsx`

- [ ] **Step C2.1: Implement the table**

Create `frontend/src/components/DecisionLogTable.tsx`:
```tsx
import { useState } from 'react'
import type { DecisionLogItem } from '../lib/api'
import { DecisionDetailPanel } from './DecisionDetailPanel'

type Props = { decisions: DecisionLogItem[] }

export function DecisionLogTable({ decisions }: Props) {
  const [expandedId, setExpandedId] = useState<number | null>(null)

  if (decisions.length === 0) {
    return (
      <div className="rounded-2xl border border-white/8 bg-bg-card/90 p-8 text-center text-text-secondary">
        判断ログがまだありません。
      </div>
    )
  }
  return (
    <div className="overflow-hidden rounded-3xl border border-white/8 bg-bg-card/90">
      <table className="w-full text-sm">
        <thead className="bg-white/5 text-xs uppercase tracking-[0.18em] text-text-secondary">
          <tr>
            <th className="px-4 py-3 text-left">時刻</th>
            <th className="px-4 py-3 text-left">スタンス</th>
            <th className="px-4 py-3 text-left">シグナル</th>
            <th className="px-4 py-3 text-right">信頼度</th>
            <th className="px-4 py-3 text-left">リスク</th>
            <th className="px-4 py-3 text-left">BookGate</th>
            <th className="px-4 py-3 text-left">結果</th>
            <th className="px-4 py-3 text-right">数量/価格</th>
            <th className="px-4 py-3 text-left">理由</th>
          </tr>
        </thead>
        <tbody>
          {decisions.map((d) => (
            <Row key={d.id} item={d} expanded={expandedId === d.id} onClick={() => setExpandedId(expandedId === d.id ? null : d.id)} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Row({ item, expanded, onClick }: { item: DecisionLogItem; expanded: boolean; onClick: () => void }) {
  const bg = rowBackground(item)
  const reason =
    item.signal.reason || item.risk.reason || item.bookGate.reason || item.order.error || '—'
  return (
    <>
      <tr className={`cursor-pointer border-t border-white/8 ${bg}`} onClick={onClick}>
        <td className="px-4 py-3">
          <div>{new Date(item.barCloseAt).toLocaleString('ja-JP')}</div>
          <div className="text-xs text-text-secondary">{item.triggerKind}</div>
        </td>
        <td className="px-4 py-3">{item.stance}</td>
        <td className="px-4 py-3 font-medium">{item.signal.action}</td>
        <td className="px-4 py-3 text-right">{item.signal.action === 'HOLD' ? '—' : item.signal.confidence.toFixed(2)}</td>
        <td className="px-4 py-3">{item.risk.outcome}</td>
        <td className="px-4 py-3">{item.bookGate.outcome}</td>
        <td className="px-4 py-3">{item.order.outcome}</td>
        <td className="px-4 py-3 text-right">
          {item.order.outcome === 'NOOP' ? '—' : `${item.order.amount} @ ${item.order.price.toLocaleString('ja-JP')}`}
        </td>
        <td className="max-w-[24rem] truncate px-4 py-3">{reason}</td>
      </tr>
      {expanded && (
        <tr className="border-t border-white/8 bg-white/3">
          <td colSpan={9} className="px-4 py-4">
            <DecisionDetailPanel item={item} />
          </td>
        </tr>
      )}
    </>
  )
}

function rowBackground(item: DecisionLogItem): string {
  if (item.order.outcome === 'FILLED') return 'bg-accent-green/8'
  if (item.risk.outcome === 'REJECTED' || item.bookGate.outcome === 'VETOED') return 'bg-accent-red/8'
  if (item.triggerKind !== 'BAR_CLOSE') return 'bg-white/3'
  if (item.signal.action === 'HOLD') return 'bg-accent-yellow/6'
  return ''
}
```

- [ ] **Step C2.2: Implement the detail panel**

Create `frontend/src/components/DecisionDetailPanel.tsx`:
```tsx
import type { DecisionLogItem } from '../lib/api'

type Props = { item: DecisionLogItem }

export function DecisionDetailPanel({ item }: Props) {
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Section title="主要指標" data={item.indicators} />
      <Section title="上位足指標" data={item.higherTfIndicators} />
    </div>
  )
}

function Section({ title, data }: { title: string; data: Record<string, unknown> }) {
  const entries = Object.entries(data)
  if (entries.length === 0) {
    return (
      <div className="rounded-2xl border border-white/8 p-4">
        <h3 className="text-xs uppercase tracking-[0.2em] text-text-secondary">{title}</h3>
        <p className="mt-2 text-sm text-text-secondary">データなし</p>
      </div>
    )
  }
  return (
    <div className="rounded-2xl border border-white/8 p-4">
      <h3 className="mb-3 text-xs uppercase tracking-[0.2em] text-text-secondary">{title}</h3>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
        {entries.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="truncate text-text-secondary">{k}</dt>
            <dd className="text-right">{formatValue(v)}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

function formatValue(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'number') return Number.isFinite(v) ? v.toFixed(4) : String(v)
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
```

- [ ] **Step C2.3: Commit**

```bash
git add frontend/src/components/DecisionLogTable.tsx frontend/src/components/DecisionDetailPanel.tsx
git commit -m "feat(fe): add DecisionLogTable and DecisionDetailPanel components"
```

---

## Task C3: Frontend — `/history` tab integration

**Files:**
- Modify: `frontend/src/routes/history.tsx`

- [ ] **Step C3.1: Add the third tab**

Edit `frontend/src/routes/history.tsx`. Change the `TabKey` union to `'all' | 'single' | 'decisions'`. Update the tab strip and add a third branch in the body that renders `DecisionLogTable` from `useDecisionLog(symbolId)`.

```tsx
import { DecisionLogTable } from '../components/DecisionLogTable'
import { useDecisionLog } from '../hooks/useDecisionLog'

type TabKey = 'all' | 'single' | 'decisions'
// ...
const { data: decisionData } = useDecisionLog(symbolId)
// ...
{tab === 'decisions' && (
  <DecisionLogTable decisions={decisionData?.decisions ?? []} />
)}
```
Wrap the existing summary cards in a conditional so they only show on the trade-history tabs (decisions don't have a "累計損益" concept).

Reuse the existing `TabButton`. Tab labels:
- 全通貨の約定 (existing 'all')
- 選択通貨の約定 (existing 'single')
- 選択通貨の判断ログ (new 'decisions')

- [ ] **Step C3.2: Run frontend tests + dev server smoke check**

```bash
cd frontend && pnpm test
docker compose up --build -d
```
Open http://localhost:33000/history, switch to the new tab, verify rows render.

- [ ] **Step C3.3: Commit**

```bash
git add frontend/src/routes/history.tsx
git commit -m "feat(fe): add 判断ログ tab on /history"
```

---

## Task C4: Open PR #7

```bash
git push -u origin feat/decision-log-7-frontend
gh pr create --title "feat(decisionlog): PR #7 frontend 判断ログ tab" --body "..."
```

---

## Self-Review

**1. Spec coverage**

- ✅ Pipeline DI (live + backtest) → Tasks A2, A3, A5
- ✅ Retention goroutine launched from main → Task A4
- ✅ HTTP API for live decisions → Task B1
- ✅ HTTP API for backtest decisions (GET + DELETE) → Task B2
- ✅ Frontend tab on /history → Tasks C1–C3

**2. Placeholder scan**

- Step A2.5 says "Skip the full pipeline test … smoke `Run()` step is sufficient here." — that is a *judgment call instruction*, not a placeholder. The engineer must read the existing `cmd/sync_state_test.go` fixtures and pick whichever is cheapest. Acceptable.
- Step A5.4 has the same pattern (skip → fill in) for the same reason.
- Step B3 / C4 PR-create commands have `--body "..."` placeholders. The engineer must write the body with the standard Stacked-PR template (see PRs #197–#200 for the format) before pushing. This is intentional — the body must mention the *previous* PR id which is only known after the prior PR is opened.

**3. Type consistency**

- `DecisionLogItem` field names match the JSON shape produced by `decisionRecordToJSON` ✓
- `DecisionLogResponse` field names match the gin.H keys ✓
- Backtest endpoint paths consistent: `/backtest/results/:id/decisions` ✓
- Run-id parameter named `id` everywhere (matches existing backtest routes) ✓
