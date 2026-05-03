# ExitPlan Phase 1: ドメイン + Repository + シャドウ運用

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ExitPlan ドメインエンティティと SQLite Repository を実装し、約定イベントを契機にシャドウで ExitPlan を作成・close する wiring を入れる。既存 RiskManager の挙動は一切変更しない（観察モード）。

**Architecture:** `internal/domain/risk/policy.go` の既存 `RiskPolicy` (StopLossSpec / TakeProfitSpec / TrailingSpec) を再利用し、ExitPlan エンティティはこれを embed する形で構築する。OrderEvent をシャドウで listen する `ExitPlanShadowHandler` を EventBus に追加し、約定 → ExitPlan 作成、close 約定 → ExitPlan close を行う。本 Phase では SL/TP/Trailing の発火判定や HWM 更新は行わない（Phase 2）。

**Tech Stack:** Go 1.25, SQLite, Gin, EventEngine (existing), 既存 testing パターン (`go test ./... -race -count=1`).

**関連設計書:** `docs/superpowers/specs/2026-05-04-exit-plan-first-class-design.md`

**Branch strategy:** `docs/exit-plan-first-class` を base に新規ブランチ `feat/exit-plan-phase1-shadow` を切る。Phase 2/3 はこのブランチを base に Stacked PR として積む。

---

## File Structure

新規作成:
- `backend/internal/domain/entity/exit_plan.go` — ExitPlan エンティティ + 不変条件
- `backend/internal/domain/entity/exit_plan_test.go` — 不変条件・計算ロジックの table-driven test
- `backend/internal/domain/repository/exit_plan_repository.go` — Repository インタフェース
- `backend/internal/infrastructure/database/exit_plan_repo.go` — SQLite 実装
- `backend/internal/infrastructure/database/exit_plan_repo_test.go` — DB 統合テスト
- `backend/internal/usecase/exitplan/shadow_handler.go` — OrderEvent を listen するシャドウ handler
- `backend/internal/usecase/exitplan/shadow_handler_test.go` — handler 単体テスト

修正:
- `backend/internal/infrastructure/database/migrations.go` — `exit_plans` テーブル追加
- `backend/internal/infrastructure/database/migrations_test.go` — マイグレーションのスキーマ確認
- `backend/cmd/event_pipeline.go` — ExitPlanShadowHandler を EventBus に register

---

### Task 1: ExitPlan ドメインエンティティ

**Files:**
- Create: `backend/internal/domain/entity/exit_plan.go`
- Test: `backend/internal/domain/entity/exit_plan_test.go`

**Notes:**
- 既存 `internal/domain/risk` package の `StopLossSpec` / `TakeProfitSpec` / `TrailingSpec` を再利用する。entity package が risk package を import する依存方向は OK（domain 内部の参照）。
- Phase 1 では SL/TP の current price 計算メソッドは追加しない（Phase 2 で `CurrentSLPrice`, `CurrentTPPrice`, `CurrentTrailingTriggerPrice` を実装する）。本 Task ではエンティティ + 不変条件 + コンストラクタのみ。

- [ ] **Step 1: 失敗するテストを書く**

```go
// backend/internal/domain/entity/exit_plan_test.go
package entity

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

func TestNewExitPlan_validInputs(t *testing.T) {
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
	}
	plan, err := NewExitPlan(NewExitPlanInput{
		PositionID: 100,
		SymbolID:   7,
		Side:       OrderSideBuy,
		EntryPrice: 10000,
		Policy:     policy,
		CreatedAt:  1700000000000,
	})
	if err != nil {
		t.Fatalf("NewExitPlan: %v", err)
	}
	if plan.PositionID != 100 || plan.SymbolID != 7 || plan.Side != OrderSideBuy {
		t.Errorf("identity fields wrong: %+v", plan)
	}
	if plan.EntryPrice != 10000 {
		t.Errorf("EntryPrice = %v, want 10000", plan.EntryPrice)
	}
	if plan.Policy.StopLoss.Percent != 1.5 {
		t.Errorf("policy not embedded: %+v", plan.Policy)
	}
	if plan.TrailingActivated {
		t.Errorf("TrailingActivated should default false")
	}
	if plan.TrailingHWM != nil {
		t.Errorf("TrailingHWM should default nil; got %v", *plan.TrailingHWM)
	}
	if plan.ClosedAt != nil {
		t.Errorf("ClosedAt should default nil")
	}
}

func TestNewExitPlan_validation(t *testing.T) {
	validPolicy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeDisabled},
	}
	cases := []struct {
		name    string
		input   NewExitPlanInput
		wantErr string
	}{
		{
			"zero PositionID",
			NewExitPlanInput{PositionID: 0, SymbolID: 7, Side: OrderSideBuy, EntryPrice: 10000, Policy: validPolicy, CreatedAt: 1},
			"PositionID must be > 0",
		},
		{
			"zero SymbolID",
			NewExitPlanInput{PositionID: 1, SymbolID: 0, Side: OrderSideBuy, EntryPrice: 10000, Policy: validPolicy, CreatedAt: 1},
			"SymbolID must be > 0",
		},
		{
			"empty Side",
			NewExitPlanInput{PositionID: 1, SymbolID: 7, Side: "", EntryPrice: 10000, Policy: validPolicy, CreatedAt: 1},
			"Side must be BUY or SELL",
		},
		{
			"non-positive EntryPrice",
			NewExitPlanInput{PositionID: 1, SymbolID: 7, Side: OrderSideBuy, EntryPrice: 0, Policy: validPolicy, CreatedAt: 1},
			"EntryPrice must be > 0",
		},
		{
			"invalid policy",
			NewExitPlanInput{PositionID: 1, SymbolID: 7, Side: OrderSideBuy, EntryPrice: 10000, Policy: risk.RiskPolicy{}, CreatedAt: 1},
			"invalid policy",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewExitPlan(tc.input)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestExitPlan_RaiseTrailingHWM_long(t *testing.T) {
	plan := mustNewExitPlan(t, 100, OrderSideBuy, 10000)
	// 含み益未到達 (HWM <= EntryPrice) は no-op
	if changed := plan.RaiseTrailingHWM(9990, 1); changed {
		t.Errorf("loss-side tick should not raise HWM")
	}
	if plan.TrailingActivated || plan.TrailingHWM != nil {
		t.Errorf("HWM must remain unactivated; got activated=%v hwm=%+v", plan.TrailingActivated, plan.TrailingHWM)
	}
	// 初めての含み益超え → Activated + HWM = price
	if changed := plan.RaiseTrailingHWM(10050, 2); !changed {
		t.Errorf("first profit tick should activate HWM")
	}
	if !plan.TrailingActivated || plan.TrailingHWM == nil || *plan.TrailingHWM != 10050 {
		t.Errorf("activation wrong: activated=%v hwm=%+v", plan.TrailingActivated, plan.TrailingHWM)
	}
	if plan.UpdatedAt != 2 {
		t.Errorf("UpdatedAt not refreshed; got %v", plan.UpdatedAt)
	}
	// より高い高値で更新
	if changed := plan.RaiseTrailingHWM(10100, 3); !changed {
		t.Errorf("higher high should update HWM")
	}
	if *plan.TrailingHWM != 10100 {
		t.Errorf("HWM = %v, want 10100", *plan.TrailingHWM)
	}
	// より低い tick は no-op
	if changed := plan.RaiseTrailingHWM(10080, 4); changed {
		t.Errorf("lower tick should not change HWM")
	}
	if *plan.TrailingHWM != 10100 {
		t.Errorf("HWM regressed: %v", *plan.TrailingHWM)
	}
}

func TestExitPlan_RaiseTrailingHWM_short(t *testing.T) {
	plan := mustNewExitPlan(t, 100, OrderSideSell, 10000)
	// ショートは安値方向で活性化
	if changed := plan.RaiseTrailingHWM(10020, 1); changed {
		t.Errorf("short loss-side tick should not raise HWM")
	}
	if changed := plan.RaiseTrailingHWM(9950, 2); !changed {
		t.Errorf("short first profit should activate HWM")
	}
	if !plan.TrailingActivated || plan.TrailingHWM == nil || *plan.TrailingHWM != 9950 {
		t.Errorf("short activation wrong: %+v", plan)
	}
	if changed := plan.RaiseTrailingHWM(9900, 3); !changed {
		t.Errorf("lower low should update short HWM")
	}
	if *plan.TrailingHWM != 9900 {
		t.Errorf("short HWM = %v, want 9900", *plan.TrailingHWM)
	}
}

func TestExitPlan_Close_invariant(t *testing.T) {
	plan := mustNewExitPlan(t, 100, OrderSideBuy, 10000)
	if err := plan.Close(1700000000999); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if plan.ClosedAt == nil || *plan.ClosedAt != 1700000000999 {
		t.Errorf("ClosedAt = %+v, want 1700000000999", plan.ClosedAt)
	}
	// 二重 close は禁止
	if err := plan.Close(1700000001000); err == nil {
		t.Errorf("second Close should error")
	}
	// closed の plan で HWM 更新も禁止
	if plan.RaiseTrailingHWM(10050, 1700000001001) {
		t.Errorf("RaiseTrailingHWM on closed plan should be no-op")
	}
}

// --- helpers ---

func mustNewExitPlan(t *testing.T, posID int64, side OrderSide, entry float64) *ExitPlan {
	t.Helper()
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
	}
	plan, err := NewExitPlan(NewExitPlanInput{
		PositionID: posID,
		SymbolID:   7,
		Side:       side,
		EntryPrice: entry,
		Policy:     policy,
		CreatedAt:  1700000000000,
	})
	if err != nil {
		t.Fatalf("NewExitPlan: %v", err)
	}
	return plan
}

func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd backend && go test ./internal/domain/entity/ -run TestNewExitPlan -count=1`
Expected: コンパイルエラー（`NewExitPlan` undefined）

- [ ] **Step 3: ExitPlan エンティティを実装**

```go
// backend/internal/domain/entity/exit_plan.go
package entity

import (
	"errors"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

// ExitPlan は建玉と 1:1 で対応する出口管理エンティティ。SL/TP のルールは
// risk.RiskPolicy として保存し、現在価格は read 時に動的計算する（ATR
// レジーム変化への追従を許容）。Trailing の HWM だけが動的状態として
// 永続化される。
//
// Phase 1 ではシャドウ運用のみ。Phase 2 で SL/TP/Trailing 発火判定と
// CurrentSLPrice / CurrentTPPrice / CurrentTrailingTriggerPrice を追加する。
type ExitPlan struct {
	ID         int64
	PositionID int64
	SymbolID   int64
	Side       OrderSide
	EntryPrice float64
	Policy     risk.RiskPolicy

	TrailingActivated bool
	TrailingHWM       *float64

	CreatedAt int64
	UpdatedAt int64
	ClosedAt  *int64
}

// NewExitPlanInput は NewExitPlan の入力。コンストラクタ専用の record 型。
type NewExitPlanInput struct {
	PositionID int64
	SymbolID   int64
	Side       OrderSide
	EntryPrice float64
	Policy     risk.RiskPolicy
	CreatedAt  int64
}

// NewExitPlan は不変条件を検証して新しい ExitPlan を返す。Repository の
// Create で永続化する前に必ずこれを通すことで「無防備な建玉」を防ぐ。
func NewExitPlan(in NewExitPlanInput) (*ExitPlan, error) {
	if in.PositionID <= 0 {
		return nil, errors.New("ExitPlan: PositionID must be > 0")
	}
	if in.SymbolID <= 0 {
		return nil, errors.New("ExitPlan: SymbolID must be > 0")
	}
	if in.Side != OrderSideBuy && in.Side != OrderSideSell {
		return nil, fmt.Errorf("ExitPlan: Side must be BUY or SELL (got %q)", in.Side)
	}
	if in.EntryPrice <= 0 {
		return nil, fmt.Errorf("ExitPlan: EntryPrice must be > 0 (got %v)", in.EntryPrice)
	}
	if err := in.Policy.Validate(); err != nil {
		return nil, fmt.Errorf("ExitPlan: invalid policy: %w", err)
	}
	if in.CreatedAt <= 0 {
		return nil, errors.New("ExitPlan: CreatedAt must be > 0")
	}
	return &ExitPlan{
		PositionID: in.PositionID,
		SymbolID:   in.SymbolID,
		Side:       in.Side,
		EntryPrice: in.EntryPrice,
		Policy:     in.Policy,
		CreatedAt:  in.CreatedAt,
		UpdatedAt:  in.CreatedAt,
	}, nil
}

// IsClosed は close 済みか判定する。
func (e *ExitPlan) IsClosed() bool {
	return e.ClosedAt != nil
}

// RaiseTrailingHWM は新しい tick 価格で Trailing の最良値を更新する。
// 含み益超え（ロング: price > EntryPrice、ショート: price < EntryPrice）で
// 初めて呼ばれた瞬間に Activated を true にし HWM を price で初期化する。
// その後はロングなら新高値、ショートなら新安値のときだけ更新する。
//
// 戻り値は HWM が変化したか（=永続化が必要か）。closed plan に対して
// 呼ばれた場合や no-op の場合は false。
func (e *ExitPlan) RaiseTrailingHWM(price float64, now int64) bool {
	if e.IsClosed() {
		return false
	}
	if !e.TrailingActivated {
		// 含み益超えで初活性化
		switch e.Side {
		case OrderSideBuy:
			if price <= e.EntryPrice {
				return false
			}
		case OrderSideSell:
			if price >= e.EntryPrice {
				return false
			}
		}
		e.TrailingActivated = true
		hwm := price
		e.TrailingHWM = &hwm
		e.UpdatedAt = now
		return true
	}
	// 既に activated。単調性を満たすときのみ更新
	if e.TrailingHWM == nil {
		// 不整合（activated だが HWM nil）。通常起こらないが防御
		hwm := price
		e.TrailingHWM = &hwm
		e.UpdatedAt = now
		return true
	}
	switch e.Side {
	case OrderSideBuy:
		if price > *e.TrailingHWM {
			*e.TrailingHWM = price
			e.UpdatedAt = now
			return true
		}
	case OrderSideSell:
		if price < *e.TrailingHWM {
			*e.TrailingHWM = price
			e.UpdatedAt = now
			return true
		}
	}
	return false
}

// Close は ExitPlan を closed 状態に遷移させる。二重 close はエラー。
func (e *ExitPlan) Close(now int64) error {
	if e.IsClosed() {
		return errors.New("ExitPlan: already closed")
	}
	e.ClosedAt = &now
	e.UpdatedAt = now
	return nil
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd backend && go test ./internal/domain/entity/ -run TestNewExitPlan -count=1 -v && go test ./internal/domain/entity/ -run TestExitPlan -count=1 -v`
Expected: 全テスト PASS

- [ ] **Step 5: コミット**

```bash
git add backend/internal/domain/entity/exit_plan.go backend/internal/domain/entity/exit_plan_test.go
git commit -m "feat(exit-plan): add ExitPlan domain entity with invariants"
```

---

### Task 2: ExitPlanRepository インタフェース

**Files:**
- Create: `backend/internal/domain/repository/exit_plan_repository.go`

**Notes:**
- ドメインレイヤは実装を持たないインタフェースのみ。Task 3 で SQLite 実装を別ファイルに置く。

- [ ] **Step 1: Repository インタフェースを書く**

```go
// backend/internal/domain/repository/exit_plan_repository.go
package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ExitPlanRepository は建玉ごとの ExitPlan を永続化する。SL/TP のルールは
// 不変なので Create 時に書ききり、変化点（Trailing 活性化・HWM 更新・close）
// だけを書き込む I/O 設計。
type ExitPlanRepository interface {
	// Create は新規 ExitPlan を永続化する。PositionID は unique 制約で
	// 同じ建玉に対して二重 ExitPlan が作られない。
	Create(ctx context.Context, plan *entity.ExitPlan) error

	// FindByPositionID は建玉 ID で ExitPlan を引く。closed 含む全件。
	// 見つからない場合は (nil, nil)。
	FindByPositionID(ctx context.Context, positionID int64) (*entity.ExitPlan, error)

	// ListOpen は ClosedAt IS NULL の ExitPlan を symbol_id で絞って返す。
	// Phase 2 の tick handler が毎 tick これを呼ぶので、走査効率を意識する。
	ListOpen(ctx context.Context, symbolID int64) ([]*entity.ExitPlan, error)

	// UpdateTrailing は HWM と Activated フラグだけを更新する。SL/TP の
	// ルール部分は変更しない。closed な plan に対してはエラー。
	UpdateTrailing(ctx context.Context, planID int64, hwm float64, activated bool, updatedAt int64) error

	// Close は ClosedAt を立てる。二重 close はエラー。
	Close(ctx context.Context, planID int64, closedAt int64) error
}
```

- [ ] **Step 2: package がビルドできることを確認**

Run: `cd backend && go build ./internal/domain/repository/...`
Expected: エラーなし

- [ ] **Step 3: コミット**

```bash
git add backend/internal/domain/repository/exit_plan_repository.go
git commit -m "feat(exit-plan): add ExitPlanRepository interface"
```

---

### Task 3: マイグレーション追加（exit_plans テーブル）

**Files:**
- Modify: `backend/internal/infrastructure/database/migrations.go`
- Test: `backend/internal/infrastructure/database/migrations_test.go`

**Notes:**
- 既存の `migrations := []string{ ... }` の末尾に追加。`addDecisionLogV2Columns` のように関数化はせず素直に CREATE TABLE で OK（新規テーブルなので）。
- `position_id` を UNIQUE にして 1:1 制約を保証。
- policy は SL/TP/Trailing の各値をカラム化（JSON でなく素直に列にする：read 時のクエリやデバッグが楽）。
- Trailing HWM は NULL 許容（未活性は NULL）。

- [ ] **Step 1: 失敗するテストを書く**

```go
// backend/internal/infrastructure/database/migrations_test.go の末尾に追加

func TestRunMigrations_createsExitPlansTable(t *testing.T) {
	db := openTestDB(t)
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	rows, err := db.Query("PRAGMA table_info(exit_plans)")
	if err != nil {
		t.Fatalf("pragma table_info(exit_plans): %v", err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    interface{}
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = true
	}
	want := []string{
		"id", "position_id", "symbol_id", "side", "entry_price",
		"sl_percent", "sl_atr_multiplier",
		"tp_percent",
		"trailing_mode", "trailing_atr_multiplier",
		"trailing_activated", "trailing_hwm",
		"created_at", "updated_at", "closed_at",
	}
	for _, c := range want {
		if !cols[c] {
			t.Errorf("exit_plans missing column %q", c)
		}
	}
	// position_id の UNIQUE 制約
	idxRows, err := db.Query("PRAGMA index_list(exit_plans)")
	if err != nil {
		t.Fatalf("pragma index_list: %v", err)
	}
	defer idxRows.Close()
	hasUniqueOnPositionID := false
	for idxRows.Next() {
		var (
			seq    int
			name   string
			unique int
			origin string
			partial int
		)
		if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan idx_list: %v", err)
		}
		if unique != 1 {
			continue
		}
		// このインデックスのカラムを取得
		colRows, err := db.Query(fmt.Sprintf("PRAGMA index_info(%s)", name))
		if err != nil {
			t.Fatalf("pragma index_info(%s): %v", name, err)
		}
		var seqno, cid int
		var cname string
		for colRows.Next() {
			if err := colRows.Scan(&seqno, &cid, &cname); err != nil {
				colRows.Close()
				t.Fatalf("scan idx_info: %v", err)
			}
			if cname == "position_id" {
				hasUniqueOnPositionID = true
			}
		}
		colRows.Close()
	}
	if !hasUniqueOnPositionID {
		t.Errorf("exit_plans should have UNIQUE constraint on position_id")
	}
}
```

注: `openTestDB` は既存ヘルパー（`migrations_test.go` 内）。`fmt` の import を test ファイル冒頭に追加。

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd backend && go test ./internal/infrastructure/database/ -run TestRunMigrations_createsExitPlansTable -count=1`
Expected: FAIL（テーブルが存在しないため `pragma table_info` が空）

- [ ] **Step 3: マイグレーション追加**

`backend/internal/infrastructure/database/migrations.go` の `migrations := []string{ ... }` の末尾、`backtest_decision_log` の index 定義の後（`}` の直前）に以下を挿入:

```go
		`CREATE TABLE IF NOT EXISTS exit_plans (
			id                      INTEGER PRIMARY KEY AUTOINCREMENT,
			position_id             INTEGER NOT NULL UNIQUE,
			symbol_id               INTEGER NOT NULL,
			side                    TEXT NOT NULL,
			entry_price             REAL NOT NULL,
			sl_percent              REAL NOT NULL,
			sl_atr_multiplier       REAL NOT NULL DEFAULT 0,
			tp_percent              REAL NOT NULL,
			trailing_mode           INTEGER NOT NULL DEFAULT 0,
			trailing_atr_multiplier REAL NOT NULL DEFAULT 0,
			trailing_activated      INTEGER NOT NULL DEFAULT 0,
			trailing_hwm            REAL,
			created_at              INTEGER NOT NULL,
			updated_at              INTEGER NOT NULL,
			closed_at               INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_exit_plans_symbol_open
			ON exit_plans(symbol_id, closed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_exit_plans_position
			ON exit_plans(position_id)`,
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd backend && go test ./internal/infrastructure/database/ -run TestRunMigrations -count=1`
Expected: PASS（既存テストもすべて緑）

- [ ] **Step 5: コミット**

```bash
git add backend/internal/infrastructure/database/migrations.go backend/internal/infrastructure/database/migrations_test.go
git commit -m "feat(exit-plan): add exit_plans table migration"
```

---

### Task 4: SQLite Repository 実装

**Files:**
- Create: `backend/internal/infrastructure/database/exit_plan_repo.go`
- Test: `backend/internal/infrastructure/database/exit_plan_repo_test.go`

**Notes:**
- 既存の `decision_log_repo.go` の構造に倣う（`db *sql.DB`、`NewExitPlanRepository(db) repository.ExitPlanRepository`）
- TrailingMode を int で保存（risk package の iota 値）。読み戻すときに `risk.TrailingMode` にキャスト。
- `trailing_hwm` は NULL 許容なので `sql.NullFloat64` で扱う。
- `closed_at` も同様に `sql.NullInt64`。

- [ ] **Step 1: 失敗するテストを書く**

```go
// backend/internal/infrastructure/database/exit_plan_repo_test.go
package database

import (
	"context"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

func TestExitPlanRepo_CreateAndFind(t *testing.T) {
	db := openTestDB(t)
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewExitPlanRepository(db)
	ctx := context.Background()

	plan := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := repo.Create(ctx, plan); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if plan.ID == 0 {
		t.Errorf("ID should be assigned after Create")
	}
	got, err := repo.FindByPositionID(ctx, 100)
	if err != nil {
		t.Fatalf("FindByPositionID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByPositionID: nil")
	}
	if got.PositionID != 100 || got.SymbolID != 7 || got.Side != entity.OrderSideBuy || got.EntryPrice != 10000 {
		t.Errorf("got = %+v", got)
	}
	if got.Policy.StopLoss.Percent != 1.5 || got.Policy.StopLoss.ATRMultiplier != 2.0 {
		t.Errorf("policy SL not roundtripped: %+v", got.Policy.StopLoss)
	}
	if got.Policy.TakeProfit.Percent != 3.0 {
		t.Errorf("policy TP not roundtripped: %+v", got.Policy.TakeProfit)
	}
	if got.Policy.Trailing.Mode != risk.TrailingModeATR || got.Policy.Trailing.ATRMultiplier != 2.5 {
		t.Errorf("policy trailing not roundtripped: %+v", got.Policy.Trailing)
	}
	if got.TrailingActivated || got.TrailingHWM != nil {
		t.Errorf("trailing should default not activated: %+v", got)
	}
	if got.ClosedAt != nil {
		t.Errorf("ClosedAt should default nil")
	}
}

func TestExitPlanRepo_Create_uniquePositionID(t *testing.T) {
	db := openTestDB(t)
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewExitPlanRepository(db)
	ctx := context.Background()
	p1 := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := repo.Create(ctx, p1); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	p2 := mustExitPlanForRepo(t, 100, 7, entity.OrderSideSell, 11000)
	if err := repo.Create(ctx, p2); err == nil {
		t.Errorf("second Create with same PositionID should fail")
	}
}

func TestExitPlanRepo_ListOpen_excludesClosed(t *testing.T) {
	db := openTestDB(t)
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewExitPlanRepository(db)
	ctx := context.Background()
	open := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	closed := mustExitPlanForRepo(t, 101, 7, entity.OrderSideSell, 11000)
	otherSym := mustExitPlanForRepo(t, 102, 8, entity.OrderSideBuy, 5000)
	for _, p := range []*entity.ExitPlan{open, closed, otherSym} {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := repo.Close(ctx, closed.ID, 1700000099999); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := repo.ListOpen(ctx, 7)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListOpen returned %d, want 1", len(got))
	}
	if got[0].PositionID != 100 {
		t.Errorf("expected open plan position 100, got %d", got[0].PositionID)
	}
}

func TestExitPlanRepo_UpdateTrailing(t *testing.T) {
	db := openTestDB(t)
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewExitPlanRepository(db)
	ctx := context.Background()
	plan := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := repo.Create(ctx, plan); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.UpdateTrailing(ctx, plan.ID, 10250, true, 1700000050000); err != nil {
		t.Fatalf("UpdateTrailing: %v", err)
	}
	got, _ := repo.FindByPositionID(ctx, 100)
	if !got.TrailingActivated {
		t.Errorf("TrailingActivated should be true")
	}
	if got.TrailingHWM == nil || *got.TrailingHWM != 10250 {
		t.Errorf("TrailingHWM = %+v, want 10250", got.TrailingHWM)
	}
	if got.UpdatedAt != 1700000050000 {
		t.Errorf("UpdatedAt = %v, want 1700000050000", got.UpdatedAt)
	}
}

func TestExitPlanRepo_Close(t *testing.T) {
	db := openTestDB(t)
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewExitPlanRepository(db)
	ctx := context.Background()
	plan := mustExitPlanForRepo(t, 100, 7, entity.OrderSideBuy, 10000)
	if err := repo.Create(ctx, plan); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Close(ctx, plan.ID, 1700000099999); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, _ := repo.FindByPositionID(ctx, 100)
	if got.ClosedAt == nil || *got.ClosedAt != 1700000099999 {
		t.Errorf("ClosedAt = %+v, want 1700000099999", got.ClosedAt)
	}
}

func mustExitPlanForRepo(t *testing.T, posID, symID int64, side entity.OrderSide, entry float64) *entity.ExitPlan {
	t.Helper()
	plan, err := entity.NewExitPlan(entity.NewExitPlanInput{
		PositionID: posID,
		SymbolID:   symID,
		Side:       side,
		EntryPrice: entry,
		Policy: risk.RiskPolicy{
			StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
			TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
			Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
		},
		CreatedAt: 1700000000000,
	})
	if err != nil {
		t.Fatalf("NewExitPlan: %v", err)
	}
	return plan
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd backend && go test ./internal/infrastructure/database/ -run TestExitPlanRepo -count=1`
Expected: コンパイルエラー（`NewExitPlanRepository` undefined）

- [ ] **Step 3: SQLite 実装を書く**

```go
// backend/internal/infrastructure/database/exit_plan_repo.go
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

type exitPlanRepo struct {
	db *sql.DB
}

// NewExitPlanRepository は ExitPlanRepository を SQLite で実装したものを返す。
// DB は RunMigrations 済みであること。
func NewExitPlanRepository(db *sql.DB) repository.ExitPlanRepository {
	return &exitPlanRepo{db: db}
}

func (r *exitPlanRepo) Create(ctx context.Context, plan *entity.ExitPlan) error {
	if plan == nil {
		return errors.New("exitPlanRepo.Create: nil plan")
	}
	const q = `
		INSERT INTO exit_plans (
			position_id, symbol_id, side, entry_price,
			sl_percent, sl_atr_multiplier,
			tp_percent,
			trailing_mode, trailing_atr_multiplier,
			trailing_activated, trailing_hwm,
			created_at, updated_at, closed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	res, err := r.db.ExecContext(ctx, q,
		plan.PositionID, plan.SymbolID, string(plan.Side), plan.EntryPrice,
		plan.Policy.StopLoss.Percent, plan.Policy.StopLoss.ATRMultiplier,
		plan.Policy.TakeProfit.Percent,
		int(plan.Policy.Trailing.Mode), plan.Policy.Trailing.ATRMultiplier,
		boolToInt(plan.TrailingActivated), nullableFloat(plan.TrailingHWM),
		plan.CreatedAt, plan.UpdatedAt, nullableInt64(plan.ClosedAt),
	)
	if err != nil {
		return fmt.Errorf("insert exit_plans: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("LastInsertId exit_plans: %w", err)
	}
	plan.ID = id
	return nil
}

func (r *exitPlanRepo) FindByPositionID(ctx context.Context, positionID int64) (*entity.ExitPlan, error) {
	const q = `
		SELECT id, position_id, symbol_id, side, entry_price,
		       sl_percent, sl_atr_multiplier,
		       tp_percent,
		       trailing_mode, trailing_atr_multiplier,
		       trailing_activated, trailing_hwm,
		       created_at, updated_at, closed_at
		FROM exit_plans
		WHERE position_id = ?
	`
	row := r.db.QueryRowContext(ctx, q, positionID)
	plan, err := scanExitPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("FindByPositionID: %w", err)
	}
	return plan, nil
}

func (r *exitPlanRepo) ListOpen(ctx context.Context, symbolID int64) ([]*entity.ExitPlan, error) {
	const q = `
		SELECT id, position_id, symbol_id, side, entry_price,
		       sl_percent, sl_atr_multiplier,
		       tp_percent,
		       trailing_mode, trailing_atr_multiplier,
		       trailing_activated, trailing_hwm,
		       created_at, updated_at, closed_at
		FROM exit_plans
		WHERE symbol_id = ? AND closed_at IS NULL
		ORDER BY id ASC
	`
	rows, err := r.db.QueryContext(ctx, q, symbolID)
	if err != nil {
		return nil, fmt.Errorf("ListOpen query: %w", err)
	}
	defer rows.Close()
	var out []*entity.ExitPlan
	for rows.Next() {
		plan, err := scanExitPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("ListOpen scan: %w", err)
		}
		out = append(out, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListOpen iter: %w", err)
	}
	return out, nil
}

func (r *exitPlanRepo) UpdateTrailing(ctx context.Context, planID int64, hwm float64, activated bool, updatedAt int64) error {
	const q = `
		UPDATE exit_plans
		SET trailing_activated = ?, trailing_hwm = ?, updated_at = ?
		WHERE id = ? AND closed_at IS NULL
	`
	res, err := r.db.ExecContext(ctx, q, boolToInt(activated), hwm, updatedAt, planID)
	if err != nil {
		return fmt.Errorf("UpdateTrailing: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("UpdateTrailing: plan id=%d not found or already closed", planID)
	}
	return nil
}

func (r *exitPlanRepo) Close(ctx context.Context, planID int64, closedAt int64) error {
	const q = `
		UPDATE exit_plans
		SET closed_at = ?, updated_at = ?
		WHERE id = ? AND closed_at IS NULL
	`
	res, err := r.db.ExecContext(ctx, q, closedAt, closedAt, planID)
	if err != nil {
		return fmt.Errorf("Close: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("Close: plan id=%d not found or already closed", planID)
	}
	return nil
}

// rowScanner は *sql.Row と *sql.Rows の Scan を共通化するための最小インタフェース。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanExitPlan(s rowScanner) (*entity.ExitPlan, error) {
	var (
		id              int64
		positionID      int64
		symbolID        int64
		side            string
		entryPrice      float64
		slPercent       float64
		slATRMult       float64
		tpPercent       float64
		trailingMode    int
		trailingATRMult float64
		trailingAct     int
		trailingHWM     sql.NullFloat64
		createdAt       int64
		updatedAt       int64
		closedAt        sql.NullInt64
	)
	if err := s.Scan(
		&id, &positionID, &symbolID, &side, &entryPrice,
		&slPercent, &slATRMult,
		&tpPercent,
		&trailingMode, &trailingATRMult,
		&trailingAct, &trailingHWM,
		&createdAt, &updatedAt, &closedAt,
	); err != nil {
		return nil, err
	}
	plan := &entity.ExitPlan{
		ID:         id,
		PositionID: positionID,
		SymbolID:   symbolID,
		Side:       entity.OrderSide(side),
		EntryPrice: entryPrice,
		Policy: risk.RiskPolicy{
			StopLoss:   risk.StopLossSpec{Percent: slPercent, ATRMultiplier: slATRMult},
			TakeProfit: risk.TakeProfitSpec{Percent: tpPercent},
			Trailing:   risk.TrailingSpec{Mode: risk.TrailingMode(trailingMode), ATRMultiplier: trailingATRMult},
		},
		TrailingActivated: trailingAct == 1,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}
	if trailingHWM.Valid {
		v := trailingHWM.Float64
		plan.TrailingHWM = &v
	}
	if closedAt.Valid {
		v := closedAt.Int64
		plan.ClosedAt = &v
	}
	return plan, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd backend && go test ./internal/infrastructure/database/ -run TestExitPlanRepo -count=1 -v`
Expected: 全 4 テスト PASS

- [ ] **Step 5: 全 DB テストが緑であることを確認**

Run: `cd backend && go test ./internal/infrastructure/database/ -count=1 -race`
Expected: PASS

- [ ] **Step 6: コミット**

```bash
git add backend/internal/infrastructure/database/exit_plan_repo.go backend/internal/infrastructure/database/exit_plan_repo_test.go
git commit -m "feat(exit-plan): SQLite ExitPlanRepository implementation"
```

---

### Task 5: ExitPlanShadowHandler（OrderEvent をシャドウで listen）

**Files:**
- Create: `backend/internal/usecase/exitplan/shadow_handler.go`
- Create: `backend/internal/usecase/exitplan/shadow_handler_test.go`

**Notes:**
- `eventengine.EventHandler` インタフェースを満たす（既存 `decision/handler.go` と同じ pattern）
- 入力: `entity.OrderEvent`（`OpenedPositionID != 0` で新規約定、`ClosedPositionID != 0` で close 約定）
- 出力: シャドウなので emit するイベントなし（戻り値 `[]entity.Event` は空）
- 失敗時のリトライは Phase 1 では実装しない。設計書 §8.2 のリトライ + Halt は Phase 2 で実装する。Phase 1 では失敗時に warn ログだけ出して握り潰す（シャドウなので production への影響なし）。
- ExitPlan 作成時に必要な `RiskPolicy` は handler 構築時に注入（pipeline 起動時に snapshot した policy を使う）

- [ ] **Step 1: 失敗するテストを書く**

```go
// backend/internal/usecase/exitplan/shadow_handler_test.go
package exitplan

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

func TestShadowHandler_OpenedPosition_createsExitPlan(t *testing.T) {
	repo := newMemRepo()
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
	}
	h := NewShadowHandler(ShadowHandlerConfig{
		Repo:   repo,
		Policy: policy,
	})

	ev := entity.OrderEvent{
		SymbolID:         7,
		Side:             "BUY",
		Action:           "OPEN",
		Price:            10000,
		Amount:           0.1,
		Timestamp:        1700000000000,
		OpenedPositionID: 100,
	}
	out, err := h.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("shadow handler should emit no events; got %d", len(out))
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 ExitPlan created, got %d", len(repo.created))
	}
	got := repo.created[0]
	if got.PositionID != 100 || got.SymbolID != 7 || got.Side != entity.OrderSideBuy || got.EntryPrice != 10000 {
		t.Errorf("ExitPlan wrong: %+v", got)
	}
}

func TestShadowHandler_ClosedPosition_closesExitPlan(t *testing.T) {
	repo := newMemRepo()
	// 事前に open plan を作っておく
	plan, _ := entity.NewExitPlan(entity.NewExitPlanInput{
		PositionID: 100, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy: risk.RiskPolicy{
			StopLoss: risk.StopLossSpec{Percent: 1.5}, TakeProfit: risk.TakeProfitSpec{Percent: 3},
			Trailing: risk.TrailingSpec{Mode: risk.TrailingModePercent},
		},
		CreatedAt: 1700000000000,
	})
	plan.ID = 999
	repo.byPosition[100] = plan

	h := NewShadowHandler(ShadowHandlerConfig{
		Repo: repo,
		Policy: risk.RiskPolicy{
			StopLoss: risk.StopLossSpec{Percent: 1.5}, TakeProfit: risk.TakeProfitSpec{Percent: 3},
			Trailing: risk.TrailingSpec{Mode: risk.TrailingModePercent},
		},
	})

	ev := entity.OrderEvent{
		SymbolID:         7,
		Side:             "SELL",
		Action:           "CLOSE",
		Price:            10500,
		Amount:           0.1,
		Timestamp:        1700000099999,
		ClosedPositionID: 100,
	}
	if _, err := h.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !repo.closeCalled {
		t.Errorf("Repo.Close should be called")
	}
	if repo.closedID != 999 || repo.closedAt != 1700000099999 {
		t.Errorf("close args wrong: id=%d at=%d", repo.closedID, repo.closedAt)
	}
}

func TestShadowHandler_OpenAndClose_inSameEvent(t *testing.T) {
	// reversal トレード: 1 OrderEvent で OpenedPositionID と ClosedPositionID
	// 両方が立つケース。両方の処理が走るべき。
	repo := newMemRepo()
	plan, _ := entity.NewExitPlan(entity.NewExitPlanInput{
		PositionID: 50, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 9500,
		Policy: risk.RiskPolicy{
			StopLoss: risk.StopLossSpec{Percent: 1.5}, TakeProfit: risk.TakeProfitSpec{Percent: 3},
			Trailing: risk.TrailingSpec{Mode: risk.TrailingModePercent},
		},
		CreatedAt: 1700000000000,
	})
	plan.ID = 555
	repo.byPosition[50] = plan

	h := NewShadowHandler(ShadowHandlerConfig{
		Repo: repo,
		Policy: risk.RiskPolicy{
			StopLoss: risk.StopLossSpec{Percent: 1.5}, TakeProfit: risk.TakeProfitSpec{Percent: 3},
			Trailing: risk.TrailingSpec{Mode: risk.TrailingModePercent},
		},
	})
	ev := entity.OrderEvent{
		SymbolID:         7,
		Price:            10000,
		Timestamp:        1700000050000,
		OpenedPositionID: 200,
		ClosedPositionID: 50,
		Side:             "SELL",
	}
	if _, err := h.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !repo.closeCalled {
		t.Errorf("close branch should fire")
	}
	if len(repo.created) != 1 || repo.created[0].PositionID != 200 {
		t.Errorf("open branch should create new ExitPlan for pos 200; got %+v", repo.created)
	}
}

func TestShadowHandler_OpenedPosition_inferSide_fromEvent(t *testing.T) {
	repo := newMemRepo()
	h := NewShadowHandler(ShadowHandlerConfig{
		Repo: repo,
		Policy: risk.RiskPolicy{
			StopLoss: risk.StopLossSpec{Percent: 1.5}, TakeProfit: risk.TakeProfitSpec{Percent: 3},
			Trailing: risk.TrailingSpec{Mode: risk.TrailingModePercent},
		},
	})
	cases := []struct {
		side string
		want entity.OrderSide
	}{
		{"BUY", entity.OrderSideBuy},
		{"SELL", entity.OrderSideSell},
	}
	for i, tc := range cases {
		ev := entity.OrderEvent{
			SymbolID: 7, Side: tc.side, Price: 10000, Timestamp: int64(1700000000000 + i),
			OpenedPositionID: int64(100 + i),
		}
		if _, err := h.Handle(context.Background(), ev); err != nil {
			t.Fatalf("case %s: %v", tc.side, err)
		}
	}
	if len(repo.created) != 2 {
		t.Fatalf("want 2 plans, got %d", len(repo.created))
	}
	if repo.created[0].Side != entity.OrderSideBuy || repo.created[1].Side != entity.OrderSideSell {
		t.Errorf("side inference failed: %+v %+v", repo.created[0].Side, repo.created[1].Side)
	}
}

func TestShadowHandler_NonOrderEvent_passThrough(t *testing.T) {
	repo := newMemRepo()
	h := NewShadowHandler(ShadowHandlerConfig{
		Repo: repo,
		Policy: risk.RiskPolicy{
			StopLoss: risk.StopLossSpec{Percent: 1.5}, TakeProfit: risk.TakeProfitSpec{Percent: 3},
			Trailing: risk.TrailingSpec{Mode: risk.TrailingModePercent},
		},
	})
	out, err := h.Handle(context.Background(), entity.TickEvent{})
	if err != nil {
		t.Fatalf("non-order event should not error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("non-order event should not emit; got %d", len(out))
	}
	if len(repo.created) != 0 || repo.closeCalled {
		t.Errorf("non-order event should not touch repo")
	}
}

func TestShadowHandler_RepoErrorIsSwallowed(t *testing.T) {
	repo := newMemRepo()
	repo.createErr = errors.New("disk full")
	h := NewShadowHandler(ShadowHandlerConfig{
		Repo: repo,
		Policy: risk.RiskPolicy{
			StopLoss: risk.StopLossSpec{Percent: 1.5}, TakeProfit: risk.TakeProfitSpec{Percent: 3},
			Trailing: risk.TrailingSpec{Mode: risk.TrailingModePercent},
		},
	})
	ev := entity.OrderEvent{
		SymbolID: 7, Side: "BUY", Price: 10000, Timestamp: 1700000000000,
		OpenedPositionID: 100,
	}
	out, err := h.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("shadow handler must not propagate repo errors (got %v)", err)
	}
	if len(out) != 0 {
		t.Errorf("non-order events should not be emitted")
	}
}

// --- in-memory repo for tests ---

type memRepo struct {
	mu          sync.Mutex
	byPosition  map[int64]*entity.ExitPlan
	created     []*entity.ExitPlan
	closeCalled bool
	closedID    int64
	closedAt    int64
	createErr   error
	closeErr    error
}

func newMemRepo() *memRepo {
	return &memRepo{byPosition: map[int64]*entity.ExitPlan{}}
}

func (m *memRepo) Create(_ context.Context, plan *entity.ExitPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	plan.ID = int64(len(m.created) + 1)
	m.created = append(m.created, plan)
	m.byPosition[plan.PositionID] = plan
	return nil
}
func (m *memRepo) FindByPositionID(_ context.Context, positionID int64) (*entity.ExitPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byPosition[positionID], nil
}
func (m *memRepo) ListOpen(_ context.Context, _ int64) ([]*entity.ExitPlan, error) {
	return nil, nil
}
func (m *memRepo) UpdateTrailing(_ context.Context, _ int64, _ float64, _ bool, _ int64) error {
	return nil
}
func (m *memRepo) Close(_ context.Context, planID int64, closedAt int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeErr != nil {
		return m.closeErr
	}
	m.closeCalled = true
	m.closedID = planID
	m.closedAt = closedAt
	return nil
}

var _ repository.ExitPlanRepository = (*memRepo)(nil)
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd backend && go test ./internal/usecase/exitplan/ -count=1`
Expected: コンパイルエラー（package が存在しない）

- [ ] **Step 3: ShadowHandler を実装**

```go
// backend/internal/usecase/exitplan/shadow_handler.go

// Package exitplan は ExitPlan を駆動するイベントハンドラを提供する。
//
// Phase 1 (シャドウ運用) では ShadowHandler が OrderEvent を listen して
// ExitPlan の作成・close だけを行う。SL/TP/Trailing の発火判定や HWM 更新は
// 既存 RiskManager / TickRiskHandler に任せたまま。観察ログを取って
// Phase 2 で発火経路を移管する。
package exitplan

import (
	"context"
	"log/slog"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

// ShadowHandlerConfig は ShadowHandler のコンストラクタ引数。
type ShadowHandlerConfig struct {
	Repo   repository.ExitPlanRepository
	Policy risk.RiskPolicy
	// Logger は省略可。nil の場合 slog.Default() を使う。
	Logger *slog.Logger
}

// ShadowHandler は OrderEvent をシャドウで listen し、ExitPlan を作成・close する。
// emit はせず、failure はログだけで握り潰す（既存の発注パスに影響を与えない）。
type ShadowHandler struct {
	repo   repository.ExitPlanRepository
	policy risk.RiskPolicy
	logger *slog.Logger
}

// NewShadowHandler は設定済みの ShadowHandler を返す。Repo nil は panic。
func NewShadowHandler(cfg ShadowHandlerConfig) *ShadowHandler {
	if cfg.Repo == nil {
		panic("exitplan.NewShadowHandler: Repo must not be nil")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ShadowHandler{
		repo:   cfg.Repo,
		policy: cfg.Policy,
		logger: logger.With("component", "exitplan_shadow"),
	}
}

// Handle implements eventengine.EventHandler. OrderEvent 以外は素通り。
func (h *ShadowHandler) Handle(ctx context.Context, ev entity.Event) ([]entity.Event, error) {
	oe, ok := ev.(entity.OrderEvent)
	if !ok {
		return nil, nil
	}
	// reversal なら open / close 両方走る
	if oe.ClosedPositionID != 0 {
		h.handleClose(ctx, oe)
	}
	if oe.OpenedPositionID != 0 {
		h.handleOpen(ctx, oe)
	}
	return nil, nil
}

func (h *ShadowHandler) handleOpen(ctx context.Context, oe entity.OrderEvent) {
	side := entity.OrderSide(oe.Side)
	if side != entity.OrderSideBuy && side != entity.OrderSideSell {
		h.logger.Warn("unknown order side, skipping shadow create",
			"side", oe.Side, "positionID", oe.OpenedPositionID,
		)
		return
	}
	plan, err := entity.NewExitPlan(entity.NewExitPlanInput{
		PositionID: oe.OpenedPositionID,
		SymbolID:   oe.SymbolID,
		Side:       side,
		EntryPrice: oe.Price,
		Policy:     h.policy,
		CreatedAt:  oe.Timestamp,
	})
	if err != nil {
		h.logger.Warn("shadow ExitPlan construction failed",
			"err", err, "positionID", oe.OpenedPositionID,
		)
		return
	}
	if err := h.repo.Create(ctx, plan); err != nil {
		h.logger.Warn("shadow ExitPlan persist failed",
			"err", err, "positionID", oe.OpenedPositionID,
		)
		return
	}
	h.logger.Info("shadow ExitPlan created",
		"positionID", oe.OpenedPositionID,
		"symbolID", oe.SymbolID,
		"side", oe.Side,
		"entryPrice", oe.Price,
		"planID", plan.ID,
	)
}

func (h *ShadowHandler) handleClose(ctx context.Context, oe entity.OrderEvent) {
	plan, err := h.repo.FindByPositionID(ctx, oe.ClosedPositionID)
	if err != nil {
		h.logger.Warn("shadow ExitPlan find failed on close",
			"err", err, "positionID", oe.ClosedPositionID,
		)
		return
	}
	if plan == nil {
		// シャドウ運用初期は楽天 API 既存建玉に対して plan が無いケースあり
		h.logger.Info("shadow ExitPlan not found on close (orphan close)",
			"positionID", oe.ClosedPositionID,
		)
		return
	}
	if plan.IsClosed() {
		// 二重 close ガード（shadow なので warn どまり）
		h.logger.Warn("shadow ExitPlan already closed",
			"positionID", oe.ClosedPositionID, "planID", plan.ID,
		)
		return
	}
	if err := h.repo.Close(ctx, plan.ID, oe.Timestamp); err != nil {
		h.logger.Warn("shadow ExitPlan close persist failed",
			"err", err, "planID", plan.ID,
		)
		return
	}
	h.logger.Info("shadow ExitPlan closed",
		"positionID", oe.ClosedPositionID, "planID", plan.ID,
		"closePrice", oe.Price,
	)
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd backend && go test ./internal/usecase/exitplan/ -count=1 -race -v`
Expected: 全テスト PASS

- [ ] **Step 5: コミット**

```bash
git add backend/internal/usecase/exitplan/shadow_handler.go backend/internal/usecase/exitplan/shadow_handler_test.go
git commit -m "feat(exit-plan): shadow handler that mirrors OrderEvents to ExitPlan repo"
```

---

### Task 6: EventDrivenPipeline に ShadowHandler を register

**Files:**
- Modify: `backend/cmd/event_pipeline.go`

**Notes:**
- 既存のイベント登録ブロック（priority 50 で `riskHandler` が EventTypeOrder を listen している箇所）の **直後** に shadow handler を追加する。
- priority は 60 に設定（risk handler の OrderEvent 処理が先に終わってからシャドウが走るのを保証）。
- シャドウは emit しないので副作用なし、既存パイプラインの挙動は不変。
- `*sql.DB` は `p.db` で参照できる前提（既存 pipeline が DB を保持している）。`p` が DB を直接保持していない場合、wireup のために `pipeline` 構造体や Builder に DB を渡す必要がある。

実装前に以下を確認すること:

```bash
grep -n "p\.db\|pipeline.*db\|sqlite\|migrations" backend/cmd/event_pipeline.go | head -10
```

DB 参照経路が pipeline に渡っていない場合、本タスクは「DB を pipeline に注入する追加の wiring」も含む。実装者が読むこと:

- `backend/cmd/main.go` で `database.RunMigrations(db)` を呼んでいるはず → そこから pipeline に渡す経路を辿る
- すでに `decision_log_repo.NewDecisionLogRepository(db)` を pipeline に渡しているなら、それと同じパターンで `NewExitPlanRepository(db)` を渡せる

- [ ] **Step 1: 既存の DB / Repository 経路を確認**

```bash
cd backend && grep -rn "NewDecisionLogRepository\|decisionLogRepo\|sqlite.OpenDB" cmd/ internal/usecase/ | head -10
```

DecisionLogRepository が pipeline / pipelineConfig にどう渡されているか把握する。

- [ ] **Step 2: pipeline config に ExitPlanRepo を追加**

`backend/cmd/event_pipeline.go` の pipeline 構造体（または Config 構造体）に以下フィールドを追加:

```go
// ExitPlanRepo はシャドウで ExitPlan を永続化する。Phase 1 ではシャドウ
// 専用、Phase 2 で tick handler の取り回しに昇格する。
exitPlanRepo repository.ExitPlanRepository
```

import 追加（既存に無ければ）:
```go
"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/exitplan"
```

- [ ] **Step 3: main.go で wire を追加**

`backend/cmd/main.go` のパイプライン構築箇所で、既存の `NewDecisionLogRepository(db)` と並んで:

```go
exitPlanRepo := database.NewExitPlanRepository(db)
```

を作り、pipeline に渡す（既存の Builder/Option パターンに従う）。

- [ ] **Step 4: shadow handler を bus に register**

`backend/cmd/event_pipeline.go` の既存 `bus.Register(entity.EventTypeOrder, 50, riskHandler)` の直後に追加:

```go
// ExitPlan shadow (priority 60): OrderEvent をシャドウで listen して
// ExitPlan の作成・close だけ行う。発注パスには干渉しない。Phase 2 で
// 出口判定本体を Exit レイヤに移管したらこの shadow は退役する。
if p.exitPlanRepo != nil {
	shadow := exitplan.NewShadowHandler(exitplan.ShadowHandlerConfig{
		Repo:   p.exitPlanRepo,
		Policy: snap.riskPolicy,
	})
	bus.Register(entity.EventTypeOrder, 60, shadow)
	slog.Info("event-pipeline: ExitPlan shadow handler registered (Phase 1)")
}
```

- [ ] **Step 5: ビルドが通ることを確認**

Run: `cd backend && go build ./... && go vet ./...`
Expected: エラーなし

- [ ] **Step 6: 既存テストが全て緑であることを確認**

Run: `cd backend && go test ./... -race -count=1`
Expected: 全テスト PASS（ExitPlan を導入したことで既存挙動は変わらないため）

- [ ] **Step 7: コミット**

```bash
git add backend/cmd/event_pipeline.go backend/cmd/main.go
git commit -m "feat(exit-plan): wire ShadowHandler into EventDrivenPipeline (Phase 1)"
```

---

### Task 7: 観察用 SQL ヘルスチェックドキュメント

**Files:**
- Create: `docs/exit-plan-health-check.md`

**Notes:**
- 設計書 §10 Phase 1 の検証項目「ExitPlan の DB 書き込みが楽天 API 建玉と整合しているか観察」に対応。
- `docs/decision-log-health-check.md` と同じ思想で SQL を書く。

- [ ] **Step 1: ヘルスチェック SQL 集を書く**

```markdown
# ExitPlan Phase 1 シャドウ運用ヘルスチェック

> Phase 1: ShadowHandler は約定イベントを listen して `exit_plans` テーブルに
> 書き込むだけ。発注パスへは干渉しない。本ドキュメントの SQL を 1 日 1 回程度
> 流して、楽天 API 建玉と DB ExitPlan の整合性が取れているか観察する。

## 1. open ExitPlan の一覧

```sql
SELECT id, position_id, symbol_id, side, entry_price,
       sl_percent, sl_atr_multiplier, tp_percent,
       trailing_mode, trailing_atr_multiplier,
       trailing_activated, trailing_hwm,
       datetime(created_at/1000, 'unixepoch', 'localtime') AS created
FROM exit_plans
WHERE closed_at IS NULL
ORDER BY created_at DESC;
```

楽天サイトで現在保有している建玉数と件数が一致するか。

## 2. 直近 24h の close 履歴

```sql
SELECT id, position_id, symbol_id, side, entry_price,
       trailing_activated, trailing_hwm,
       datetime(created_at/1000, 'unixepoch', 'localtime') AS opened,
       datetime(closed_at/1000,  'unixepoch', 'localtime') AS closed
FROM exit_plans
WHERE closed_at IS NOT NULL
  AND closed_at > (strftime('%s', 'now') - 86400) * 1000
ORDER BY closed_at DESC;
```

## 3. 同 position_id で複数 plan が作られていないか（UNIQUE 違反検知）

```sql
SELECT position_id, COUNT(*) AS n
FROM exit_plans
GROUP BY position_id
HAVING n > 1;
```

DB 制約で 1:1 を強制しているので空が期待値。

## 4. 楽天 API 建玉に対応する ExitPlan が存在しない孤児

bot ログで `shadow ExitPlan not found on close (orphan close)` を grep:

```bash
docker compose logs backend --since 24h | grep "orphan close" | wc -l
```

シャドウ運用初期は **bot 起動前から保有していた建玉** に対して plan が無いまま
close されるとここがカウントされる。完全に 0 にはならない（既存建玉ぶん）。
新規約定 → close のサイクルに対しては 0 が期待値。

## 5. 一定時間経った open plan が closed されているか（漏れ検知）

```sql
-- 24h 以上 open のままの plan
SELECT id, position_id, symbol_id, side, entry_price,
       datetime(created_at/1000, 'unixepoch', 'localtime') AS opened,
       (strftime('%s', 'now') - created_at/1000) / 3600 AS hours_open
FROM exit_plans
WHERE closed_at IS NULL
  AND created_at < (strftime('%s', 'now') - 86400) * 1000
ORDER BY created_at;
```

長時間 open は楽天 API 上では既に close されている可能性。Phase 2 の
Reconciler が無いので Phase 1 では手動確認。
```

- [ ] **Step 2: コミット**

```bash
git add docs/exit-plan-health-check.md
git commit -m "docs(exit-plan): Phase 1 shadow run health-check SQL"
```

---

### Task 8: 全体検証 + PR 作成

- [ ] **Step 1: 全テスト実行（race + count=1）**

Run: `cd backend && go test ./... -race -count=1`
Expected: 全テスト PASS

- [ ] **Step 2: vet**

Run: `cd backend && go vet ./...`
Expected: エラーなし

- [ ] **Step 3: ビルド確認**

Run: `cd backend && go build ./...`
Expected: エラーなし

- [ ] **Step 4: docker compose で実起動確認**

Run: `docker compose up --build -d && sleep 30 && docker compose logs backend --tail 50`
Expected: 起動エラーなし、`ExitPlan shadow handler registered` ログが見える

- [ ] **Step 5: ブランチ push と PR 作成**

```bash
git push -u origin feat/exit-plan-phase1-shadow

gh pr create --title "feat(exit-plan): Phase 1 — ExitPlan ドメイン + Repository + シャドウ運用" \
  --base docs/exit-plan-first-class \
  --body "$(cat <<'EOF'
## Summary

設計書 [docs/superpowers/specs/2026-05-04-exit-plan-first-class-design.md](../blob/docs/exit-plan-first-class/docs/superpowers/specs/2026-05-04-exit-plan-first-class-design.md) Phase 1 の実装。

- ExitPlan ドメインエンティティ + 不変条件（`internal/domain/entity/exit_plan.go`）
- ExitPlanRepository インタフェース + SQLite 実装
- `exit_plans` テーブルマイグレーション（position_id UNIQUE）
- OrderEvent をシャドウで listen する ShadowHandler（既存挙動には影響なし）
- 観察用 SQL ヘルスチェック（`docs/exit-plan-health-check.md`）

Phase 1 は **シャドウ運用**。SL/TP/Trailing 発火判定や HWM 更新はまだ既存
RiskManager に任せている。Phase 2 でこれらを ExitHandler に移管する。

## Test plan

- [x] `go test ./... -race -count=1` 全緑
- [x] `go vet ./...` クリーン
- [x] `docker compose up --build -d` 起動成功
- [ ] production 環境で 1 日シャドウ運用後、`docs/exit-plan-health-check.md` の
      SQL で楽天 API 建玉と ExitPlan の整合性を確認

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 6: CI 待機 → グリーンになったらマージ**

```bash
gh pr checks --watch
gh pr merge --squash --auto --delete-branch
```

---

## Self-Review

**Spec coverage:**
- 設計書 §5 ExitPlan ドメインモデル: Task 1 で実装、Phase 2 用の `CurrentSLPrice` 等は明示的に Phase 2 へ deferral
- §5.4 Repository インタフェース: Task 2 で全メソッド定義
- §6.4 約定時フロー（ExitPlanCreated, ExitPlanClosed）: Task 5 ShadowHandler でシャドウ実装
- §10 Phase 1 検証: Task 7 でヘルスチェック SQL を提供

Phase 1 のスコープ範囲外（Phase 2/3 で実装）:
- §6.2 Tick 処理フロー、§5.2 動的価格計算
- §6.5 既存 RiskManager 廃止
- §7 UI 拡張
- §8 整合性ガード（リトライ + Halt + Reconciler）

**Placeholder scan:** 全コード例は完成形を提示済み。`...` や TBD は無い。

**Type consistency:**
- `entity.OrderSide` (BUY/SELL) は全タスクで統一
- `risk.RiskPolicy` の `StopLoss / TakeProfit / Trailing` フィールド名は `policy.go` のものと一致
- `repository.ExitPlanRepository` のメソッドシグネチャは Task 2 と Task 4 / Task 5 で一致
- `entity.OrderEvent` の `OpenedPositionID / ClosedPositionID / SymbolID / Side / Price / Timestamp` は既存 entity と一致

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-04-exit-plan-phase1-shadow.md`. Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks
2. **Inline Execution** — current session, batch with checkpoints

Which approach?
