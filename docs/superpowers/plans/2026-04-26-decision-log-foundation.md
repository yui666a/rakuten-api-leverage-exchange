# Decision Log Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist every 15-minute pipeline cycle (BUY/SELL/HOLD + reasons + indicator snapshot) into SQLite. This plan covers the foundation (entities, events, migrations, repositories, recorder) but stops short of pipeline DI, API, and frontend — those land in follow-up plans.

**Architecture:** A new `DecisionRecorder` registers as a priority-99 EventBus subscriber on every event type. It assembles one row per bar (or per tick-driven SL/TP close) by observing IndicatorEvent / SignalEvent / ApprovedSignalEvent / RejectedSignalEvent / OrderEvent and flushes to either `decision_log` (live) or `backtest_decision_log` (backtest, 3-day retention). Existing trading logic is untouched except for two surgical additions: `RejectedSignalEvent` is now emitted by `RiskHandler` and the BookGate path, and `OrderEvent` gains `Trigger` / `OpenedPositionID` / `ClosedPositionID` fields so the recorder can distinguish bar-close opens from tick-driven closes.

**Tech Stack:** Go 1.25, SQLite (`mattn/go-sqlite3`), `slog`, table-driven `_test.go`. Follows Clean Architecture: `domain/entity` → `domain/repository` → `infrastructure/database` → `usecase/decisionlog`.

**Spec reference:** `docs/superpowers/specs/2026-04-26-decision-log-design.md`

**Out of scope for this plan (separate plans to follow):**
- Wiring `DecisionRecorder` into `EventDrivenPipeline` and Backtest `Runner`
- HTTP API (`GET /api/v1/decisions`, backtest endpoints)
- Frontend (`/history` tab + table + detail panel)
- Stance source for live recorder (deferred until pipeline wiring plan)

---

## File Structure

### New files
- `backend/internal/domain/entity/decision.go` — `DecisionRecord` struct, outcome enum constants
- `backend/internal/domain/entity/decision_event.go` — `RejectedSignalEvent` + `EventTypeRejected` constant
- `backend/internal/domain/repository/decision_log.go` — `DecisionLogRepository` and `BacktestDecisionLogRepository` interfaces, `DecisionLogFilter` struct
- `backend/internal/infrastructure/database/decision_log_repo.go` — live repository
- `backend/internal/infrastructure/database/decision_log_repo_test.go` — repository CRUD + paging tests
- `backend/internal/infrastructure/database/backtest_decision_log_repo.go` — backtest repository (run-scoped + retention DELETE)
- `backend/internal/infrastructure/database/backtest_decision_log_repo_test.go`
- `backend/internal/usecase/decisionlog/recorder.go` — `DecisionRecorder` (EventBus subscriber)
- `backend/internal/usecase/decisionlog/recorder_test.go` — state-machine coverage
- `backend/internal/usecase/decisionlog/retention.go` — backtest 3-day retention goroutine
- `backend/internal/usecase/decisionlog/retention_test.go`

### Modified files
- `backend/internal/domain/entity/backtest_event.go` — add `EventTypeRejected` to const block, extend `OrderEvent` with `Trigger`, `OpenedPositionID`, `ClosedPositionID`
- `backend/internal/infrastructure/database/migrations.go` — append `decision_log` and `backtest_decision_log` table + index DDL to the migration list
- `backend/internal/usecase/backtest/handler.go` — `RiskHandler.Handle` and BookGate veto branches emit `RejectedSignalEvent` instead of returning `nil, nil`; `ExecutionHandler` populates new `OrderEvent` fields; `TickRiskHandler.Handle` populates new `OrderEvent` fields
- `backend/internal/infrastructure/live/real_executor.go` — populate `Trigger="BAR_CLOSE"` and `OpenedPositionID` in returned `OrderEvent`
- `backend/internal/usecase/backtest/handler_test.go` (or appropriate `*_test.go`) — assert new `OrderEvent` fields and `RejectedSignalEvent` emission

---

## Design Notes (read before coding)

### `DecisionRecord` field layout
The struct mirrors the SQL schema 1:1. Use `int64` for timestamps (unix ms), `float64` for prices/amounts. Indicator snapshots are stored as `string` (already-marshalled JSON) so the recorder marshals once and the repository writes verbatim — no double serialization, no struct coupling between entity and indicator details.

### Outcome enums (string constants in `entity/decision.go`)
```go
const (
    DecisionTriggerBarClose     = "BAR_CLOSE"
    DecisionTriggerTickSLTP     = "TICK_SLTP"
    DecisionTriggerTickTrailing = "TICK_TRAILING"

    DecisionRiskApproved = "APPROVED"
    DecisionRiskRejected = "REJECTED"
    DecisionRiskSkipped  = "SKIPPED"

    DecisionBookAllowed = "ALLOWED"
    DecisionBookVetoed  = "VETOED"
    DecisionBookSkipped = "SKIPPED"

    DecisionOrderFilled = "FILLED"
    DecisionOrderFailed = "FAILED"
    DecisionOrderNoop   = "NOOP"

    RejectedStageRisk     = "risk"
    RejectedStageBookGate = "book_gate"
)
```

### Why `OrderEvent` needs new fields
The recorder reads `OrderEvent` and must answer: "did this open a new position, close an existing one, or both (reverse)?" Currently `OrderEvent` carries `Action` ("open"/"close") on the `Side`-adjacent field but no position IDs. Adding three optional fields (`Trigger`, `OpenedPositionID`, `ClosedPositionID`) keeps the wire shape backward compatible (zero values mean "unknown / legacy"). All call-sites populate them explicitly.

### Recorder concurrency
`EventBus.Dispatch` is single-threaded per event chain (FIFO queue, no goroutines). The recorder's `pendingByBar` map therefore needs no mutex *as long as* a single recorder instance handles one symbol's pipeline. We document this invariant and add a `// not goroutine-safe; bound to one EventBus chain` comment on the struct. Multi-symbol expansion is out of scope.

### Tests use in-memory SQLite
`database/sqlite.go` already exposes a constructor; reuse the same `Open(":memory:")` pattern that `client_order_repo_test.go` uses (verify by reading that test before writing the new ones).

---

## Task 1: Add outcome enums and `DecisionRecord` entity

**Files:**
- Create: `backend/internal/domain/entity/decision.go`

- [ ] **Step 1.1: Write the failing test**

Create `backend/internal/domain/entity/decision_test.go`:
```go
package entity

import "testing"

func TestDecisionConstants_AreNonEmpty(t *testing.T) {
    cases := []struct {
        name  string
        value string
    }{
        {"DecisionTriggerBarClose", DecisionTriggerBarClose},
        {"DecisionTriggerTickSLTP", DecisionTriggerTickSLTP},
        {"DecisionTriggerTickTrailing", DecisionTriggerTickTrailing},
        {"DecisionRiskApproved", DecisionRiskApproved},
        {"DecisionRiskRejected", DecisionRiskRejected},
        {"DecisionRiskSkipped", DecisionRiskSkipped},
        {"DecisionBookAllowed", DecisionBookAllowed},
        {"DecisionBookVetoed", DecisionBookVetoed},
        {"DecisionBookSkipped", DecisionBookSkipped},
        {"DecisionOrderFilled", DecisionOrderFilled},
        {"DecisionOrderFailed", DecisionOrderFailed},
        {"DecisionOrderNoop", DecisionOrderNoop},
        {"RejectedStageRisk", RejectedStageRisk},
        {"RejectedStageBookGate", RejectedStageBookGate},
    }
    for _, c := range cases {
        if c.value == "" {
            t.Errorf("%s must not be empty", c.name)
        }
    }
}

func TestDecisionRecord_ZeroValueIsValid(t *testing.T) {
    var r DecisionRecord
    if r.SignalAction != "" || r.SequenceInBar != 0 {
        t.Errorf("zero value should be all-zero, got %+v", r)
    }
}
```

- [ ] **Step 1.2: Run test — expect compile failure**

Run: `cd backend && go test ./internal/domain/entity/ -run TestDecision -count=1`
Expected: build error referencing undefined `DecisionRecord` and constants.

- [ ] **Step 1.3: Implement `decision.go`**

Create `backend/internal/domain/entity/decision.go`:
```go
package entity

// DecisionRecord captures a single pipeline decision (BUY/SELL/HOLD plus its
// reasons and the indicator snapshot that produced it). One bar emits at
// least one row (BAR_CLOSE) and may emit additional rows for tick-driven
// SL/TP/Trailing closes (sequence_in_bar > 0).
type DecisionRecord struct {
    ID              int64
    BarCloseAt      int64  // unix ms
    SequenceInBar   int    // 0 for BAR_CLOSE, then 1, 2, ... for in-bar tick events
    TriggerKind     string // DecisionTrigger* constants

    SymbolID        int64
    CurrencyPair    string
    PrimaryInterval string

    Stance    string
    LastPrice float64

    SignalAction     string  // "BUY" | "SELL" | "HOLD"
    SignalConfidence float64
    SignalReason     string

    RiskOutcome string // DecisionRisk* constants
    RiskReason  string

    BookGateOutcome string // DecisionBook* constants
    BookGateReason  string

    OrderOutcome   string // DecisionOrder* constants
    OrderID        int64
    ExecutedAmount float64
    ExecutedPrice  float64
    OrderError     string

    ClosedPositionID int64
    OpenedPositionID int64

    IndicatorsJSON         string // already-marshalled IndicatorSet
    HigherTFIndicatorsJSON string

    CreatedAt int64
}

// Trigger kinds.
const (
    DecisionTriggerBarClose     = "BAR_CLOSE"
    DecisionTriggerTickSLTP     = "TICK_SLTP"
    DecisionTriggerTickTrailing = "TICK_TRAILING"
)

// Risk gate outcomes.
const (
    DecisionRiskApproved = "APPROVED"
    DecisionRiskRejected = "REJECTED"
    DecisionRiskSkipped  = "SKIPPED"
)

// BookGate outcomes.
const (
    DecisionBookAllowed = "ALLOWED"
    DecisionBookVetoed  = "VETOED"
    DecisionBookSkipped = "SKIPPED"
)

// Order execution outcomes.
const (
    DecisionOrderFilled = "FILLED"
    DecisionOrderFailed = "FAILED"
    DecisionOrderNoop   = "NOOP"
)

// Rejected signal stages (used by RejectedSignalEvent.Stage).
const (
    RejectedStageRisk     = "risk"
    RejectedStageBookGate = "book_gate"
)
```

- [ ] **Step 1.4: Run test — expect pass**

Run: `cd backend && go test ./internal/domain/entity/ -run TestDecision -count=1`
Expected: PASS.

- [ ] **Step 1.5: Commit**

```bash
git add backend/internal/domain/entity/decision.go backend/internal/domain/entity/decision_test.go
git commit -m "feat(entity): add DecisionRecord and outcome enums for decision log"
```

---

## Task 2: Add `RejectedSignalEvent` and extend `OrderEvent`

**Files:**
- Create: `backend/internal/domain/entity/decision_event.go`
- Modify: `backend/internal/domain/entity/backtest_event.go`

- [ ] **Step 2.1: Write the failing test**

Create `backend/internal/domain/entity/decision_event_test.go`:
```go
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
```

- [ ] **Step 2.2: Run test — expect compile failure**

Run: `cd backend && go test ./internal/domain/entity/ -run "TestRejectedSignalEvent|TestOrderEvent_New" -count=1`
Expected: build error referencing undefined `RejectedSignalEvent`, `EventTypeRejected`, and `OrderEvent.Trigger`.

- [ ] **Step 2.3: Add `EventTypeRejected` to const block**

Edit `backend/internal/domain/entity/backtest_event.go`. Replace the const block at lines 3–10:
```go
const (
    EventTypeCandle    = "candle"
    EventTypeIndicator = "indicator"
    EventTypeTick      = "tick"
    EventTypeSignal    = "signal"
    EventTypeApproved  = "approved_signal"
    EventTypeRejected  = "rejected_signal"
    EventTypeOrder     = "order"
)
```

- [ ] **Step 2.4: Extend `OrderEvent` with new fields**

Edit `backend/internal/domain/entity/backtest_event.go`. Replace the `OrderEvent` struct (around lines 88–97):
```go
type OrderEvent struct {
    OrderID   int64
    SymbolID  int64
    Side      string
    Action    string
    Price     float64
    Amount    float64
    Reason    string
    Timestamp int64
    // Trigger identifies what produced this order. Zero value means
    // "legacy / unknown" so existing call-sites that haven't been updated
    // still compile and dispatch normally. Recorder uses this to decide
    // whether the row is part of the bar's BAR_CLOSE record or a separate
    // tick-driven row.
    Trigger string
    // OpenedPositionID is set when this order opened a new position.
    OpenedPositionID int64
    // ClosedPositionID is set when this order closed an existing position
    // (set on both stand-alone closes and the close-leg of a reversal).
    ClosedPositionID int64
}
```

- [ ] **Step 2.5: Create `decision_event.go`**

Create `backend/internal/domain/entity/decision_event.go`:
```go
package entity

// RejectedSignalEvent fires when a SignalEvent is dropped before it can
// become an ApprovedSignalEvent. Stage tells the observer where in the
// pipeline the rejection happened (RejectedStageRisk / RejectedStageBookGate)
// and Reason carries the human-readable explanation copied from the
// rejecting handler. The struct exists only to give DecisionRecorder a way
// to observe rejections — the trading pipeline itself does not consume it.
type RejectedSignalEvent struct {
    Signal    Signal
    Stage     string // RejectedStageRisk | RejectedStageBookGate
    Reason    string
    Price     float64
    Timestamp int64
}

func (e RejectedSignalEvent) EventType() string     { return EventTypeRejected }
func (e RejectedSignalEvent) EventTimestamp() int64 { return e.Timestamp }
```

- [ ] **Step 2.6: Run test — expect pass**

Run: `cd backend && go test ./internal/domain/entity/ -count=1`
Expected: PASS (full package, including pre-existing tests).

- [ ] **Step 2.7: Commit**

```bash
git add backend/internal/domain/entity/decision_event.go backend/internal/domain/entity/decision_event_test.go backend/internal/domain/entity/backtest_event.go
git commit -m "feat(entity): add RejectedSignalEvent and extend OrderEvent for decision log"
```

---

## Task 3: Define repository interfaces

**Files:**
- Create: `backend/internal/domain/repository/decision_log.go`

- [ ] **Step 3.1: Write the failing test**

Create `backend/internal/domain/repository/decision_log_test.go`:
```go
package repository

import (
    "testing"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestDecisionLogFilter_ZeroValueIsAllSymbols(t *testing.T) {
    var f DecisionLogFilter
    if f.SymbolID != 0 || f.Limit != 0 {
        t.Errorf("zero value must be all-zero: %+v", f)
    }
}

// Compile-time assertion: any *fakeRepo implementer satisfies the interface.
type fakeRepo struct{}

func (fakeRepo) Insert(_ any, _ entity.DecisionRecord) error             { return nil }
func (fakeRepo) List(_ any, _ DecisionLogFilter) ([]entity.DecisionRecord, int64, error) {
    return nil, 0, nil
}

// We assert via a typed nil to avoid an unused-import rather than a real
// fake; the goal is to lock the interface signature, not to test behaviour.
func TestDecisionLogRepository_InterfaceShape(t *testing.T) {
    var _ DecisionLogRepository = (*minimalRepo)(nil)
    var _ BacktestDecisionLogRepository = (*minimalBacktestRepo)(nil)
}
```

Then add the minimal implementers below in the same test file:
```go
type minimalRepo struct{}

func (*minimalRepo) Insert(_ any, _ entity.DecisionRecord) error { return nil }
func (*minimalRepo) List(_ any, _ DecisionLogFilter) ([]entity.DecisionRecord, int64, error) {
    return nil, 0, nil
}

type minimalBacktestRepo struct{}

func (*minimalBacktestRepo) Insert(_ any, _ entity.DecisionRecord, _ string) error { return nil }
func (*minimalBacktestRepo) ListByRun(_ any, _ string, _ int, _ int64) ([]entity.DecisionRecord, int64, error) {
    return nil, 0, nil
}
func (*minimalBacktestRepo) DeleteByRun(_ any, _ string) (int64, error)   { return 0, nil }
func (*minimalBacktestRepo) DeleteOlderThan(_ any, _ int64) (int64, error) { return 0, nil }
```

Note: `_ any` for `ctx` keeps the test file independent of the `context` import; we'll use `context.Context` in the real interface and adjust the test imports accordingly. Replace `any` with `context.Context` and add the `context` import in the next step.

Update the test to use `context.Context`:
```go
package repository

import (
    "context"
    "testing"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type minimalRepo struct{}

func (*minimalRepo) Insert(_ context.Context, _ entity.DecisionRecord) error { return nil }
func (*minimalRepo) List(_ context.Context, _ DecisionLogFilter) ([]entity.DecisionRecord, int64, error) {
    return nil, 0, nil
}

type minimalBacktestRepo struct{}

func (*minimalBacktestRepo) Insert(_ context.Context, _ entity.DecisionRecord, _ string) error {
    return nil
}
func (*minimalBacktestRepo) ListByRun(_ context.Context, _ string, _ int, _ int64) ([]entity.DecisionRecord, int64, error) {
    return nil, 0, nil
}
func (*minimalBacktestRepo) DeleteByRun(_ context.Context, _ string) (int64, error) { return 0, nil }
func (*minimalBacktestRepo) DeleteOlderThan(_ context.Context, _ int64) (int64, error) {
    return 0, nil
}

func TestDecisionLogFilter_ZeroValueIsAllSymbols(t *testing.T) {
    var f DecisionLogFilter
    if f.SymbolID != 0 || f.Limit != 0 {
        t.Errorf("zero value must be all-zero: %+v", f)
    }
}

func TestDecisionLogRepository_InterfaceShape(t *testing.T) {
    var _ DecisionLogRepository = (*minimalRepo)(nil)
    var _ BacktestDecisionLogRepository = (*minimalBacktestRepo)(nil)
}
```

- [ ] **Step 3.2: Run test — expect compile failure**

Run: `cd backend && go test ./internal/domain/repository/ -run "TestDecisionLog" -count=1`
Expected: build error referencing undefined `DecisionLogRepository` etc.

- [ ] **Step 3.3: Implement `decision_log.go`**

Create `backend/internal/domain/repository/decision_log.go`:
```go
package repository

import (
    "context"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// DecisionLogFilter narrows a List query. Zero values mean "no filter":
//   - SymbolID == 0    -> all symbols
//   - From == 0        -> no lower bound
//   - To == 0          -> no upper bound
//   - Cursor == 0      -> latest page
//   - Limit <= 0       -> repository default
type DecisionLogFilter struct {
    SymbolID int64
    From     int64 // unix ms inclusive
    To       int64 // unix ms inclusive
    Cursor   int64 // returns rows with id < Cursor
    Limit    int
}

// DecisionLogRepository persists live-pipeline decisions. Implementations
// must be safe for concurrent reads but a single recorder writes serially.
type DecisionLogRepository interface {
    Insert(ctx context.Context, record entity.DecisionRecord) error
    // List returns rows newest-first along with the next cursor (the id of
    // the oldest row in the page, suitable as Cursor for the next call).
    // nextCursor == 0 means "no more rows".
    List(ctx context.Context, filter DecisionLogFilter) (records []entity.DecisionRecord, nextCursor int64, err error)
}

// BacktestDecisionLogRepository scopes records to a backtest run id and
// supports retention sweeping. Insert ties each record to runID; ListByRun
// returns the run's rows newest-first; Delete* enables both immediate and
// scheduled cleanup.
type BacktestDecisionLogRepository interface {
    Insert(ctx context.Context, record entity.DecisionRecord, runID string) error
    ListByRun(ctx context.Context, runID string, limit int, cursor int64) (records []entity.DecisionRecord, nextCursor int64, err error)
    DeleteByRun(ctx context.Context, runID string) (deleted int64, err error)
    DeleteOlderThan(ctx context.Context, cutoff int64) (deleted int64, err error)
}
```

- [ ] **Step 3.4: Run test — expect pass**

Run: `cd backend && go test ./internal/domain/repository/ -run "TestDecisionLog" -count=1`
Expected: PASS.

- [ ] **Step 3.5: Commit**

```bash
git add backend/internal/domain/repository/decision_log.go backend/internal/domain/repository/decision_log_test.go
git commit -m "feat(repository): define DecisionLogRepository interfaces"
```

---

## Task 4: Add migrations for `decision_log` and `backtest_decision_log`

**Files:**
- Modify: `backend/internal/infrastructure/database/migrations.go`
- Modify: `backend/internal/infrastructure/database/migrations_test.go`

- [ ] **Step 4.1: Read the existing migration list**

Open `backend/internal/infrastructure/database/migrations.go` and confirm the migration list ends with `backtest_trades` / `multi_period_results` / `walk_forward_results`. Append the new tables at the end of the same `[]string` slice (preserving the existing order so re-running on existing DBs only adds the new statements).

- [ ] **Step 4.2: Write the failing test**

Add to `backend/internal/infrastructure/database/migrations_test.go`:
```go
func TestMigrations_DecisionLogTablesExist(t *testing.T) {
    db := openMemoryDB(t)
    if err := Migrate(db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    for _, table := range []string{"decision_log", "backtest_decision_log"} {
        var name string
        err := db.QueryRow(
            `SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
            table,
        ).Scan(&name)
        if err != nil {
            t.Errorf("table %s not found: %v", table, err)
        }
    }
}

func TestMigrations_DecisionLogIndexesExist(t *testing.T) {
    db := openMemoryDB(t)
    if err := Migrate(db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    for _, idx := range []string{
        "idx_decision_log_symbol_time",
        "idx_decision_log_created",
        "idx_backtest_decision_log_run",
        "idx_backtest_decision_log_created",
    } {
        var name string
        err := db.QueryRow(
            `SELECT name FROM sqlite_master WHERE type='index' AND name=?`,
            idx,
        ).Scan(&name)
        if err != nil {
            t.Errorf("index %s not found: %v", idx, err)
        }
    }
}
```

If `openMemoryDB` doesn't already exist in the test file, copy the helper from any other `_test.go` in the same package (e.g. `client_order_repo_test.go`) — do NOT invent a new pattern.

- [ ] **Step 4.3: Run test — expect fail**

Run: `cd backend && go test ./internal/infrastructure/database/ -run "TestMigrations_DecisionLog" -count=1`
Expected: FAIL with "table decision_log not found".

- [ ] **Step 4.4: Append migrations**

Add to the end of the migration `[]string` in `migrations.go` (immediately before the closing `}`):
```go
        `CREATE TABLE IF NOT EXISTS decision_log (
            id              INTEGER PRIMARY KEY AUTOINCREMENT,
            bar_close_at    INTEGER NOT NULL,
            sequence_in_bar INTEGER NOT NULL DEFAULT 0,
            trigger_kind    TEXT    NOT NULL,
            symbol_id        INTEGER NOT NULL,
            currency_pair    TEXT    NOT NULL,
            primary_interval TEXT    NOT NULL,
            stance          TEXT    NOT NULL,
            last_price      REAL    NOT NULL,
            signal_action     TEXT NOT NULL,
            signal_confidence REAL NOT NULL DEFAULT 0,
            signal_reason     TEXT NOT NULL DEFAULT '',
            risk_outcome    TEXT NOT NULL,
            risk_reason     TEXT NOT NULL DEFAULT '',
            book_gate_outcome TEXT NOT NULL DEFAULT 'SKIPPED',
            book_gate_reason  TEXT NOT NULL DEFAULT '',
            order_outcome    TEXT    NOT NULL,
            order_id         INTEGER NOT NULL DEFAULT 0,
            executed_amount  REAL    NOT NULL DEFAULT 0,
            executed_price   REAL    NOT NULL DEFAULT 0,
            order_error      TEXT    NOT NULL DEFAULT '',
            closed_position_id INTEGER NOT NULL DEFAULT 0,
            opened_position_id INTEGER NOT NULL DEFAULT 0,
            indicators_json           TEXT NOT NULL DEFAULT '{}',
            higher_tf_indicators_json TEXT NOT NULL DEFAULT '{}',
            created_at      INTEGER NOT NULL
        )`,
        `CREATE INDEX IF NOT EXISTS idx_decision_log_symbol_time
            ON decision_log(symbol_id, bar_close_at DESC, sequence_in_bar)`,
        `CREATE INDEX IF NOT EXISTS idx_decision_log_created
            ON decision_log(created_at)`,

        `CREATE TABLE IF NOT EXISTS backtest_decision_log (
            id               INTEGER PRIMARY KEY AUTOINCREMENT,
            backtest_run_id  TEXT    NOT NULL,
            bar_close_at     INTEGER NOT NULL,
            sequence_in_bar  INTEGER NOT NULL DEFAULT 0,
            trigger_kind     TEXT    NOT NULL,
            symbol_id        INTEGER NOT NULL,
            currency_pair    TEXT    NOT NULL,
            primary_interval TEXT    NOT NULL,
            stance           TEXT    NOT NULL,
            last_price       REAL    NOT NULL,
            signal_action     TEXT NOT NULL,
            signal_confidence REAL NOT NULL DEFAULT 0,
            signal_reason     TEXT NOT NULL DEFAULT '',
            risk_outcome      TEXT NOT NULL,
            risk_reason       TEXT NOT NULL DEFAULT '',
            book_gate_outcome TEXT NOT NULL DEFAULT 'SKIPPED',
            book_gate_reason  TEXT NOT NULL DEFAULT '',
            order_outcome     TEXT    NOT NULL,
            order_id          INTEGER NOT NULL DEFAULT 0,
            executed_amount   REAL    NOT NULL DEFAULT 0,
            executed_price    REAL    NOT NULL DEFAULT 0,
            order_error       TEXT    NOT NULL DEFAULT '',
            closed_position_id INTEGER NOT NULL DEFAULT 0,
            opened_position_id INTEGER NOT NULL DEFAULT 0,
            indicators_json           TEXT NOT NULL DEFAULT '{}',
            higher_tf_indicators_json TEXT NOT NULL DEFAULT '{}',
            created_at       INTEGER NOT NULL
        )`,
        `CREATE INDEX IF NOT EXISTS idx_backtest_decision_log_run
            ON backtest_decision_log(backtest_run_id, bar_close_at, sequence_in_bar)`,
        `CREATE INDEX IF NOT EXISTS idx_backtest_decision_log_created
            ON backtest_decision_log(created_at)`,
```

- [ ] **Step 4.5: Run test — expect pass**

Run: `cd backend && go test ./internal/infrastructure/database/ -run "TestMigrations" -count=1`
Expected: PASS (including the new `TestMigrations_DecisionLogTablesExist` and `TestMigrations_DecisionLogIndexesExist`).

- [ ] **Step 4.6: Run full migration test suite**

Run: `cd backend && go test ./internal/infrastructure/database/ -count=1 -race`
Expected: PASS.

- [ ] **Step 4.7: Commit**

```bash
git add backend/internal/infrastructure/database/migrations.go backend/internal/infrastructure/database/migrations_test.go
git commit -m "feat(db): add decision_log and backtest_decision_log migrations"
```

---

## Task 5: Implement live `decisionLogRepo`

**Files:**
- Create: `backend/internal/infrastructure/database/decision_log_repo.go`
- Create: `backend/internal/infrastructure/database/decision_log_repo_test.go`

- [ ] **Step 5.1: Read the existing repo pattern**

Open `backend/internal/infrastructure/database/client_order_repo.go` and `client_order_repo_test.go`. Note:
- Constructor name pattern (e.g. `NewClientOrderRepository(db *sql.DB)`)
- How `*sql.DB` is wrapped
- How tests call `openMemoryDB` + `Migrate`
- How `time.Now().UnixMilli()` is used for timestamps

Mirror these patterns exactly.

- [ ] **Step 5.2: Write the failing test**

Create `backend/internal/infrastructure/database/decision_log_repo_test.go`:
```go
package database

import (
    "context"
    "testing"
    "time"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

func newDecisionRecord(t *testing.T, symbolID int64, barTs int64, seq int) entity.DecisionRecord {
    t.Helper()
    return entity.DecisionRecord{
        BarCloseAt:       barTs,
        SequenceInBar:    seq,
        TriggerKind:      entity.DecisionTriggerBarClose,
        SymbolID:         symbolID,
        CurrencyPair:     "LTC_JPY",
        PrimaryInterval:  "PT15M",
        Stance:           "TREND_FOLLOW",
        LastPrice:        30210,
        SignalAction:     "HOLD",
        SignalConfidence: 0,
        SignalReason:     "trend follow: ADX below threshold",
        RiskOutcome:      entity.DecisionRiskSkipped,
        BookGateOutcome:  entity.DecisionBookSkipped,
        OrderOutcome:     entity.DecisionOrderNoop,
        IndicatorsJSON:   `{"rsi":48.2}`,
        CreatedAt:        time.Now().UnixMilli(),
    }
}

func TestDecisionLogRepo_InsertAndList(t *testing.T) {
    db := openMemoryDB(t)
    if err := Migrate(db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    repo := NewDecisionLogRepository(db)
    ctx := context.Background()

    base := int64(1745654700000)
    for i := 0; i < 3; i++ {
        rec := newDecisionRecord(t, 7, base+int64(i)*900_000, 0)
        if err := repo.Insert(ctx, rec); err != nil {
            t.Fatalf("Insert[%d]: %v", i, err)
        }
    }

    rows, next, err := repo.List(ctx, repository.DecisionLogFilter{SymbolID: 7, Limit: 10})
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(rows) != 3 {
        t.Fatalf("List len = %d, want 3", len(rows))
    }
    if rows[0].BarCloseAt < rows[1].BarCloseAt {
        t.Errorf("rows must be newest first, got %d before %d", rows[0].BarCloseAt, rows[1].BarCloseAt)
    }
    if next != 0 {
        t.Errorf("nextCursor must be 0 when fewer rows than limit, got %d", next)
    }
}

func TestDecisionLogRepo_CursorPaging(t *testing.T) {
    db := openMemoryDB(t)
    if err := Migrate(db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    repo := NewDecisionLogRepository(db)
    ctx := context.Background()

    base := int64(1745654700000)
    for i := 0; i < 5; i++ {
        rec := newDecisionRecord(t, 7, base+int64(i)*900_000, 0)
        if err := repo.Insert(ctx, rec); err != nil {
            t.Fatalf("Insert[%d]: %v", i, err)
        }
    }

    page1, next1, err := repo.List(ctx, repository.DecisionLogFilter{Limit: 2})
    if err != nil {
        t.Fatalf("List page1: %v", err)
    }
    if len(page1) != 2 || next1 == 0 {
        t.Fatalf("page1 len=%d next=%d (want 2 / non-zero)", len(page1), next1)
    }

    page2, _, err := repo.List(ctx, repository.DecisionLogFilter{Limit: 10, Cursor: next1})
    if err != nil {
        t.Fatalf("List page2: %v", err)
    }
    if len(page2) != 3 {
        t.Errorf("page2 len = %d, want 3 (5 total - 2 on page1)", len(page2))
    }
    for _, r := range page2 {
        if r.ID >= next1 {
            t.Errorf("page2 row id %d must be < cursor %d", r.ID, next1)
        }
    }
}

func TestDecisionLogRepo_FilterByTimeRange(t *testing.T) {
    db := openMemoryDB(t)
    if err := Migrate(db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    repo := NewDecisionLogRepository(db)
    ctx := context.Background()

    base := int64(1745654700000)
    for i := 0; i < 5; i++ {
        rec := newDecisionRecord(t, 7, base+int64(i)*900_000, 0)
        if err := repo.Insert(ctx, rec); err != nil {
            t.Fatalf("Insert[%d]: %v", i, err)
        }
    }

    rows, _, err := repo.List(ctx, repository.DecisionLogFilter{
        From:  base + 900_000,
        To:    base + 3*900_000,
        Limit: 10,
    })
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(rows) != 3 {
        t.Errorf("len = %d, want 3 (inclusive on both ends)", len(rows))
    }
}
```

- [ ] **Step 5.3: Run test — expect compile failure**

Run: `cd backend && go test ./internal/infrastructure/database/ -run "TestDecisionLogRepo" -count=1`
Expected: build error referencing undefined `NewDecisionLogRepository`.

- [ ] **Step 5.4: Implement the repo**

Create `backend/internal/infrastructure/database/decision_log_repo.go`:
```go
package database

import (
    "context"
    "database/sql"
    "fmt"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

const decisionLogDefaultLimit = 200

// decisionLogRepo persists DecisionRecord rows into the live `decision_log`
// table. Read-side methods order newest-first by id (autoincrement matches
// insertion order, which matches creation time for a single-writer pipeline).
type decisionLogRepo struct {
    db *sql.DB
}

// NewDecisionLogRepository returns a repository.DecisionLogRepository backed
// by the given *sql.DB. The DB must already have the `decision_log` table.
func NewDecisionLogRepository(db *sql.DB) repository.DecisionLogRepository {
    return &decisionLogRepo{db: db}
}

func (r *decisionLogRepo) Insert(ctx context.Context, rec entity.DecisionRecord) error {
    const q = `
        INSERT INTO decision_log (
            bar_close_at, sequence_in_bar, trigger_kind,
            symbol_id, currency_pair, primary_interval,
            stance, last_price,
            signal_action, signal_confidence, signal_reason,
            risk_outcome, risk_reason,
            book_gate_outcome, book_gate_reason,
            order_outcome, order_id, executed_amount, executed_price, order_error,
            closed_position_id, opened_position_id,
            indicators_json, higher_tf_indicators_json,
            created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `
    if _, err := r.db.ExecContext(ctx, q,
        rec.BarCloseAt, rec.SequenceInBar, rec.TriggerKind,
        rec.SymbolID, rec.CurrencyPair, rec.PrimaryInterval,
        rec.Stance, rec.LastPrice,
        rec.SignalAction, rec.SignalConfidence, rec.SignalReason,
        rec.RiskOutcome, rec.RiskReason,
        rec.BookGateOutcome, rec.BookGateReason,
        rec.OrderOutcome, rec.OrderID, rec.ExecutedAmount, rec.ExecutedPrice, rec.OrderError,
        rec.ClosedPositionID, rec.OpenedPositionID,
        rec.IndicatorsJSON, rec.HigherTFIndicatorsJSON,
        rec.CreatedAt,
    ); err != nil {
        return fmt.Errorf("decision_log insert: %w", err)
    }
    return nil
}

func (r *decisionLogRepo) List(ctx context.Context, f repository.DecisionLogFilter) ([]entity.DecisionRecord, int64, error) {
    limit := f.Limit
    if limit <= 0 {
        limit = decisionLogDefaultLimit
    }

    args := make([]any, 0, 5)
    where := "1=1"
    if f.SymbolID > 0 {
        where += " AND symbol_id = ?"
        args = append(args, f.SymbolID)
    }
    if f.From > 0 {
        where += " AND bar_close_at >= ?"
        args = append(args, f.From)
    }
    if f.To > 0 {
        where += " AND bar_close_at <= ?"
        args = append(args, f.To)
    }
    if f.Cursor > 0 {
        where += " AND id < ?"
        args = append(args, f.Cursor)
    }
    args = append(args, limit)

    q := fmt.Sprintf(`
        SELECT id, bar_close_at, sequence_in_bar, trigger_kind,
               symbol_id, currency_pair, primary_interval,
               stance, last_price,
               signal_action, signal_confidence, signal_reason,
               risk_outcome, risk_reason,
               book_gate_outcome, book_gate_reason,
               order_outcome, order_id, executed_amount, executed_price, order_error,
               closed_position_id, opened_position_id,
               indicators_json, higher_tf_indicators_json,
               created_at
        FROM decision_log
        WHERE %s
        ORDER BY id DESC
        LIMIT ?
    `, where)

    rows, err := r.db.QueryContext(ctx, q, args...)
    if err != nil {
        return nil, 0, fmt.Errorf("decision_log list: %w", err)
    }
    defer rows.Close()

    out := make([]entity.DecisionRecord, 0, limit)
    for rows.Next() {
        var rec entity.DecisionRecord
        if err := rows.Scan(
            &rec.ID, &rec.BarCloseAt, &rec.SequenceInBar, &rec.TriggerKind,
            &rec.SymbolID, &rec.CurrencyPair, &rec.PrimaryInterval,
            &rec.Stance, &rec.LastPrice,
            &rec.SignalAction, &rec.SignalConfidence, &rec.SignalReason,
            &rec.RiskOutcome, &rec.RiskReason,
            &rec.BookGateOutcome, &rec.BookGateReason,
            &rec.OrderOutcome, &rec.OrderID, &rec.ExecutedAmount, &rec.ExecutedPrice, &rec.OrderError,
            &rec.ClosedPositionID, &rec.OpenedPositionID,
            &rec.IndicatorsJSON, &rec.HigherTFIndicatorsJSON,
            &rec.CreatedAt,
        ); err != nil {
            return nil, 0, fmt.Errorf("decision_log scan: %w", err)
        }
        out = append(out, rec)
    }
    if err := rows.Err(); err != nil {
        return nil, 0, fmt.Errorf("decision_log rows: %w", err)
    }

    var next int64
    if len(out) == limit {
        next = out[len(out)-1].ID
    }
    return out, next, nil
}
```

- [ ] **Step 5.5: Run test — expect pass**

Run: `cd backend && go test ./internal/infrastructure/database/ -run "TestDecisionLogRepo" -count=1 -race`
Expected: PASS.

- [ ] **Step 5.6: Commit**

```bash
git add backend/internal/infrastructure/database/decision_log_repo.go backend/internal/infrastructure/database/decision_log_repo_test.go
git commit -m "feat(db): add live decisionLogRepo with cursor paging and time filter"
```

---

## Task 6: Implement `backtestDecisionLogRepo`

**Files:**
- Create: `backend/internal/infrastructure/database/backtest_decision_log_repo.go`
- Create: `backend/internal/infrastructure/database/backtest_decision_log_repo_test.go`

- [ ] **Step 6.1: Write the failing test**

Create `backend/internal/infrastructure/database/backtest_decision_log_repo_test.go`:
```go
package database

import (
    "context"
    "testing"
    "time"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestBacktestDecisionLogRepo_InsertAndListByRun(t *testing.T) {
    db := openMemoryDB(t)
    if err := Migrate(db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    repo := NewBacktestDecisionLogRepository(db)
    ctx := context.Background()

    runA := "run-aaa"
    runB := "run-bbb"
    base := int64(1745654700000)

    for _, runID := range []string{runA, runA, runB} {
        rec := entity.DecisionRecord{
            BarCloseAt:      base,
            TriggerKind:     entity.DecisionTriggerBarClose,
            SymbolID:        7,
            CurrencyPair:    "LTC_JPY",
            PrimaryInterval: "PT15M",
            Stance:          "HOLD",
            LastPrice:       30210,
            SignalAction:    "HOLD",
            RiskOutcome:     entity.DecisionRiskSkipped,
            BookGateOutcome: entity.DecisionBookSkipped,
            OrderOutcome:    entity.DecisionOrderNoop,
            CreatedAt:       time.Now().UnixMilli(),
        }
        if err := repo.Insert(ctx, rec, runID); err != nil {
            t.Fatalf("Insert: %v", err)
        }
    }

    rowsA, _, err := repo.ListByRun(ctx, runA, 100, 0)
    if err != nil {
        t.Fatalf("ListByRun A: %v", err)
    }
    if len(rowsA) != 2 {
        t.Errorf("runA rows = %d, want 2", len(rowsA))
    }
    rowsB, _, err := repo.ListByRun(ctx, runB, 100, 0)
    if err != nil {
        t.Fatalf("ListByRun B: %v", err)
    }
    if len(rowsB) != 1 {
        t.Errorf("runB rows = %d, want 1", len(rowsB))
    }
}

func TestBacktestDecisionLogRepo_DeleteByRun(t *testing.T) {
    db := openMemoryDB(t)
    if err := Migrate(db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    repo := NewBacktestDecisionLogRepository(db)
    ctx := context.Background()

    runA := "run-aaa"
    runB := "run-bbb"
    rec := entity.DecisionRecord{
        BarCloseAt: 1, TriggerKind: entity.DecisionTriggerBarClose, SymbolID: 7,
        CurrencyPair: "LTC_JPY", PrimaryInterval: "PT15M", Stance: "HOLD",
        LastPrice: 1, SignalAction: "HOLD",
        RiskOutcome: entity.DecisionRiskSkipped, BookGateOutcome: entity.DecisionBookSkipped,
        OrderOutcome: entity.DecisionOrderNoop, CreatedAt: 1,
    }
    for i := 0; i < 5; i++ {
        if err := repo.Insert(ctx, rec, runA); err != nil {
            t.Fatalf("Insert A: %v", err)
        }
    }
    if err := repo.Insert(ctx, rec, runB); err != nil {
        t.Fatalf("Insert B: %v", err)
    }

    deleted, err := repo.DeleteByRun(ctx, runA)
    if err != nil {
        t.Fatalf("DeleteByRun: %v", err)
    }
    if deleted != 5 {
        t.Errorf("deleted = %d, want 5", deleted)
    }
    rowsA, _, _ := repo.ListByRun(ctx, runA, 10, 0)
    if len(rowsA) != 0 {
        t.Errorf("runA rows after delete = %d, want 0", len(rowsA))
    }
    rowsB, _, _ := repo.ListByRun(ctx, runB, 10, 0)
    if len(rowsB) != 1 {
        t.Errorf("runB rows after delete = %d, want 1 (untouched)", len(rowsB))
    }
}

func TestBacktestDecisionLogRepo_DeleteOlderThan(t *testing.T) {
    db := openMemoryDB(t)
    if err := Migrate(db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    repo := NewBacktestDecisionLogRepository(db)
    ctx := context.Background()

    now := time.Now().UnixMilli()
    threeDays := int64(3 * 24 * 60 * 60 * 1000)

    old := entity.DecisionRecord{
        BarCloseAt: now - 2*threeDays, TriggerKind: entity.DecisionTriggerBarClose,
        SymbolID: 7, CurrencyPair: "LTC_JPY", PrimaryInterval: "PT15M",
        Stance: "HOLD", LastPrice: 1, SignalAction: "HOLD",
        RiskOutcome: entity.DecisionRiskSkipped, BookGateOutcome: entity.DecisionBookSkipped,
        OrderOutcome: entity.DecisionOrderNoop, CreatedAt: now - 2*threeDays,
    }
    fresh := old
    fresh.CreatedAt = now
    fresh.BarCloseAt = now

    if err := repo.Insert(ctx, old, "run-old"); err != nil {
        t.Fatalf("Insert old: %v", err)
    }
    if err := repo.Insert(ctx, fresh, "run-fresh"); err != nil {
        t.Fatalf("Insert fresh: %v", err)
    }

    deleted, err := repo.DeleteOlderThan(ctx, now-threeDays)
    if err != nil {
        t.Fatalf("DeleteOlderThan: %v", err)
    }
    if deleted != 1 {
        t.Errorf("deleted = %d, want 1", deleted)
    }

    rowsFresh, _, _ := repo.ListByRun(ctx, "run-fresh", 10, 0)
    if len(rowsFresh) != 1 {
        t.Errorf("fresh rows = %d, want 1", len(rowsFresh))
    }
    rowsOld, _, _ := repo.ListByRun(ctx, "run-old", 10, 0)
    if len(rowsOld) != 0 {
        t.Errorf("old rows = %d, want 0", len(rowsOld))
    }
}
```

- [ ] **Step 6.2: Run test — expect compile failure**

Run: `cd backend && go test ./internal/infrastructure/database/ -run "TestBacktestDecisionLogRepo" -count=1`
Expected: build error referencing undefined `NewBacktestDecisionLogRepository`.

- [ ] **Step 6.3: Implement the repo**

Create `backend/internal/infrastructure/database/backtest_decision_log_repo.go`:
```go
package database

import (
    "context"
    "database/sql"
    "fmt"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

const backtestDecisionLogDefaultLimit = 500

type backtestDecisionLogRepo struct {
    db *sql.DB
}

func NewBacktestDecisionLogRepository(db *sql.DB) repository.BacktestDecisionLogRepository {
    return &backtestDecisionLogRepo{db: db}
}

func (r *backtestDecisionLogRepo) Insert(ctx context.Context, rec entity.DecisionRecord, runID string) error {
    const q = `
        INSERT INTO backtest_decision_log (
            backtest_run_id,
            bar_close_at, sequence_in_bar, trigger_kind,
            symbol_id, currency_pair, primary_interval,
            stance, last_price,
            signal_action, signal_confidence, signal_reason,
            risk_outcome, risk_reason,
            book_gate_outcome, book_gate_reason,
            order_outcome, order_id, executed_amount, executed_price, order_error,
            closed_position_id, opened_position_id,
            indicators_json, higher_tf_indicators_json,
            created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `
    if _, err := r.db.ExecContext(ctx, q,
        runID,
        rec.BarCloseAt, rec.SequenceInBar, rec.TriggerKind,
        rec.SymbolID, rec.CurrencyPair, rec.PrimaryInterval,
        rec.Stance, rec.LastPrice,
        rec.SignalAction, rec.SignalConfidence, rec.SignalReason,
        rec.RiskOutcome, rec.RiskReason,
        rec.BookGateOutcome, rec.BookGateReason,
        rec.OrderOutcome, rec.OrderID, rec.ExecutedAmount, rec.ExecutedPrice, rec.OrderError,
        rec.ClosedPositionID, rec.OpenedPositionID,
        rec.IndicatorsJSON, rec.HigherTFIndicatorsJSON,
        rec.CreatedAt,
    ); err != nil {
        return fmt.Errorf("backtest_decision_log insert: %w", err)
    }
    return nil
}

func (r *backtestDecisionLogRepo) ListByRun(ctx context.Context, runID string, limit int, cursor int64) ([]entity.DecisionRecord, int64, error) {
    if limit <= 0 {
        limit = backtestDecisionLogDefaultLimit
    }
    args := []any{runID}
    where := "backtest_run_id = ?"
    if cursor > 0 {
        where += " AND id < ?"
        args = append(args, cursor)
    }
    args = append(args, limit)

    q := fmt.Sprintf(`
        SELECT id, bar_close_at, sequence_in_bar, trigger_kind,
               symbol_id, currency_pair, primary_interval,
               stance, last_price,
               signal_action, signal_confidence, signal_reason,
               risk_outcome, risk_reason,
               book_gate_outcome, book_gate_reason,
               order_outcome, order_id, executed_amount, executed_price, order_error,
               closed_position_id, opened_position_id,
               indicators_json, higher_tf_indicators_json,
               created_at
        FROM backtest_decision_log
        WHERE %s
        ORDER BY id DESC
        LIMIT ?
    `, where)

    rows, err := r.db.QueryContext(ctx, q, args...)
    if err != nil {
        return nil, 0, fmt.Errorf("backtest_decision_log list: %w", err)
    }
    defer rows.Close()

    out := make([]entity.DecisionRecord, 0, limit)
    for rows.Next() {
        var rec entity.DecisionRecord
        if err := rows.Scan(
            &rec.ID, &rec.BarCloseAt, &rec.SequenceInBar, &rec.TriggerKind,
            &rec.SymbolID, &rec.CurrencyPair, &rec.PrimaryInterval,
            &rec.Stance, &rec.LastPrice,
            &rec.SignalAction, &rec.SignalConfidence, &rec.SignalReason,
            &rec.RiskOutcome, &rec.RiskReason,
            &rec.BookGateOutcome, &rec.BookGateReason,
            &rec.OrderOutcome, &rec.OrderID, &rec.ExecutedAmount, &rec.ExecutedPrice, &rec.OrderError,
            &rec.ClosedPositionID, &rec.OpenedPositionID,
            &rec.IndicatorsJSON, &rec.HigherTFIndicatorsJSON,
            &rec.CreatedAt,
        ); err != nil {
            return nil, 0, fmt.Errorf("backtest_decision_log scan: %w", err)
        }
        out = append(out, rec)
    }
    if err := rows.Err(); err != nil {
        return nil, 0, fmt.Errorf("backtest_decision_log rows: %w", err)
    }

    var next int64
    if len(out) == limit {
        next = out[len(out)-1].ID
    }
    return out, next, nil
}

func (r *backtestDecisionLogRepo) DeleteByRun(ctx context.Context, runID string) (int64, error) {
    res, err := r.db.ExecContext(ctx, `DELETE FROM backtest_decision_log WHERE backtest_run_id = ?`, runID)
    if err != nil {
        return 0, fmt.Errorf("backtest_decision_log delete by run: %w", err)
    }
    n, err := res.RowsAffected()
    if err != nil {
        return 0, fmt.Errorf("backtest_decision_log rows affected: %w", err)
    }
    return n, nil
}

func (r *backtestDecisionLogRepo) DeleteOlderThan(ctx context.Context, cutoff int64) (int64, error) {
    res, err := r.db.ExecContext(ctx, `DELETE FROM backtest_decision_log WHERE created_at < ?`, cutoff)
    if err != nil {
        return 0, fmt.Errorf("backtest_decision_log delete older: %w", err)
    }
    n, err := res.RowsAffected()
    if err != nil {
        return 0, fmt.Errorf("backtest_decision_log rows affected: %w", err)
    }
    return n, nil
}
```

- [ ] **Step 6.4: Run test — expect pass**

Run: `cd backend && go test ./internal/infrastructure/database/ -run "TestBacktestDecisionLogRepo" -count=1 -race`
Expected: PASS.

- [ ] **Step 6.5: Commit**

```bash
git add backend/internal/infrastructure/database/backtest_decision_log_repo.go backend/internal/infrastructure/database/backtest_decision_log_repo_test.go
git commit -m "feat(db): add backtestDecisionLogRepo with run-scoped CRUD and retention"
```

---

## Task 7: Implement `DecisionRecorder` (state machine)

**Files:**
- Create: `backend/internal/usecase/decisionlog/recorder.go`
- Create: `backend/internal/usecase/decisionlog/recorder_test.go`

- [ ] **Step 7.1: Write the failing test (HOLD-only path)**

Create `backend/internal/usecase/decisionlog/recorder_test.go`:
```go
package decisionlog

import (
    "context"
    "errors"
    "testing"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type stubRepo struct {
    inserted []entity.DecisionRecord
    insertErr error
}

func (s *stubRepo) Insert(_ context.Context, rec entity.DecisionRecord) error {
    if s.insertErr != nil {
        return s.insertErr
    }
    s.inserted = append(s.inserted, rec)
    return nil
}

func TestRecorder_HoldOnlyBarFlushesOnNextIndicator(t *testing.T) {
    repo := &stubRepo{}
    rec := NewRecorder(repo, RecorderConfig{
        SymbolID:        7,
        CurrencyPair:    "LTC_JPY",
        PrimaryInterval: "PT15M",
        StanceProvider:  func() string { return "TREND_FOLLOW" },
    })
    ctx := context.Background()

    bar1 := indicatorEvent(7, 1_000)
    bar2 := indicatorEvent(7, 2_000)

    if _, err := rec.Handle(ctx, bar1); err != nil {
        t.Fatalf("Handle bar1: %v", err)
    }
    // Nothing else for bar1; flush only happens at next IndicatorEvent.
    if len(repo.inserted) != 0 {
        t.Fatalf("after bar1 alone, expected 0 inserts, got %d", len(repo.inserted))
    }

    if _, err := rec.Handle(ctx, bar2); err != nil {
        t.Fatalf("Handle bar2: %v", err)
    }
    if len(repo.inserted) != 1 {
        t.Fatalf("expected 1 insert (bar1 flushed), got %d", len(repo.inserted))
    }
    got := repo.inserted[0]
    if got.SignalAction != "HOLD" {
        t.Errorf("SignalAction = %q, want HOLD", got.SignalAction)
    }
    if got.RiskOutcome != entity.DecisionRiskSkipped {
        t.Errorf("RiskOutcome = %q, want SKIPPED", got.RiskOutcome)
    }
    if got.OrderOutcome != entity.DecisionOrderNoop {
        t.Errorf("OrderOutcome = %q, want NOOP", got.OrderOutcome)
    }
    if got.TriggerKind != entity.DecisionTriggerBarClose {
        t.Errorf("TriggerKind = %q, want BAR_CLOSE", got.TriggerKind)
    }
    if got.BarCloseAt != 1_000 {
        t.Errorf("BarCloseAt = %d, want 1_000", got.BarCloseAt)
    }
}

func TestRecorder_FullBuyFlushesOnOrder(t *testing.T) {
    repo := &stubRepo{}
    rec := NewRecorder(repo, RecorderConfig{
        SymbolID:        7,
        CurrencyPair:    "LTC_JPY",
        PrimaryInterval: "PT15M",
        StanceProvider:  func() string { return "TREND_FOLLOW" },
    })
    ctx := context.Background()

    if _, err := rec.Handle(ctx, indicatorEvent(7, 1_000)); err != nil {
        t.Fatalf("Handle indicator: %v", err)
    }
    if _, err := rec.Handle(ctx, entity.SignalEvent{
        Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Confidence: 0.7, Reason: "ema cross", Timestamp: 1_000},
        Price:     30210,
        Timestamp: 1_000,
    }); err != nil {
        t.Fatalf("Handle signal: %v", err)
    }
    if _, err := rec.Handle(ctx, entity.ApprovedSignalEvent{
        Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Reason: "ema cross"},
        Price:     30210,
        Timestamp: 1_000,
        Amount:    0.5,
    }); err != nil {
        t.Fatalf("Handle approved: %v", err)
    }
    if _, err := rec.Handle(ctx, entity.OrderEvent{
        OrderID: 42, SymbolID: 7, Side: "BUY", Action: "open",
        Price: 30215, Amount: 0.5, Reason: "ema cross", Timestamp: 1_001,
        Trigger: entity.DecisionTriggerBarClose, OpenedPositionID: 100,
    }); err != nil {
        t.Fatalf("Handle order: %v", err)
    }

    if len(repo.inserted) != 1 {
        t.Fatalf("expected 1 insert (flushed on OrderEvent), got %d", len(repo.inserted))
    }
    got := repo.inserted[0]
    if got.SignalAction != "BUY" || got.RiskOutcome != entity.DecisionRiskApproved ||
        got.BookGateOutcome != entity.DecisionBookAllowed || got.OrderOutcome != entity.DecisionOrderFilled {
        t.Errorf("flushed record fields wrong: %+v", got)
    }
    if got.OpenedPositionID != 100 || got.OrderID != 42 || got.ExecutedAmount != 0.5 {
        t.Errorf("execution fields wrong: %+v", got)
    }
}

func TestRecorder_RiskRejectionFlushesImmediately(t *testing.T) {
    repo := &stubRepo{}
    rec := NewRecorder(repo, RecorderConfig{
        SymbolID:        7,
        CurrencyPair:    "LTC_JPY",
        PrimaryInterval: "PT15M",
        StanceProvider:  func() string { return "TREND_FOLLOW" },
    })
    ctx := context.Background()

    _, _ = rec.Handle(ctx, indicatorEvent(7, 1_000))
    _, _ = rec.Handle(ctx, entity.SignalEvent{
        Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Reason: "ema cross"},
        Price:     30210,
        Timestamp: 1_000,
    })
    _, _ = rec.Handle(ctx, entity.RejectedSignalEvent{
        Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Reason: "ema cross"},
        Stage:     entity.RejectedStageRisk,
        Reason:    "daily loss limit hit",
        Price:     30210,
        Timestamp: 1_000,
    })

    if len(repo.inserted) != 1 {
        t.Fatalf("expected 1 insert (flushed on Rejected), got %d", len(repo.inserted))
    }
    got := repo.inserted[0]
    if got.RiskOutcome != entity.DecisionRiskRejected || got.RiskReason != "daily loss limit hit" {
        t.Errorf("risk fields wrong: %+v", got)
    }
    if got.SignalAction != "BUY" {
        t.Errorf("SignalAction must be preserved as BUY, got %q", got.SignalAction)
    }
    if got.OrderOutcome != entity.DecisionOrderNoop {
        t.Errorf("OrderOutcome must remain NOOP, got %q", got.OrderOutcome)
    }
}

func TestRecorder_BookGateVetoMarksApprovedThenVetoed(t *testing.T) {
    repo := &stubRepo{}
    rec := NewRecorder(repo, RecorderConfig{
        SymbolID:        7,
        CurrencyPair:    "LTC_JPY",
        PrimaryInterval: "PT15M",
        StanceProvider:  func() string { return "TREND_FOLLOW" },
    })
    ctx := context.Background()

    _, _ = rec.Handle(ctx, indicatorEvent(7, 1_000))
    _, _ = rec.Handle(ctx, entity.SignalEvent{
        Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionSell, Reason: "rsi extreme"},
        Price:     30210,
        Timestamp: 1_000,
    })
    _, _ = rec.Handle(ctx, entity.RejectedSignalEvent{
        Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionSell, Reason: "rsi extreme"},
        Stage:     entity.RejectedStageBookGate,
        Reason:    "thin book on bid",
        Price:     30210,
        Timestamp: 1_000,
    })

    if len(repo.inserted) != 1 {
        t.Fatalf("expected 1 insert, got %d", len(repo.inserted))
    }
    got := repo.inserted[0]
    if got.RiskOutcome != entity.DecisionRiskApproved {
        t.Errorf("RiskOutcome must be APPROVED (book gate is post-risk), got %q", got.RiskOutcome)
    }
    if got.BookGateOutcome != entity.DecisionBookVetoed || got.BookGateReason != "thin book on bid" {
        t.Errorf("book gate fields wrong: %+v", got)
    }
}

func TestRecorder_TickSLTPClosePersistedAsSeparateRow(t *testing.T) {
    repo := &stubRepo{}
    rec := NewRecorder(repo, RecorderConfig{
        SymbolID:        7,
        CurrencyPair:    "LTC_JPY",
        PrimaryInterval: "PT15M",
        StanceProvider:  func() string { return "TREND_FOLLOW" },
    })
    ctx := context.Background()

    _, _ = rec.Handle(ctx, indicatorEvent(7, 1_000))
    // Tick-driven close arrives mid-bar (timestamp between 1_000 and next bar).
    _, _ = rec.Handle(ctx, entity.OrderEvent{
        OrderID: 99, SymbolID: 7, Side: "SELL", Action: "close",
        Price: 30180, Amount: 0.5, Reason: "stop_loss", Timestamp: 1_500,
        Trigger: entity.DecisionTriggerTickSLTP, ClosedPositionID: 100,
    })

    // Pending bar1 record is still HOLD; tick close goes in as its own row.
    if len(repo.inserted) != 1 {
        t.Fatalf("expected 1 insert (tick row, bar1 still pending), got %d", len(repo.inserted))
    }
    got := repo.inserted[0]
    if got.TriggerKind != entity.DecisionTriggerTickSLTP {
        t.Errorf("TriggerKind = %q, want TICK_SLTP", got.TriggerKind)
    }
    if got.ClosedPositionID != 100 {
        t.Errorf("ClosedPositionID = %d, want 100", got.ClosedPositionID)
    }
    if got.SequenceInBar != 1 {
        t.Errorf("SequenceInBar = %d, want 1 (bar1 BAR_CLOSE = 0, then this = 1)", got.SequenceInBar)
    }
    if got.SignalReason != "stop_loss" {
        t.Errorf("SignalReason = %q, want %q", got.SignalReason, "stop_loss")
    }
}

func TestRecorder_InsertErrorDoesNotPropagate(t *testing.T) {
    repo := &stubRepo{insertErr: errors.New("db down")}
    rec := NewRecorder(repo, RecorderConfig{
        SymbolID:        7,
        CurrencyPair:    "LTC_JPY",
        PrimaryInterval: "PT15M",
        StanceProvider:  func() string { return "TREND_FOLLOW" },
    })
    ctx := context.Background()

    // bar1 indicator -> bar2 indicator (forces flush of bar1) must NOT return an error.
    if _, err := rec.Handle(ctx, indicatorEvent(7, 1_000)); err != nil {
        t.Fatalf("Handle indicator returned error: %v", err)
    }
    if _, err := rec.Handle(ctx, indicatorEvent(7, 2_000)); err != nil {
        t.Fatalf("Handle indicator must swallow Insert errors, got: %v", err)
    }
}

// indicatorEvent builds a minimal IndicatorEvent for tests.
func indicatorEvent(symbolID int64, ts int64) entity.IndicatorEvent {
    return entity.IndicatorEvent{
        SymbolID:  symbolID,
        Interval:  "PT15M",
        LastPrice: 30210,
        Timestamp: ts,
    }
}
```

- [ ] **Step 7.2: Run test — expect compile failure**

Run: `cd backend && go test ./internal/usecase/decisionlog/ -count=1`
Expected: build error referencing undefined `NewRecorder`.

- [ ] **Step 7.3: Implement the recorder**

Create `backend/internal/usecase/decisionlog/recorder.go`:
```go
// Package decisionlog persists every pipeline decision (BUY/SELL/HOLD plus
// the reasons each gate produced) into SQLite. Recorder is an EventBus
// subscriber registered at priority 99 so it runs after all primary
// handlers; it never blocks or modifies the pipeline.
package decisionlog

import (
    "context"
    "encoding/json"
    "log/slog"
    "time"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// RecorderConfig binds the recorder to one pipeline instance.
//
// StanceProvider is called every IndicatorEvent to snapshot the pipeline's
// current stance. A nil provider falls back to "UNKNOWN" — useful for tests
// and for the wiring step before pipeline integration lands.
type RecorderConfig struct {
    SymbolID        int64
    CurrencyPair    string
    PrimaryInterval string
    StanceProvider  func() string
}

// Recorder observes EventBus events and writes one DecisionRecord per
// completed cycle. It is NOT goroutine-safe; one Recorder must be bound to
// exactly one EventBus chain (the EventBus dispatch loop is single-threaded
// per chain so this matches the runtime invariant).
type Recorder struct {
    repo   repository.DecisionLogRepository
    cfg    RecorderConfig
    nowFn  func() time.Time
    logger *slog.Logger

    pending           *draft
    nextSequenceInBar int
    lastIndicatorJSON string
    lastHigherTFJSON  string
}

func NewRecorder(repo repository.DecisionLogRepository, cfg RecorderConfig) *Recorder {
    return &Recorder{
        repo:   repo,
        cfg:    cfg,
        nowFn:  time.Now,
        logger: slog.Default(),
    }
}

// SetClock overrides the timestamp source. Tests use this to make CreatedAt
// deterministic; production never calls it.
func (r *Recorder) SetClock(fn func() time.Time) { r.nowFn = fn }

// Handle implements eventengine.EventHandler. It returns nil chained events
// so the bus stays unaffected by recorder activity.
func (r *Recorder) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
    switch ev := event.(type) {
    case entity.IndicatorEvent:
        r.onIndicator(ctx, ev)
    case entity.SignalEvent:
        r.onSignal(ev)
    case entity.ApprovedSignalEvent:
        r.onApproved(ev)
    case entity.RejectedSignalEvent:
        r.onRejected(ctx, ev)
    case entity.OrderEvent:
        r.onOrder(ctx, ev)
    }
    return nil, nil
}

// draft is the in-progress record for one bar. Fields are mutated as more
// events flow in; flush() persists and clears it.
type draft struct {
    rec entity.DecisionRecord
}

func (r *Recorder) stance() string {
    if r.cfg.StanceProvider == nil {
        return "UNKNOWN"
    }
    return r.cfg.StanceProvider()
}

func (r *Recorder) onIndicator(ctx context.Context, ev entity.IndicatorEvent) {
    // If a previous bar's draft is still pending, flush it as HOLD/NOOP —
    // the strategy never produced a SignalEvent so nothing else will arrive.
    r.flushPending(ctx)

    // Reset sequence numbering for the new bar.
    r.nextSequenceInBar = 1

    indicatorsJSON, err := json.Marshal(ev.Primary)
    if err != nil {
        r.logger.Warn("decisionlog: marshal indicators failed", "error", err)
        indicatorsJSON = []byte("{}")
    }
    var higherJSON []byte
    if ev.HigherTF != nil {
        higherJSON, err = json.Marshal(ev.HigherTF)
        if err != nil {
            r.logger.Warn("decisionlog: marshal higher-tf indicators failed", "error", err)
            higherJSON = []byte("{}")
        }
    } else {
        higherJSON = []byte("{}")
    }
    r.lastIndicatorJSON = string(indicatorsJSON)
    r.lastHigherTFJSON = string(higherJSON)

    r.pending = &draft{
        rec: entity.DecisionRecord{
            BarCloseAt:             ev.Timestamp,
            SequenceInBar:          0,
            TriggerKind:            entity.DecisionTriggerBarClose,
            SymbolID:               r.cfg.SymbolID,
            CurrencyPair:           r.cfg.CurrencyPair,
            PrimaryInterval:        r.cfg.PrimaryInterval,
            Stance:                 r.stance(),
            LastPrice:              ev.LastPrice,
            SignalAction:           string(entity.SignalActionHold),
            RiskOutcome:            entity.DecisionRiskSkipped,
            BookGateOutcome:        entity.DecisionBookSkipped,
            OrderOutcome:           entity.DecisionOrderNoop,
            IndicatorsJSON:         r.lastIndicatorJSON,
            HigherTFIndicatorsJSON: r.lastHigherTFJSON,
        },
    }
}

func (r *Recorder) onSignal(ev entity.SignalEvent) {
    if r.pending == nil {
        return
    }
    r.pending.rec.SignalAction = string(ev.Signal.Action)
    r.pending.rec.SignalConfidence = ev.Signal.Confidence
    r.pending.rec.SignalReason = ev.Signal.Reason
}

func (r *Recorder) onApproved(ev entity.ApprovedSignalEvent) {
    if r.pending == nil {
        return
    }
    r.pending.rec.RiskOutcome = entity.DecisionRiskApproved
    r.pending.rec.BookGateOutcome = entity.DecisionBookAllowed
    _ = ev
}

func (r *Recorder) onRejected(ctx context.Context, ev entity.RejectedSignalEvent) {
    if r.pending == nil {
        return
    }
    switch ev.Stage {
    case entity.RejectedStageRisk:
        r.pending.rec.RiskOutcome = entity.DecisionRiskRejected
        r.pending.rec.RiskReason = ev.Reason
    case entity.RejectedStageBookGate:
        r.pending.rec.RiskOutcome = entity.DecisionRiskApproved
        r.pending.rec.BookGateOutcome = entity.DecisionBookVetoed
        r.pending.rec.BookGateReason = ev.Reason
    }
    r.flushPending(ctx)
}

func (r *Recorder) onOrder(ctx context.Context, ev entity.OrderEvent) {
    switch ev.Trigger {
    case entity.DecisionTriggerTickSLTP, entity.DecisionTriggerTickTrailing:
        r.persistTickOrder(ctx, ev)
    default:
        // Treat empty Trigger as a bar-close order for backward compat.
        r.persistBarOrder(ctx, ev)
    }
}

func (r *Recorder) persistBarOrder(ctx context.Context, ev entity.OrderEvent) {
    if r.pending == nil {
        return
    }
    if ev.OrderID > 0 {
        r.pending.rec.OrderOutcome = entity.DecisionOrderFilled
    } else {
        r.pending.rec.OrderOutcome = entity.DecisionOrderFailed
    }
    r.pending.rec.OrderID = ev.OrderID
    r.pending.rec.ExecutedAmount = ev.Amount
    r.pending.rec.ExecutedPrice = ev.Price
    r.pending.rec.OpenedPositionID = ev.OpenedPositionID
    r.pending.rec.ClosedPositionID = ev.ClosedPositionID
    r.flushPending(ctx)
}

func (r *Recorder) persistTickOrder(ctx context.Context, ev entity.OrderEvent) {
    rec := entity.DecisionRecord{
        BarCloseAt:             ev.Timestamp,
        SequenceInBar:          r.nextSequenceInBar,
        TriggerKind:            ev.Trigger,
        SymbolID:               r.cfg.SymbolID,
        CurrencyPair:           r.cfg.CurrencyPair,
        PrimaryInterval:        r.cfg.PrimaryInterval,
        Stance:                 r.stance(),
        LastPrice:              ev.Price,
        SignalAction:           string(entity.SignalActionHold),
        SignalReason:           ev.Reason,
        RiskOutcome:            entity.DecisionRiskSkipped,
        BookGateOutcome:        entity.DecisionBookSkipped,
        OrderOutcome:           entity.DecisionOrderFilled,
        OrderID:                ev.OrderID,
        ExecutedAmount:         ev.Amount,
        ExecutedPrice:          ev.Price,
        ClosedPositionID:       ev.ClosedPositionID,
        OpenedPositionID:       ev.OpenedPositionID,
        IndicatorsJSON:         r.lastIndicatorJSON,
        HigherTFIndicatorsJSON: r.lastHigherTFJSON,
        CreatedAt:              r.nowFn().UnixMilli(),
    }
    if rec.IndicatorsJSON == "" {
        rec.IndicatorsJSON = "{}"
    }
    if rec.HigherTFIndicatorsJSON == "" {
        rec.HigherTFIndicatorsJSON = "{}"
    }
    if ev.OrderID == 0 {
        rec.OrderOutcome = entity.DecisionOrderFailed
    }
    if err := r.repo.Insert(ctx, rec); err != nil {
        r.logger.Warn("decisionlog: tick insert failed", "error", err)
        return
    }
    r.nextSequenceInBar++
}

func (r *Recorder) flushPending(ctx context.Context) {
    if r.pending == nil {
        return
    }
    rec := r.pending.rec
    rec.CreatedAt = r.nowFn().UnixMilli()
    if rec.IndicatorsJSON == "" {
        rec.IndicatorsJSON = "{}"
    }
    if rec.HigherTFIndicatorsJSON == "" {
        rec.HigherTFIndicatorsJSON = "{}"
    }
    if err := r.repo.Insert(ctx, rec); err != nil {
        r.logger.Warn("decisionlog: insert failed", "error", err)
    }
    r.pending = nil
}
```

- [ ] **Step 7.4: Run test — expect pass**

Run: `cd backend && go test ./internal/usecase/decisionlog/ -count=1 -race`
Expected: PASS for all 6 tests.

- [ ] **Step 7.5: Commit**

```bash
git add backend/internal/usecase/decisionlog/recorder.go backend/internal/usecase/decisionlog/recorder_test.go
git commit -m "feat(decisionlog): add Recorder state machine for EventBus subscription"
```

---

## Task 8: Implement retention goroutine

**Files:**
- Create: `backend/internal/usecase/decisionlog/retention.go`
- Create: `backend/internal/usecase/decisionlog/retention_test.go`

- [ ] **Step 8.1: Write the failing test**

Create `backend/internal/usecase/decisionlog/retention_test.go`:
```go
package decisionlog

import (
    "context"
    "errors"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type stubBacktestRepo struct {
    mu      sync.Mutex
    cutoffs []int64
    err     error
}

func (s *stubBacktestRepo) Insert(_ context.Context, _ entity.DecisionRecord, _ string) error {
    return nil
}
func (s *stubBacktestRepo) ListByRun(_ context.Context, _ string, _ int, _ int64) ([]entity.DecisionRecord, int64, error) {
    return nil, 0, nil
}
func (s *stubBacktestRepo) DeleteByRun(_ context.Context, _ string) (int64, error) { return 0, nil }
func (s *stubBacktestRepo) DeleteOlderThan(_ context.Context, cutoff int64) (int64, error) {
    s.mu.Lock()
    s.cutoffs = append(s.cutoffs, cutoff)
    s.mu.Unlock()
    return 1, s.err
}

func TestRetention_RunsImmediatelyAndOnTicker(t *testing.T) {
    repo := &stubBacktestRepo{}
    fixedNow := int64(10_000_000)
    cleanup := NewRetentionCleanup(repo, RetentionConfig{
        MaxAge:   3 * 24 * time.Hour,
        Interval: 20 * time.Millisecond,
        NowFn:    func() time.Time { return time.UnixMilli(fixedNow) },
    })

    ctx, cancel := context.WithCancel(context.Background())
    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        defer wg.Done()
        cleanup.Run(ctx)
    }()

    // Wait for at least 2 sweeps (initial + 1 tick).
    deadline := time.Now().Add(500 * time.Millisecond)
    for {
        repo.mu.Lock()
        n := len(repo.cutoffs)
        repo.mu.Unlock()
        if n >= 2 || time.Now().After(deadline) {
            break
        }
        time.Sleep(5 * time.Millisecond)
    }
    cancel()
    wg.Wait()

    repo.mu.Lock()
    defer repo.mu.Unlock()
    if len(repo.cutoffs) < 2 {
        t.Fatalf("expected ≥2 sweeps, got %d", len(repo.cutoffs))
    }
    expected := fixedNow - int64(3*24*time.Hour/time.Millisecond)
    for _, c := range repo.cutoffs {
        if c != expected {
            t.Errorf("cutoff = %d, want %d", c, expected)
        }
    }
}

func TestRetention_DeleteErrorDoesNotKillLoop(t *testing.T) {
    repo := &stubBacktestRepo{err: errors.New("db down")}
    var sweeps atomic.Int32
    repo2 := &countingErrorRepo{stub: repo, count: &sweeps}

    cleanup := NewRetentionCleanup(repo2, RetentionConfig{
        MaxAge:   3 * 24 * time.Hour,
        Interval: 10 * time.Millisecond,
    })

    ctx, cancel := context.WithCancel(context.Background())
    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        defer wg.Done()
        cleanup.Run(ctx)
    }()

    deadline := time.Now().Add(200 * time.Millisecond)
    for sweeps.Load() < 3 && time.Now().Before(deadline) {
        time.Sleep(5 * time.Millisecond)
    }
    cancel()
    wg.Wait()

    if sweeps.Load() < 3 {
        t.Errorf("loop must continue after errors, sweeps = %d", sweeps.Load())
    }
}

type countingErrorRepo struct {
    stub  *stubBacktestRepo
    count *atomic.Int32
}

func (c *countingErrorRepo) Insert(ctx context.Context, rec entity.DecisionRecord, runID string) error {
    return c.stub.Insert(ctx, rec, runID)
}
func (c *countingErrorRepo) ListByRun(ctx context.Context, runID string, limit int, cursor int64) ([]entity.DecisionRecord, int64, error) {
    return c.stub.ListByRun(ctx, runID, limit, cursor)
}
func (c *countingErrorRepo) DeleteByRun(ctx context.Context, runID string) (int64, error) {
    return c.stub.DeleteByRun(ctx, runID)
}
func (c *countingErrorRepo) DeleteOlderThan(ctx context.Context, cutoff int64) (int64, error) {
    c.count.Add(1)
    return c.stub.DeleteOlderThan(ctx, cutoff)
}
```

- [ ] **Step 8.2: Run test — expect compile failure**

Run: `cd backend && go test ./internal/usecase/decisionlog/ -run "TestRetention" -count=1`
Expected: build error referencing undefined `NewRetentionCleanup`.

- [ ] **Step 8.3: Implement retention**

Create `backend/internal/usecase/decisionlog/retention.go`:
```go
package decisionlog

import (
    "context"
    "log/slog"
    "time"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// RetentionConfig controls the backtest decision-log cleanup loop.
//
//   - MaxAge: rows older than now-MaxAge get deleted.
//   - Interval: how often the loop runs.
//   - NowFn: clock injection for tests; nil falls back to time.Now.
type RetentionConfig struct {
    MaxAge   time.Duration
    Interval time.Duration
    NowFn    func() time.Time
}

// RetentionCleanup periodically deletes backtest decision-log rows older
// than MaxAge. It runs an initial sweep at start, then sweeps every
// Interval until the context is cancelled. Errors are logged at warn level
// and do not abort the loop.
type RetentionCleanup struct {
    repo   repository.BacktestDecisionLogRepository
    cfg    RetentionConfig
    logger *slog.Logger
}

func NewRetentionCleanup(repo repository.BacktestDecisionLogRepository, cfg RetentionConfig) *RetentionCleanup {
    if cfg.NowFn == nil {
        cfg.NowFn = time.Now
    }
    return &RetentionCleanup{repo: repo, cfg: cfg, logger: slog.Default()}
}

// Run blocks until ctx is cancelled.
func (c *RetentionCleanup) Run(ctx context.Context) {
    c.sweep(ctx)
    if c.cfg.Interval <= 0 {
        return
    }
    ticker := time.NewTicker(c.cfg.Interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            c.sweep(ctx)
        }
    }
}

func (c *RetentionCleanup) sweep(ctx context.Context) {
    cutoff := c.cfg.NowFn().Add(-c.cfg.MaxAge).UnixMilli()
    deleted, err := c.repo.DeleteOlderThan(ctx, cutoff)
    if err != nil {
        c.logger.Warn("decisionlog retention: sweep failed", "cutoff", cutoff, "error", err)
        return
    }
    if deleted > 0 {
        c.logger.Info("decisionlog retention: pruned rows", "deleted", deleted, "cutoff", cutoff)
    }
}
```

- [ ] **Step 8.4: Run test — expect pass**

Run: `cd backend && go test ./internal/usecase/decisionlog/ -run "TestRetention" -count=1 -race`
Expected: PASS.

- [ ] **Step 8.5: Run full package test**

Run: `cd backend && go test ./internal/usecase/decisionlog/ -count=1 -race`
Expected: PASS.

- [ ] **Step 8.6: Commit**

```bash
git add backend/internal/usecase/decisionlog/retention.go backend/internal/usecase/decisionlog/retention_test.go
git commit -m "feat(decisionlog): add retention cleanup goroutine for backtest logs"
```

---

## Task 9: Wire `RejectedSignalEvent` emission in `RiskHandler` and BookGate paths

**Files:**
- Modify: `backend/internal/usecase/backtest/handler.go`
- Modify: `backend/internal/usecase/backtest/handler_test.go` (or add a new `_test.go` if the existing file is too large)

- [ ] **Step 9.1: Read current rejection branches**

Open `backend/internal/usecase/backtest/handler.go` and find the three `return nil, nil` branches in `RiskHandler.Handle`:
1. Sizer rejected (line ~465 area): `if skipReason != "" || sized <= 0 { return nil, nil }`
2. RiskManager check failed (line ~484): `if !check.Approved { return nil, nil }`
3. BookGate veto (line ~493): inside the `if h.BookGate != nil` block, `return nil, nil`

Each branch loses information today. We replace each with a `RejectedSignalEvent` emission carrying a stage + reason.

- [ ] **Step 9.2: Write the failing test**

Add to `backend/internal/usecase/backtest/handler_test.go` (verify the package and helpers first):
```go
func TestRiskHandler_EmitsRejectedOnRiskManagerVeto(t *testing.T) {
    rm := newRiskManagerThatRejects(t, "daily loss limit hit")
    h := &RiskHandler{
        RiskManager: rm,
        TradeAmount: 0.5,
    }
    sig := entity.SignalEvent{
        Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Reason: "ema cross"},
        Price:     30210,
        Timestamp: 1_000,
    }
    out, err := h.Handle(context.Background(), sig)
    if err != nil {
        t.Fatalf("Handle: %v", err)
    }
    if len(out) != 1 {
        t.Fatalf("expected 1 emitted event, got %d", len(out))
    }
    rej, ok := out[0].(entity.RejectedSignalEvent)
    if !ok {
        t.Fatalf("expected RejectedSignalEvent, got %T", out[0])
    }
    if rej.Stage != entity.RejectedStageRisk {
        t.Errorf("Stage = %q, want %q", rej.Stage, entity.RejectedStageRisk)
    }
    if rej.Reason == "" {
        t.Errorf("Reason must be populated from RiskManager check")
    }
    if rej.Signal.Action != entity.SignalActionBuy {
        t.Errorf("Signal must be carried through, got action %q", rej.Signal.Action)
    }
}
```

`newRiskManagerThatRejects` may already exist; if not, model it after the existing rejection-test helpers in `risk_test.go` (search for `RiskManager` constructor patterns and how rejection is forced in current tests). Reuse the same approach — do not invent a new mocking style.

If the existing `RiskManager` cannot easily be forced to reject (e.g. requires daily-loss state), you may instead drive the test through `TradeAmount: -1` to hit a cheaper rejection path; but prefer to use the real `CheckOrderAt` rejection path since that is what the production code traverses.

- [ ] **Step 9.3: Run test — expect fail**

Run: `cd backend && go test ./internal/usecase/backtest/ -run "TestRiskHandler_EmitsRejected" -count=1`
Expected: FAIL with `expected 1 emitted event, got 0`.

- [ ] **Step 9.4: Replace `return nil, nil` with `RejectedSignalEvent` emission**

In `backend/internal/usecase/backtest/handler.go`, locate the sizer-rejection branch (around line 465) and replace:
```go
        if skipReason != "" || sized <= 0 {
            return nil, nil
        }
```
with:
```go
        if skipReason != "" || sized <= 0 {
            return []entity.Event{entity.RejectedSignalEvent{
                Signal:    signalEvent.Signal,
                Stage:     entity.RejectedStageRisk,
                Reason:    "sizer skipped: " + skipReason,
                Price:     signalEvent.Price,
                Timestamp: signalEvent.Timestamp,
            }}, nil
        }
```
If `skipReason` is empty (sized == 0) the message becomes `"sizer skipped: "`. That's fine — emptiness still marks the rejection; if you want to avoid the trailing colon, branch:
```go
        if skipReason != "" || sized <= 0 {
            reason := "sizer skipped"
            if skipReason != "" {
                reason = "sizer skipped: " + skipReason
            } else if sized <= 0 {
                reason = "sizer returned zero lot"
            }
            return []entity.Event{entity.RejectedSignalEvent{
                Signal:    signalEvent.Signal,
                Stage:     entity.RejectedStageRisk,
                Reason:    reason,
                Price:     signalEvent.Price,
                Timestamp: signalEvent.Timestamp,
            }}, nil
        }
```

Locate the RiskManager rejection branch (around line 484):
```go
    check := h.RiskManager.CheckOrderAt(ctx, time.UnixMilli(signalEvent.Timestamp), proposal)
    if !check.Approved {
        return nil, nil
    }
```
and replace with:
```go
    check := h.RiskManager.CheckOrderAt(ctx, time.UnixMilli(signalEvent.Timestamp), proposal)
    if !check.Approved {
        return []entity.Event{entity.RejectedSignalEvent{
            Signal:    signalEvent.Signal,
            Stage:     entity.RejectedStageRisk,
            Reason:    check.Reason,
            Price:     signalEvent.Price,
            Timestamp: signalEvent.Timestamp,
        }}, nil
    }
```
(If `check.Reason` is not the actual field name on `RiskCheckResult`, read `risk.go` and adjust to the real field. Do NOT invent a field; use what exists. If no reason is exposed, add `"risk manager rejected"` as a placeholder reason and open a follow-up to surface the real reason — note this in the commit message.)

Locate the BookGate branch (around line 491):
```go
    if h.BookGate != nil {
        decision := h.BookGate.Check(ctx, signalEvent.Signal.SymbolID, side, amount, signalEvent.Timestamp)
        if !decision.Allow {
            if h.BookGateRejects == nil {
                h.BookGateRejects = make(map[string]int)
            }
            h.BookGateRejects[decision.Reason]++
            return nil, nil
        }
    }
```
and replace the `return nil, nil` with:
```go
            return []entity.Event{entity.RejectedSignalEvent{
                Signal:    signalEvent.Signal,
                Stage:     entity.RejectedStageBookGate,
                Reason:    decision.Reason,
                Price:     signalEvent.Price,
                Timestamp: signalEvent.Timestamp,
            }}, nil
```

- [ ] **Step 9.5: Run targeted test — expect pass**

Run: `cd backend && go test ./internal/usecase/backtest/ -run "TestRiskHandler_EmitsRejected" -count=1`
Expected: PASS.

- [ ] **Step 9.6: Run full backtest package — guard against regressions**

Run: `cd backend && go test ./internal/usecase/backtest/ -count=1 -race`
Expected: PASS.

If any pre-existing test fails because it asserted "no events emitted on rejection," update those tests to expect `[]entity.Event{RejectedSignalEvent{...}}` instead. The change is intentional.

- [ ] **Step 9.7: Commit**

```bash
git add backend/internal/usecase/backtest/handler.go backend/internal/usecase/backtest/handler_test.go
git commit -m "feat(backtest): emit RejectedSignalEvent on risk/book-gate veto"
```

---

## Task 10: Populate new `OrderEvent` fields at executor / tick-handler call sites

**Files:**
- Modify: `backend/internal/infrastructure/live/real_executor.go`
- Modify: `backend/internal/usecase/backtest/handler.go` (TickRiskHandler `Close` calls)
- Modify: any sim executor that constructs `OrderEvent` (search before editing)

- [ ] **Step 10.1: Find all `OrderEvent{...}` constructions**

Run: `cd backend && grep -rn "entity.OrderEvent{" --include="*.go" .`
Expected output lists every place that builds an `OrderEvent` literal. For each, decide:
- Bar-close opens (executor.Open / OpenWithUrgency) → `Trigger: DecisionTriggerBarClose, OpenedPositionID: <newPosID>`
- Tick-driven closes (TickRiskHandler stop_loss / take_profit / trailing_stop calls to executor.Close) → `Trigger: DecisionTriggerTickSLTP` or `DecisionTriggerTickTrailing`, `ClosedPositionID: <closedPosID>`
- Reversals (executor.Open that immediately closes a counter-position) → both `OpenedPositionID` and `ClosedPositionID`

If any executor's `Close` returns an `OrderEvent` without a position ID, the position ID must be added to the `Close` signature/return. Read each executor before editing.

- [ ] **Step 10.2: Write the failing test (live executor)**

In `backend/internal/infrastructure/live/real_executor_test.go`, add:
```go
func TestRealExecutor_Open_PopulatesTriggerAndOpenedPositionID(t *testing.T) {
    // Use the existing mockOrderClient pattern in this file. The mock returns
    // a synthetic order with a known ID; assert the returned OrderEvent has
    // Trigger == BAR_CLOSE and OpenedPositionID == <mock position ID>.
    // (Read the existing mock in this file first; reuse it verbatim.)
    t.Skip("TODO: fill in once mockOrderClient pattern is read")
}
```
Keep the test skipped initially; un-skip in the next step after reading the mock pattern. (We're sequencing read → un-skip → implement to keep TDD discipline visible.)

Then read the mock and complete the test to assert:
```go
if got.Trigger != entity.DecisionTriggerBarClose {
    t.Errorf("Trigger = %q, want %q", got.Trigger, entity.DecisionTriggerBarClose)
}
if got.OpenedPositionID != expectedID {
    t.Errorf("OpenedPositionID = %d, want %d", got.OpenedPositionID, expectedID)
}
```

- [ ] **Step 10.3: Run test — expect fail**

Run: `cd backend && go test ./internal/infrastructure/live/ -run "TestRealExecutor_Open_Populates" -count=1`
Expected: FAIL.

- [ ] **Step 10.4: Update `RealExecutor.Open` and `OpenWithUrgency` returns**

In `backend/internal/infrastructure/live/real_executor.go`, find the two locations that return `entity.OrderEvent{...}` (around lines 190 and 252) and add the new fields to the literal:
```go
    return entity.OrderEvent{
        OrderID:          orderID,
        SymbolID:         symbolID,
        Side:             string(side),
        Action:           "open",
        Price:            executedPrice,
        Amount:           amount,
        Reason:           reason,
        Timestamp:        timestamp,
        Trigger:          entity.DecisionTriggerBarClose,
        OpenedPositionID: openedPosID, // pull from SOR plan result
    }, nil
```
The position ID source depends on the SOR plan return value — read the surrounding code to find where the new position ID is available. If the SOR plan does not return one, set `OpenedPositionID: orderID` as a best-effort proxy and add a TODO comment explaining the limitation; this still gives the recorder enough to correlate close-with-open.

If the executor has a `Close` method that returns an `OrderEvent`, update it to set `Trigger: <determined by caller>` and `ClosedPositionID: positionID`. The trigger is set by the caller (TickRiskHandler) — see Step 10.6.

- [ ] **Step 10.5: Run live test — expect pass**

Run: `cd backend && go test ./internal/infrastructure/live/ -run "TestRealExecutor_Open_Populates" -count=1`
Expected: PASS.

- [ ] **Step 10.6: Update `TickRiskHandler.Close` callers to set `Trigger`**

In `backend/internal/usecase/backtest/handler.go`, find the three TickRiskHandler `Close` call sites (lines 658, 695, 709). After each successful close, mutate the returned `OrderEvent` to set the trigger before appending to `emitted`:

```go
                orderEvent.Trigger = entity.DecisionTriggerTickSLTP // or TickTrailing for trailing-stop calls
                orderEvent.ClosedPositionID = pos.PositionID
                emitted = append(emitted, orderEvent)
```

Use `DecisionTriggerTickSLTP` for the SL/TP exit (line 658 area) and `DecisionTriggerTickTrailing` for the two trailing-stop call sites (lines 695, 709).

- [ ] **Step 10.7: Update related test assertions**

If any test in `handler_test.go` (or sim executor tests) asserts on the full shape of the emitted `OrderEvent`, update the expected values to include the new fields. Run:
```bash
cd backend && go test ./internal/usecase/backtest/ -count=1
```
Fix any test that fails because it asserted exact struct equality without the new fields.

- [ ] **Step 10.8: Run full backend test suite**

Run: `cd backend && go test ./... -count=1 -race`
Expected: PASS.

- [ ] **Step 10.9: Commit**

```bash
git add backend/internal/infrastructure/live/real_executor.go backend/internal/infrastructure/live/real_executor_test.go backend/internal/usecase/backtest/handler.go backend/internal/usecase/backtest/handler_test.go
git commit -m "feat(executor): populate Trigger/OpenedPositionID/ClosedPositionID on OrderEvent"
```

---

## Task 11: End-to-end integration test (recorder + bus + repo)

**Files:**
- Create: `backend/internal/usecase/decisionlog/integration_test.go`

- [ ] **Step 11.1: Write the integration test**

Create `backend/internal/usecase/decisionlog/integration_test.go`:
```go
package decisionlog_test

import (
    "context"
    "testing"

    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/decisionlog"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

func TestRecorder_EndToEnd_FullCycleAndHoldBar(t *testing.T) {
    db := database.OpenMemoryDBForTest(t) // use existing test helper if exposed; otherwise duplicate the openMemoryDB pattern locally
    if err := database.Migrate(db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    repo := database.NewDecisionLogRepository(db)

    rec := decisionlog.NewRecorder(repo, decisionlog.RecorderConfig{
        SymbolID:        7,
        CurrencyPair:    "LTC_JPY",
        PrimaryInterval: "PT15M",
        StanceProvider:  func() string { return "TREND_FOLLOW" },
    })

    bus := eventengine.NewEventBus()
    bus.Register(entity.EventTypeIndicator, 99, rec)
    bus.Register(entity.EventTypeSignal, 99, rec)
    bus.Register(entity.EventTypeApproved, 99, rec)
    bus.Register(entity.EventTypeRejected, 99, rec)
    bus.Register(entity.EventTypeOrder, 99, rec)

    ctx := context.Background()

    // Bar 1: full BUY cycle.
    if err := bus.Dispatch(ctx, []entity.Event{
        entity.IndicatorEvent{SymbolID: 7, Interval: "PT15M", LastPrice: 30210, Timestamp: 1_000},
        entity.SignalEvent{
            Signal: entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Confidence: 0.7, Reason: "ema cross"},
            Price: 30210, Timestamp: 1_000,
        },
        entity.ApprovedSignalEvent{
            Signal: entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Reason: "ema cross"},
            Price: 30210, Timestamp: 1_000, Amount: 0.5,
        },
        entity.OrderEvent{
            OrderID: 42, SymbolID: 7, Side: "BUY", Action: "open",
            Price: 30215, Amount: 0.5, Reason: "ema cross", Timestamp: 1_001,
            Trigger: entity.DecisionTriggerBarClose, OpenedPositionID: 100,
        },
    }); err != nil {
        t.Fatalf("Dispatch bar1: %v", err)
    }

    // Bar 2: HOLD only. Flushing happens when bar3 indicator arrives, so dispatch one more.
    if err := bus.Dispatch(ctx, []entity.Event{
        entity.IndicatorEvent{SymbolID: 7, Interval: "PT15M", LastPrice: 30220, Timestamp: 2_000},
    }); err != nil {
        t.Fatalf("Dispatch bar2: %v", err)
    }
    if err := bus.Dispatch(ctx, []entity.Event{
        entity.IndicatorEvent{SymbolID: 7, Interval: "PT15M", LastPrice: 30230, Timestamp: 3_000},
    }); err != nil {
        t.Fatalf("Dispatch bar3: %v", err)
    }

    rows, _, err := repo.List(ctx, repository.DecisionLogFilter{SymbolID: 7, Limit: 100})
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(rows) != 2 {
        t.Fatalf("expected 2 rows (bar1 + bar2), got %d", len(rows))
    }
    // Newest first: bar2 then bar1.
    if rows[0].BarCloseAt != 2_000 || rows[0].SignalAction != "HOLD" {
        t.Errorf("bar2 row wrong: %+v", rows[0])
    }
    if rows[1].BarCloseAt != 1_000 || rows[1].SignalAction != "BUY" || rows[1].OrderOutcome != entity.DecisionOrderFilled {
        t.Errorf("bar1 row wrong: %+v", rows[1])
    }
}
```

If `database.OpenMemoryDBForTest` does not exist, define a small helper inside the test file using the same `_ "github.com/mattn/go-sqlite3"` driver registration the other repo tests use.

- [ ] **Step 11.2: Run the integration test — expect pass**

Run: `cd backend && go test ./internal/usecase/decisionlog/ -count=1 -race`
Expected: PASS.

- [ ] **Step 11.3: Run full backend suite**

Run: `cd backend && go test ./... -count=1 -race`
Expected: PASS.

- [ ] **Step 11.4: Commit**

```bash
git add backend/internal/usecase/decisionlog/integration_test.go
git commit -m "test(decisionlog): end-to-end recorder + bus + repo integration"
```

---

## Task 12: Open PR

- [ ] **Step 12.1: Verify branch state**

Run: `git -C /Users/h.aiso/Projects/rakuten-api-leverage-exchange status` and `git -C /Users/h.aiso/Projects/rakuten-api-leverage-exchange log --oneline -15`
Expected: 11 new commits ahead of `main`, working tree clean.

- [ ] **Step 12.2: Push and open PR**

Use Conventional Commits PR title and the spec link in the body:
```bash
gh pr create --title "feat(decisionlog): add foundation (entity, events, db, recorder)" --body "$(cat <<'EOF'
## Summary
- Adds the foundation for persisting every 15-minute pipeline decision (BUY/SELL/HOLD + reasons + indicator snapshot).
- Introduces `DecisionRecord` entity, `RejectedSignalEvent`, `decision_log` and `backtest_decision_log` SQLite tables, two repositories, the `DecisionRecorder` EventBus subscriber, and a 3-day retention loop for backtest logs.
- Wires `RejectedSignalEvent` emission into `RiskHandler` and the BookGate path; populates new `OrderEvent` fields (`Trigger`, `OpenedPositionID`, `ClosedPositionID`) at all call-sites.

Pipeline DI, HTTP API, and frontend ship in follow-up PRs (see plan file).

Spec: `docs/superpowers/specs/2026-04-26-decision-log-design.md`
Plan: `docs/superpowers/plans/2026-04-26-decision-log-foundation.md`

## Test plan
- [ ] `cd backend && go test ./... -race -count=1` is green
- [ ] New tests cover: HOLD-only flush, full BUY flush, risk rejection, book-gate veto, tick-driven SL/TP separate row, repo CRUD + cursor paging, retention sweep, end-to-end bus integration

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review

**1. Spec coverage**

- ✅ `decision_log` schema → Task 4
- ✅ `backtest_decision_log` schema → Task 4
- ✅ `RejectedSignalEvent` → Task 2
- ✅ `OrderEvent` Trigger / Opened / ClosedPositionID → Task 2 + Task 10
- ✅ `DecisionRecord` entity → Task 1
- ✅ Repository interfaces → Task 3
- ✅ Live repo with cursor paging → Task 5
- ✅ Backtest repo with run-scoped CRUD + retention DELETE → Task 6
- ✅ Recorder state machine (HOLD / BUY / SELL / Risk REJECT / BookGate VETO / TICK_SLTP) → Task 7
- ✅ Retention goroutine (3-day) → Task 8
- ✅ RiskHandler / BookGate emit RejectedSignalEvent → Task 9
- ✅ Executor populates new OrderEvent fields → Task 10
- ✅ End-to-end integration test → Task 11
- ⏸ Pipeline DI (`EventDrivenPipeline.runEventLoop` + Backtest Runner registers recorder + starts retention loop) → **deferred to next plan**
- ⏸ HTTP API → deferred to next plan
- ⏸ Frontend → deferred to next plan
- ⏸ `cmd/main.go` invocation of retention goroutine → deferred to next plan (it is constructed here but not wired)

The deferred items are explicit in "Out of scope for this plan" — this PR delivers a working, tested foundation that the next PR consumes.

**2. Placeholder scan**

- Step 9.4 contains `"add a TODO comment explaining the limitation"` — this is a guarded fallback for the case where the SOR plan return value lacks a position ID. The instruction is conditional, not a placeholder; the engineer must either find the real position ID or document why the proxy was used. Acceptable.
- Step 10.1 says "search before editing" with an exact `grep` command — this is direction, not a placeholder.
- Step 10.2 has a `t.Skip("TODO: fill in...")` *as part of the failing test*, with explicit instruction to un-skip in the next step. This is intentional TDD scaffolding, not a leftover.

No remaining "TODO / TBD / fill in details" in implementation code.

**3. Type consistency**

- `DecisionRecord` field names match across entity definition, repo SQL bindings, recorder construction, and integration test ✓
- `RejectedSignalEvent.Stage` uses `RejectedStageRisk` / `RejectedStageBookGate` consistently ✓
- `OrderEvent.Trigger` uses `DecisionTrigger*` constants consistently ✓
- `RecorderConfig.StanceProvider` is `func() string` everywhere ✓
- `DecisionLogFilter` field names (`SymbolID`, `From`, `To`, `Cursor`, `Limit`) match between interface and repo implementation ✓
- `BacktestDecisionLogRepository.ListByRun` signature `(ctx, runID, limit, cursor)` matches across interface, repo, and stub ✓

No naming drift detected.
