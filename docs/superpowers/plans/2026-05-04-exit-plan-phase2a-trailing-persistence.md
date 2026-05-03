# ExitPlan Phase 2a: 動的計算と Trailing 永続化（TickRiskHandler は据え置き）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ExitPlan に SL/TP/Trailing の動的計算メソッドを追加し、tick handler から HWM 永続化を駆動する。既存 `TickRiskHandler` の発火経路には触らず、ExitPlan が「観察される正しい状態」を持つようにすることで、Phase 2b の発火経路移管 / Phase 3 の UI 拡張の土台を作る。

**Architecture:**

設計書 §10 の Phase 2 を 2 段に分割した前半。Phase 2 全体は「TickRiskHandler を Exit レイヤに置き換える」だが、それは ExitPlan の動的計算と HWM 永続化が先に揃ってからの方が安全。本 plan で以下を満たす:

1. `ExitPlan.CurrentSLPrice(currentATR)` / `CurrentTPPrice()` / `CurrentTrailingTriggerPrice(currentATR)` を実装
2. ATR を保持・更新する `exitplan.ATRSource` を追加（live は IndicatorEvent から、test は直接注入）
3. TickEvent を listen する `TrailingPersistenceHandler` を追加し、HWM 引き上げを DB write
4. `TickRiskHandler` の HWM 計算と並行運用（同じ tick を見て同じ HWM を計算するが、永続化先が違う）
5. ヘルスチェック SQL に「TickRiskHandler の HWM と DB の HWM が一致しているか」を追加

**Tech Stack:** Go 1.25, SQLite, EventEngine（既存）, Phase 1 で導入した `domain/exitplan` + `usecase/exitplan`

**関連設計書:** `docs/superpowers/specs/2026-05-04-exit-plan-first-class-design.md`

**ブランチ戦略:** `feat/exit-plan-phase2a-trailing-persistence` を `main` から切る。Phase 2b は本 PR マージ後に別 PR で。

---

## File Structure

新規作成:
- `backend/internal/domain/exitplan/pricing.go` — SL/TP/Trailing 価格計算（ATR モード/Percent モード）
- `backend/internal/domain/exitplan/pricing_test.go` — table-driven test
- `backend/internal/usecase/exitplan/atr_source.go` — IndicatorEvent から ATR を吸い上げる side-handler
- `backend/internal/usecase/exitplan/atr_source_test.go`
- `backend/internal/usecase/exitplan/trailing_handler.go` — TickEvent で ExitPlan の HWM を駆動
- `backend/internal/usecase/exitplan/trailing_handler_test.go`

修正:
- `backend/cmd/event_pipeline.go` — TrailingPersistenceHandler と ATRSource を EventBus に register
- `docs/exit-plan-health-check.md` — HWM 整合性 SQL を追加

---

### Task 1: ExitPlan に動的価格計算を追加

**Files:**
- Create: `backend/internal/domain/exitplan/pricing.go`
- Create: `backend/internal/domain/exitplan/pricing_test.go`

**Notes:**
- 計算ロジックは既存 `backtest.TickRiskHandler` の `stopLossDistance` / `trailingDistance` と完全互換にする（バックテスト整合性のため）
- `max(percent_distance, atr_distance)` の保守的方針も踏襲

- [ ] **Step 1: 失敗するテストを書く**

```go
// backend/internal/domain/exitplan/pricing_test.go
package exitplan

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

func TestExitPlan_CurrentSLPrice_long(t *testing.T) {
	cases := []struct {
		name       string
		policy     risk.RiskPolicy
		atr        float64
		entry      float64
		wantSL     float64
	}{
		{
			"percent only — long",
			risk.RiskPolicy{
				StopLoss:   risk.StopLossSpec{Percent: 1.5},
				TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
				Trailing:   risk.TrailingSpec{Mode: risk.TrailingModePercent},
			},
			0,
			10000,
			9850, // 10000 - 150
		},
		{
			"ATR mode, ATR wins — long",
			risk.RiskPolicy{
				StopLoss:   risk.StopLossSpec{Percent: 1.0, ATRMultiplier: 2.0},
				TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
				Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
			},
			100, // ATR distance = 200, percent distance = 100, ATR wins
			10000,
			9800, // 10000 - 200
		},
		{
			"ATR mode but ATR=0 fallback to percent — long",
			risk.RiskPolicy{
				StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
				TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
				Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
			},
			0,
			10000,
			9850, // percent fallback
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := New(NewInput{
				PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: tc.entry,
				Policy: tc.policy, CreatedAt: 1,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got := plan.CurrentSLPrice(tc.atr)
			if got != tc.wantSL {
				t.Errorf("CurrentSLPrice = %v, want %v", got, tc.wantSL)
			}
		})
	}
}

func TestExitPlan_CurrentSLPrice_short(t *testing.T) {
	plan := mustNewForTest(t, 1, entity.OrderSideSell, 10000)
	// percent fallback (ATR=0): 10000 + 150 = 10150
	if got := plan.CurrentSLPrice(0); got != 10150 {
		t.Errorf("short SL with ATR=0: %v, want 10150", got)
	}
	// ATR active: 100 * 2.0 = 200, percent = 150, ATR wins
	if got := plan.CurrentSLPrice(100); got != 10200 {
		t.Errorf("short SL with ATR=100: %v, want 10200", got)
	}
}

func TestExitPlan_CurrentTPPrice(t *testing.T) {
	long := mustNewForTest(t, 1, entity.OrderSideBuy, 10000)
	if got := long.CurrentTPPrice(); got != 10300 { // 3.0%
		t.Errorf("long TP: %v, want 10300", got)
	}
	short := mustNewForTest(t, 2, entity.OrderSideSell, 10000)
	if got := short.CurrentTPPrice(); got != 9700 {
		t.Errorf("short TP: %v, want 9700", got)
	}
}

func TestExitPlan_CurrentTPPrice_disabled(t *testing.T) {
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5},
		TakeProfit: risk.TakeProfitSpec{Percent: 0}, // ← disabled
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeDisabled},
	}
	// TP=0 はバリデーションで弾かれるはず（risk.RiskPolicy.Validate）
	_, err := New(NewInput{
		PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy: policy, CreatedAt: 1,
	})
	if err == nil {
		t.Errorf("expected validation error for TP=0")
	}
}

func TestExitPlan_CurrentTrailingTriggerPrice(t *testing.T) {
	plan := mustNewForTest(t, 1, entity.OrderSideBuy, 10000)
	// 未活性化なら nil
	if got := plan.CurrentTrailingTriggerPrice(0); got != nil {
		t.Errorf("unactivated should return nil, got %v", *got)
	}
	// 活性化させる: HWM=10250
	plan.RaiseTrailingHWM(10250, 100)
	// percent fallback (ATR=0): distance = 10000 * 1.5/100 = 150
	// trigger = HWM - distance = 10250 - 150 = 10100
	got := plan.CurrentTrailingTriggerPrice(0)
	if got == nil {
		t.Fatal("activated should return non-nil")
	}
	if *got != 10100 {
		t.Errorf("trigger = %v, want 10100", *got)
	}
	// ATR active: distance = max(150, 100*2.5=250) = 250 → trigger = 10250-250 = 10000
	got = plan.CurrentTrailingTriggerPrice(100)
	if *got != 10000 {
		t.Errorf("trigger with ATR: %v, want 10000", *got)
	}
}

func TestExitPlan_CurrentTrailingTriggerPrice_disabled(t *testing.T) {
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeDisabled},
	}
	plan, err := New(NewInput{
		PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy: policy, CreatedAt: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan.RaiseTrailingHWM(10250, 100)
	if got := plan.CurrentTrailingTriggerPrice(0); got != nil {
		t.Errorf("disabled trailing should return nil")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd backend && go test ./internal/domain/exitplan/ -run TestExitPlan_CurrentSL -count=1`
Expected: コンパイルエラー（`CurrentSLPrice` 未定義）

- [ ] **Step 3: 計算ロジックを実装**

```go
// backend/internal/domain/exitplan/pricing.go
package exitplan

import (
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

// CurrentSLPrice は現時点の SL 価格を返す。ATR モードでは currentATR が
// 揺らぐと結果も揺らぐ（仕様：ATR レジーム変化への追従）。
//
// 計算は既存 backtest.TickRiskHandler.stopLossDistance と完全互換:
//   distance = max(entryPrice × percent / 100, currentATR × multiplier)
// ATR モードかつ ATR=0 のときは percent にフォールバックする。
func (e *ExitPlan) CurrentSLPrice(currentATR float64) float64 {
	distance := stopLossDistance(e.Policy.StopLoss, e.EntryPrice, currentATR)
	if e.Side == entity.OrderSideBuy {
		return e.EntryPrice - distance
	}
	return e.EntryPrice + distance
}

// CurrentTPPrice は TP 価格を返す。Policy.TakeProfit.Percent は New で
// > 0 が保証されているので無効ケースの分岐は不要。
func (e *ExitPlan) CurrentTPPrice() float64 {
	distance := e.EntryPrice * e.Policy.TakeProfit.Percent / 100.0
	if e.Side == entity.OrderSideBuy {
		return e.EntryPrice + distance
	}
	return e.EntryPrice - distance
}

// CurrentTrailingTriggerPrice は HWM から SL 距離分戻った価格を返す。
// TrailingActivated == false のときと TrailingMode == Disabled のときは nil。
func (e *ExitPlan) CurrentTrailingTriggerPrice(currentATR float64) *float64 {
	if e.Policy.Trailing.Mode == risk.TrailingModeDisabled {
		return nil
	}
	if !e.TrailingActivated || e.TrailingHWM == nil {
		return nil
	}
	distance := trailingDistance(e.Policy, e.EntryPrice, currentATR)
	if distance <= 0 {
		return nil
	}
	hwm := *e.TrailingHWM
	var trigger float64
	if e.Side == entity.OrderSideBuy {
		trigger = hwm - distance
	} else {
		trigger = hwm + distance
	}
	return &trigger
}

// stopLossDistance は SL 距離。max(percent, ATR) で保守的に取る。
// 既存 backtest.TickRiskHandler.stopLossDistance と完全互換。
func stopLossDistance(spec risk.StopLossSpec, entryPrice, currentATR float64) float64 {
	percentDist := entryPrice * spec.Percent / 100.0
	atrDist := 0.0
	if spec.ATRMultiplier > 0 && currentATR > 0 {
		atrDist = currentATR * spec.ATRMultiplier
	}
	if atrDist > percentDist {
		return atrDist
	}
	return percentDist
}

// trailingDistance は Trailing の reversal 距離。Disabled なら 0。
// 既存 backtest.TickRiskHandler.trailingDistance と完全互換。
func trailingDistance(policy risk.RiskPolicy, entryPrice, currentATR float64) float64 {
	switch policy.Trailing.Mode {
	case risk.TrailingModeDisabled:
		return 0
	case risk.TrailingModeATR:
		percentDist := entryPrice * policy.StopLoss.Percent / 100.0
		atrDist := 0.0
		if policy.Trailing.ATRMultiplier > 0 && currentATR > 0 {
			atrDist = currentATR * policy.Trailing.ATRMultiplier
		}
		if atrDist > percentDist {
			return atrDist
		}
		return percentDist
	default: // TrailingModePercent
		return entryPrice * policy.StopLoss.Percent / 100.0
	}
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd backend && go test ./internal/domain/exitplan/ -count=1 -race -v`
Expected: 全テスト PASS

- [ ] **Step 5: コミット**

```bash
git add backend/internal/domain/exitplan/pricing.go backend/internal/domain/exitplan/pricing_test.go
git commit -m "feat(exit-plan): dynamic SL/TP/Trailing price computation"
```

---

### Task 2: ATRSource handler

**Files:**
- Create: `backend/internal/usecase/exitplan/atr_source.go`
- Create: `backend/internal/usecase/exitplan/atr_source_test.go`

**Notes:**
- IndicatorEvent から ATR を吸い上げて in-memory 保持。Trailing handler が読む。
- 既存 `TickRiskHandler.UpdateATR` と同じ責務（NaN/負値スキップ、0 は受け入れ）

- [ ] **Step 1: 失敗するテストを書く**

```go
// backend/internal/usecase/exitplan/atr_source_test.go
package exitplan

import (
	"context"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestATRSource_consumesIndicatorEvent(t *testing.T) {
	src := NewATRSource()
	atr := 12.5
	ev := entity.IndicatorEvent{
		Primary: entity.IndicatorSet{
			ATR: &atr,
		},
	}
	if _, err := src.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := src.Current(); got != 12.5 {
		t.Errorf("Current = %v, want 12.5", got)
	}
}

func TestATRSource_NaNIgnored(t *testing.T) {
	src := NewATRSource()
	atr := math.NaN()
	ev := entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr}}
	if _, err := src.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := src.Current(); got != 0 {
		t.Errorf("NaN should be ignored, got %v", got)
	}
}

func TestATRSource_NegativeIgnored(t *testing.T) {
	src := NewATRSource()
	atr := -1.0
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr}})
	if got := src.Current(); got != 0 {
		t.Errorf("negative should be ignored, got %v", got)
	}
}

func TestATRSource_ZeroAccepted(t *testing.T) {
	src := NewATRSource()
	atr1 := 10.0
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr1}})
	atr2 := 0.0
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr2}})
	if got := src.Current(); got != 0 {
		t.Errorf("zero should be accepted (replace stale positive), got %v", got)
	}
}

func TestATRSource_NilATR_noOp(t *testing.T) {
	src := NewATRSource()
	atr := 10.0
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr}})
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: nil}})
	if got := src.Current(); got != 10 {
		t.Errorf("nil ATR should not overwrite, got %v", got)
	}
}

func TestATRSource_NonIndicatorEvent_noOp(t *testing.T) {
	src := NewATRSource()
	if _, err := src.Handle(context.Background(), entity.TickEvent{}); err != nil {
		t.Fatalf("non-indicator should not error: %v", err)
	}
	if src.Current() != 0 {
		t.Errorf("Current should remain 0")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd backend && go test ./internal/usecase/exitplan/ -run TestATRSource -count=1`
Expected: コンパイルエラー（`NewATRSource` 未定義）

- [ ] **Step 3: 実装**

```go
// backend/internal/usecase/exitplan/atr_source.go
package exitplan

import (
	"context"
	"math"
	"sync"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ATRSource は IndicatorEvent から ATR を吸い上げて、TrailingHandler が
// 動的計算で参照するための in-memory 共有値を保持する。
//
// 既存 backtest.TickRiskHandler.UpdateATR と同じ受け入れ規則:
//   - NaN は無視
//   - 負値は無視
//   - 0 は受け入れる（ボラ消失からの復帰時に stale positive ATR が残らない）
type ATRSource struct {
	mu  sync.RWMutex
	atr float64
}

// NewATRSource はゼロ初期状態の ATRSource を返す。
func NewATRSource() *ATRSource {
	return &ATRSource{}
}

// Handle implements eventengine.EventHandler. IndicatorEvent 以外は素通り。
func (s *ATRSource) Handle(_ context.Context, ev entity.Event) ([]entity.Event, error) {
	ie, ok := ev.(entity.IndicatorEvent)
	if !ok {
		return nil, nil
	}
	if ie.Primary.ATR == nil {
		return nil, nil
	}
	v := *ie.Primary.ATR
	if math.IsNaN(v) || v < 0 {
		return nil, nil
	}
	s.mu.Lock()
	s.atr = v
	s.mu.Unlock()
	return nil, nil
}

// Current は現在の ATR を返す。スレッドセーフ。
func (s *ATRSource) Current() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.atr
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd backend && go test ./internal/usecase/exitplan/ -run TestATRSource -count=1 -race -v`
Expected: 全テスト PASS

- [ ] **Step 5: コミット**

```bash
git add backend/internal/usecase/exitplan/atr_source.go backend/internal/usecase/exitplan/atr_source_test.go
git commit -m "feat(exit-plan): ATRSource handler for IndicatorEvent intake"
```

---

### Task 3: TrailingPersistenceHandler

**Files:**
- Create: `backend/internal/usecase/exitplan/trailing_handler.go`
- Create: `backend/internal/usecase/exitplan/trailing_handler_test.go`

**Notes:**
- TickEvent を listen
- 各 ExitPlan に対し `RaiseTrailingHWM(price, ts)` を呼ぶ。changed なら `repo.UpdateTrailing()` で永続化
- 失敗は warn ログで握り潰す（Phase 2a はまだシャドウ的扱い、本番発火経路には触らないので）
- Closed plan も含めた全部を毎 tick 走査するのは重いので、`ListOpen` で絞る

- [ ] **Step 1: 失敗するテストを書く**

```go
// backend/internal/usecase/exitplan/trailing_handler_test.go
package exitplan

import (
	"context"
	"sync"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	domainexitplan "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/exitplan"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

func TestTrailingHandler_long_activation(t *testing.T) {
	repo := newTrailingMemRepo()
	plan, _ := domainexitplan.New(domainexitplan.NewInput{
		PositionID: 100, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy: defaultPolicy(),
		CreatedAt: 1,
	})
	plan.ID = 555
	repo.byPosition[100] = plan
	repo.openList = []*domainexitplan.ExitPlan{plan}

	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})

	// 含み益未満は no-op
	_, err := h.Handle(context.Background(), entity.TickEvent{
		SymbolID: 7, Price: 9990, Timestamp: 100,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.updateCalled {
		t.Errorf("loss-side tick should not persist")
	}
	if plan.TrailingActivated {
		t.Errorf("HWM should not activate")
	}

	// 含み益超え → 活性化 + 永続化
	_, err = h.Handle(context.Background(), entity.TickEvent{
		SymbolID: 7, Price: 10050, Timestamp: 200,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !repo.updateCalled {
		t.Errorf("activation should persist")
	}
	if !plan.TrailingActivated || plan.TrailingHWM == nil || *plan.TrailingHWM != 10050 {
		t.Errorf("plan state wrong: %+v", plan)
	}
	if repo.updateHWM != 10050 || !repo.updateActivated {
		t.Errorf("update args wrong: hwm=%v activated=%v", repo.updateHWM, repo.updateActivated)
	}
}

func TestTrailingHandler_long_higherHigh(t *testing.T) {
	repo := newTrailingMemRepo()
	plan, _ := domainexitplan.New(domainexitplan.NewInput{
		PositionID: 100, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy: defaultPolicy(),
		CreatedAt: 1,
	})
	plan.ID = 555
	plan.RaiseTrailingHWM(10050, 100) // pre-activated
	repo.byPosition[100] = plan
	repo.openList = []*domainexitplan.ExitPlan{plan}

	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})

	// より低い tick → no-op
	h.Handle(context.Background(), entity.TickEvent{SymbolID: 7, Price: 10030, Timestamp: 200})
	if repo.updateCalled {
		t.Errorf("lower tick should not persist")
	}
	// 新高値 → 永続化
	h.Handle(context.Background(), entity.TickEvent{SymbolID: 7, Price: 10100, Timestamp: 300})
	if !repo.updateCalled {
		t.Errorf("new high should persist")
	}
	if repo.updateHWM != 10100 {
		t.Errorf("HWM = %v, want 10100", repo.updateHWM)
	}
}

func TestTrailingHandler_otherSymbol_skipped(t *testing.T) {
	repo := newTrailingMemRepo()
	plan, _ := domainexitplan.New(domainexitplan.NewInput{
		PositionID: 100, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy: defaultPolicy(),
		CreatedAt: 1,
	})
	plan.ID = 555
	repo.byPosition[100] = plan
	repo.openList = []*domainexitplan.ExitPlan{plan}

	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})
	h.Handle(context.Background(), entity.TickEvent{SymbolID: 8, Price: 10100, Timestamp: 100})
	if repo.listCalled {
		// repo の ListOpen は呼ばれない（早期 filter）
		// または呼ばれても update は走らない、どちらでも実装で OK
	}
	if repo.updateCalled {
		t.Errorf("different symbol tick should not persist")
	}
}

func TestTrailingHandler_repoErrorSwallowed(t *testing.T) {
	repo := newTrailingMemRepo()
	repo.listErr = errFakeRepo
	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})
	if _, err := h.Handle(context.Background(), entity.TickEvent{
		SymbolID: 7, Price: 10100, Timestamp: 100,
	}); err != nil {
		t.Fatalf("repo error must not propagate, got %v", err)
	}
}

func TestTrailingHandler_NonTickEvent_passThrough(t *testing.T) {
	repo := newTrailingMemRepo()
	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})
	if _, err := h.Handle(context.Background(), entity.IndicatorEvent{}); err != nil {
		t.Fatalf("non-tick: %v", err)
	}
	if repo.listCalled {
		t.Errorf("non-tick should not query repo")
	}
}

// --- helpers ---

var errFakeRepo = errFake{}

type errFake struct{}

func (errFake) Error() string { return "fake repo error" }

type trailingMemRepo struct {
	mu              sync.Mutex
	byPosition      map[int64]*domainexitplan.ExitPlan
	openList        []*domainexitplan.ExitPlan
	listCalled      bool
	updateCalled    bool
	updateID        int64
	updateHWM       float64
	updateActivated bool
	listErr         error
}

func newTrailingMemRepo() *trailingMemRepo {
	return &trailingMemRepo{byPosition: map[int64]*domainexitplan.ExitPlan{}}
}

func (m *trailingMemRepo) Create(_ context.Context, _ *domainexitplan.ExitPlan) error {
	return nil
}
func (m *trailingMemRepo) FindByPositionID(_ context.Context, posID int64) (*domainexitplan.ExitPlan, error) {
	return m.byPosition[posID], nil
}
func (m *trailingMemRepo) ListOpen(_ context.Context, _ int64) ([]*domainexitplan.ExitPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalled = true
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.openList, nil
}
func (m *trailingMemRepo) UpdateTrailing(_ context.Context, planID int64, hwm float64, activated bool, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalled = true
	m.updateID = planID
	m.updateHWM = hwm
	m.updateActivated = activated
	return nil
}
func (m *trailingMemRepo) Close(_ context.Context, _ int64, _ int64) error { return nil }

var _ domainexitplan.Repository = (*trailingMemRepo)(nil)
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd backend && go test ./internal/usecase/exitplan/ -run TestTrailingHandler -count=1`
Expected: コンパイルエラー（`NewTrailingPersistenceHandler` 未定義）

- [ ] **Step 3: 実装**

```go
// backend/internal/usecase/exitplan/trailing_handler.go
package exitplan

import (
	"context"
	"log/slog"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	domainexitplan "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/exitplan"
)

// TrailingPersistenceConfig は TrailingPersistenceHandler のコンストラクタ引数。
type TrailingPersistenceConfig struct {
	Repo   domainexitplan.Repository
	Logger *slog.Logger
}

// TrailingPersistenceHandler は TickEvent を listen して、open ExitPlan の
// HWM を更新する。永続化失敗は warn ログで握り潰す（Phase 2a はまだ
// 既存 TickRiskHandler の発火経路には影響しない）。
//
// Phase 2b で TickRiskHandler が ExitPlan ベースに置き換わったら、本
// handler の HWM 更新が発火判定の唯一のソースになる。
type TrailingPersistenceHandler struct {
	repo   domainexitplan.Repository
	logger *slog.Logger
}

// NewTrailingPersistenceHandler は handler を返す。Repo nil で panic。
func NewTrailingPersistenceHandler(cfg TrailingPersistenceConfig) *TrailingPersistenceHandler {
	if cfg.Repo == nil {
		panic("exitplan.NewTrailingPersistenceHandler: Repo must not be nil")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &TrailingPersistenceHandler{
		repo:   cfg.Repo,
		logger: logger.With("component", "exitplan_trailing"),
	}
}

// Handle implements eventengine.EventHandler.
func (h *TrailingPersistenceHandler) Handle(ctx context.Context, ev entity.Event) ([]entity.Event, error) {
	te, ok := ev.(entity.TickEvent)
	if !ok {
		return nil, nil
	}
	plans, err := h.repo.ListOpen(ctx, te.SymbolID)
	if err != nil {
		h.logger.Warn("ListOpen failed", "err", err, "symbolID", te.SymbolID)
		return nil, nil
	}
	for _, plan := range plans {
		changed := plan.RaiseTrailingHWM(te.Price, te.Timestamp)
		if !changed {
			continue
		}
		hwm := *plan.TrailingHWM
		if err := h.repo.UpdateTrailing(ctx, plan.ID, hwm, plan.TrailingActivated, te.Timestamp); err != nil {
			h.logger.Warn("UpdateTrailing failed",
				"err", err,
				"planID", plan.ID,
				"positionID", plan.PositionID,
				"hwm", hwm,
			)
			continue
		}
		h.logger.Debug("trailing HWM persisted",
			"planID", plan.ID,
			"positionID", plan.PositionID,
			"hwm", hwm,
			"activated", plan.TrailingActivated,
		)
	}
	return nil, nil
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd backend && go test ./internal/usecase/exitplan/ -count=1 -race -v`
Expected: 全テスト PASS

- [ ] **Step 5: コミット**

```bash
git add backend/internal/usecase/exitplan/trailing_handler.go backend/internal/usecase/exitplan/trailing_handler_test.go
git commit -m "feat(exit-plan): TrailingPersistenceHandler drives HWM via TickEvent"
```

---

### Task 4: EventDrivenPipeline に register

**Files:**
- Modify: `backend/cmd/event_pipeline.go`

**Notes:**
- ATRSource を `entity.EventTypeIndicator` priority 26 に register（StrategyHandler 20 と recorder 99 の間、indicatorEventTap 25 の直後）
- TrailingPersistenceHandler を `entity.EventTypeTick` priority 16 に register（TickRiskHandler 15 の直後 = 既存の発火後）

- [ ] **Step 1: register 追加**

`backend/cmd/event_pipeline.go` の既存 shadow handler register（priority 60）の前後どちらかに以下を追加。実装者は本ファイルを開いて、`bus.Register(entity.EventTypeOrder, 60, shadow)` の直後に挿入する:

```go
// ATRSource: ExitPlan の動的価格計算が参照する currentATR を IndicatorEvent
// から吸い上げる side-handler。priority 26 で indicatorEventTap (25) の直後。
if p.exitPlanRepo != nil {
	atrSrc := usecaseexitplan.NewATRSource()
	bus.Register(entity.EventTypeIndicator, 26, atrSrc)

	// Trailing persistence: TickEvent ごとに ExitPlan の HWM を引き上げる。
	// 既存 TickRiskHandler (priority 15) の直後 (16) に置くことで、発火後に
	// 残った plan の HWM を更新する順序になる。Phase 2a では既存発火経路
	// には触れず、永続化された HWM を観察するだけ。
	trailing := usecaseexitplan.NewTrailingPersistenceHandler(usecaseexitplan.TrailingPersistenceConfig{
		Repo: p.exitPlanRepo,
	})
	bus.Register(entity.EventTypeTick, 16, trailing)

	slog.Info("event-pipeline: ExitPlan trailing persistence registered (Phase 2a)")
}
```

実装者注: 既存 shadow handler register が `if p.exitPlanRepo != nil { ... }` の中にあるなら、同じ if ブロック内に追加するほうがコードが綺麗。

- [ ] **Step 2: ビルド確認**

Run: `cd backend && go build ./... && go vet ./...`
Expected: エラーなし

- [ ] **Step 3: 既存テストが緑であることを確認**

Run: `cd backend && go test ./... -race -count=1`
Expected: 全テスト PASS

- [ ] **Step 4: docker compose で実起動確認**

Run: `docker compose up --build -d && sleep 30 && docker compose logs backend --tail 50 | grep -E "ExitPlan (shadow|trailing)"`
Expected: 両 handler の registered ログが見える

- [ ] **Step 5: コミット**

```bash
git add backend/cmd/event_pipeline.go
git commit -m "feat(exit-plan): wire ATRSource and TrailingPersistenceHandler (Phase 2a)"
```

---

### Task 5: ヘルスチェック SQL に HWM 整合性確認を追加

**Files:**
- Modify: `docs/exit-plan-health-check.md`

- [ ] **Step 1: SQL 追加**

`docs/exit-plan-health-check.md` の末尾に以下を追加:

```markdown
## 8. Trailing 活性化の傾向（Phase 2a）

```sql
-- 直近 24h 以内に活性化された ExitPlan
SELECT id, position_id, symbol_id, side, entry_price,
       trailing_hwm,
       datetime(updated_at/1000, 'unixepoch', 'localtime') AS hwm_updated
FROM exit_plans
WHERE trailing_activated = 1
  AND updated_at > (strftime('%s', 'now') - 86400) * 1000
ORDER BY updated_at DESC;
```

```sql
-- 活性化済み plan の HWM が建値からどれだけ伸びているか
SELECT id, position_id, side, entry_price, trailing_hwm,
       CASE
         WHEN side = 'BUY'  THEN (trailing_hwm - entry_price) / entry_price * 100
         WHEN side = 'SELL' THEN (entry_price - trailing_hwm) / entry_price * 100
       END AS profit_pct
FROM exit_plans
WHERE closed_at IS NULL
  AND trailing_activated = 1
ORDER BY profit_pct DESC;
```

## 9. Phase 2a 健全性チェック

- 含み益に乗った建玉に対して `trailing_activated = 1` になっているか（手動検証）
- HWM が単調増加（ロング）/ 単調減少（ショート）になっているか
- bot ログで `UpdateTrailing failed` warn が頻発していないか:
  ```bash
  docker compose logs backend --since 24h | grep "UpdateTrailing failed" | wc -l
  ```
- Phase 2b（既存 TickRiskHandler の置き換え）に進む前に、HWM 永続化が
  数日間安定していることを確認すること。

## 10. Phase 2b へ進む判断基準

Phase 2a を 1 週間運用して以下を満たしたら Phase 2b へ:

- HWM 永続化失敗が 0 件
- 活性化済み plan の HWM 推移が手動 SQL チェックで自然（巻き戻し / 不整合がない）
- Phase 1 のヘルスチェック (1〜7) も継続して緑
```

- [ ] **Step 2: コミット**

```bash
git add docs/exit-plan-health-check.md
git commit -m "docs(exit-plan): Phase 2a HWM persistence health checks"
```

---

### Task 6: 全体検証 + PR 作成

- [ ] **Step 1: 全テスト**

Run: `cd backend && go test ./... -race -count=1`
Expected: 全テスト PASS

- [ ] **Step 2: vet + build**

Run: `cd backend && go vet ./... && go build ./...`
Expected: エラーなし

- [ ] **Step 3: docker compose で実起動**

Run: `docker compose up --build -d && sleep 30 && docker compose logs backend --tail 80 | grep -iE "exitplan|trailing"`
Expected: 起動成功 + handler registered ログ

- [ ] **Step 4: push + PR 作成**

```bash
git push -u origin feat/exit-plan-phase2a-trailing-persistence

gh pr create --title "feat(exit-plan): Phase 2a — 動的価格計算 + Trailing HWM 永続化" \
  --base main \
  --body "$(cat <<'EOF'
## Summary

設計書 [docs/superpowers/specs/2026-05-04-exit-plan-first-class-design.md] Phase 2 を 2 段に分割した前半。

- ExitPlan に CurrentSLPrice / CurrentTPPrice / CurrentTrailingTriggerPrice を実装
- IndicatorEvent から ATR を吸い上げる ATRSource handler
- TickEvent ごとに HWM を永続化する TrailingPersistenceHandler
- 既存 TickRiskHandler の発火経路には触らない（Phase 2b で置き換え予定）

## Test plan

- [x] go test ./... -race -count=1 全緑
- [x] go vet クリーン
- [x] docker compose 起動成功
- [ ] 1 週間運用して HWM 永続化の健全性を確認

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 5: CI 待機 + マージ**

```bash
gh pr checks --watch
gh pr merge --squash --auto --delete-branch
```

---

## Self-Review

**Spec coverage:**
- 設計書 §5.2 動的価格計算: Task 1 で `CurrentSLPrice` / `CurrentTPPrice` / `CurrentTrailingTriggerPrice` 実装
- §6.2 Tick 処理フローの一部（HWM 引き上げ + 永続化）: Task 3 で実装
- §6.5 既存 RiskManager 廃止: 本 plan のスコープ外（Phase 2b へ）
- §10 Phase 2 検証要件: HWM 永続化のみ本 plan で検証、発火経路の整合性確認は Phase 2b で

**Placeholder scan:** 全コードブロックに具体的な実装あり。TBD なし。

**Type consistency:**
- `entity.OrderSide` / `risk.RiskPolicy` / `domainexitplan.ExitPlan` は Phase 1 で確定済みの型を使用
- `eventengine.EventHandler` の `Handle(ctx, ev) ([]entity.Event, error)` シグネチャは既存と一致

---

## Execution Handoff

Phase 1 と同じく Inline Execution で実装する（subagent 分散より作業者本人が一貫して書く方が context window 的に効率的）。
