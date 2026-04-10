# Claude Code 自律トレーディングシステム 実装計画

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** LLM (Claude API) による Stance 判定をルールベース判定 + Claude Code オーバーライドに置き換え、REST API 経由で Claude Code が自律的に操作できるようにする

**Architecture:** StrategyEngine の LLM 依存を StanceResolver インターフェースに置換。RuleBasedStanceResolver がテクニカル指標ベースの自動判定 + オーバーライド管理を担う。新規 REST API で注文実行・板情報・ティッカー取得を追加。注文 API は冪等性キーで二重発注を防止する。

**Tech Stack:** Go (Gin), SQLite, TDD

**Spec:** `docs/design/2026-04-10-claude-code-autonomous-trading-design.md`

---

## File Structure

### 新規作成

| ファイル | 責務 |
|---------|------|
| `backend/internal/usecase/stance.go` | StanceResolver IF + RuleBasedStanceResolver 実装 |
| `backend/internal/usecase/stance_test.go` | StanceResolver のテスト |
| `backend/internal/domain/repository/stance_override.go` | オーバーライド永続化 IF |
| `backend/internal/domain/repository/client_order.go` | 冪等性キー永続化 IF |
| `backend/internal/infrastructure/database/stance_override_repo.go` | オーバーライド SQLite 実装 |
| `backend/internal/infrastructure/database/client_order_repo.go` | 冪等性キー SQLite 実装 |
| `backend/internal/interfaces/api/handler/order.go` | POST /orders ハンドラー |
| `backend/internal/interfaces/api/handler/orderbook.go` | GET /orderbook ハンドラー |
| `backend/internal/interfaces/api/handler/ticker.go` | GET /ticker ハンドラー |

### 変更

| ファイル | 変更内容 |
|---------|---------|
| `backend/internal/usecase/strategy.go` | LLMService → StanceResolver IF に置換 |
| `backend/internal/usecase/strategy_test.go` | mock を StanceResolver 用に更新 |
| `backend/internal/interfaces/api/handler/strategy.go` | PUT/DELETE 追加、source 追加 |
| `backend/internal/interfaces/api/router.go` | 新規エンドポイント追加 |
| `backend/internal/infrastructure/database/migrations.go` | stance_overrides, client_orders テーブル追加 |
| `backend/cmd/main.go` | LLM 初期化 → RuleBasedStanceResolver に置換 |

### 削除

| ファイル | 理由 |
|---------|------|
| `backend/internal/infrastructure/llm/claude_client.go` | LLM 直接呼び出し不要 |
| `backend/internal/usecase/llm.go` | LLMService 不要 |
| `backend/internal/usecase/llm_test.go` | LLMService テスト不要 |

---

## Task 1: StanceResolver インターフェースと RuleBasedStanceResolver

**Files:**
- Create: `backend/internal/usecase/stance.go`
- Create: `backend/internal/usecase/stance_test.go`
- Create: `backend/internal/domain/repository/stance_override.go`

- [ ] **Step 1: リポジトリ IF を定義**

```go
// backend/internal/domain/repository/stance_override.go
package repository

import (
	"context"
	"time"
)

type StanceOverrideRecord struct {
	Stance    string
	Reasoning string
	SetAt     int64
	TTLSec    int64
}

type StanceOverrideRepository interface {
	Save(ctx context.Context, record StanceOverrideRecord) error
	Load(ctx context.Context) (*StanceOverrideRecord, error)
	Delete(ctx context.Context) error
}
```

- [ ] **Step 2: テストを書く — ルールベース判定**

```go
// backend/internal/usecase/stance_test.go
package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestRuleBasedStanceResolver_RSIOversold_Contrarian(t *testing.T) {
	r := NewRuleBasedStanceResolver(nil)
	indicators := entity.IndicatorSet{
		SMA20: ptr(5000000),
		SMA50: ptr(5000000),
		RSI14: ptr(20.0), // < 25
	}
	result := r.Resolve(context.Background(), indicators)
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN for RSI=20, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_RSIOverbought_Contrarian(t *testing.T) {
	r := NewRuleBasedStanceResolver(nil)
	indicators := entity.IndicatorSet{
		SMA20: ptr(5000000),
		SMA50: ptr(5000000),
		RSI14: ptr(80.0), // > 75
	}
	result := r.Resolve(context.Background(), indicators)
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN for RSI=80, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_SMAConverged_Hold(t *testing.T) {
	r := NewRuleBasedStanceResolver(nil)
	indicators := entity.IndicatorSet{
		SMA20: ptr(5000000),
		SMA50: ptr(5000400), // 乖離率 0.008% < 0.1%
		RSI14: ptr(50.0),
	}
	result := r.Resolve(context.Background(), indicators)
	if result.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD for converged SMA, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_SMAUptrend_TrendFollow(t *testing.T) {
	r := NewRuleBasedStanceResolver(nil)
	indicators := entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000), // 乖離率 2% > 0.1%
		RSI14: ptr(50.0),
	}
	result := r.Resolve(context.Background(), indicators)
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW for uptrend, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_SMADowntrend_TrendFollow(t *testing.T) {
	r := NewRuleBasedStanceResolver(nil)
	indicators := entity.IndicatorSet{
		SMA20: ptr(4900000),
		SMA50: ptr(5000000), // 乖離率 2% > 0.1%
		RSI14: ptr(50.0),
	}
	result := r.Resolve(context.Background(), indicators)
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW for downtrend, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_InsufficientIndicators_Hold(t *testing.T) {
	r := NewRuleBasedStanceResolver(nil)
	indicators := entity.IndicatorSet{} // no data
	result := r.Resolve(context.Background(), indicators)
	if result.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD for insufficient data, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_OverrideTakesPriority(t *testing.T) {
	r := NewRuleBasedStanceResolver(nil)
	r.SetOverride(entity.MarketStanceContrarian, "news-based override", 60*time.Minute)

	indicators := entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	}
	result := r.Resolve(context.Background(), indicators)
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN override, got %s", result.Stance)
	}
	if result.Source != "override" {
		t.Fatalf("expected source=override, got %s", result.Source)
	}
}

func TestRuleBasedStanceResolver_ExpiredOverrideFallsBack(t *testing.T) {
	r := NewRuleBasedStanceResolver(nil)
	r.SetOverride(entity.MarketStanceContrarian, "expired", 0) // TTL=0 → expired immediately

	// wait a tiny bit to ensure expiry
	time.Sleep(time.Millisecond)

	indicators := entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	}
	result := r.Resolve(context.Background(), indicators)
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW after expired override, got %s", result.Stance)
	}
	if result.Source != "rule-based" {
		t.Fatalf("expected source=rule-based, got %s", result.Source)
	}
}

func TestRuleBasedStanceResolver_ClearOverride(t *testing.T) {
	r := NewRuleBasedStanceResolver(nil)
	r.SetOverride(entity.MarketStanceContrarian, "test", 60*time.Minute)
	r.ClearOverride()

	indicators := entity.IndicatorSet{
		SMA20: ptr(5100000),
		SMA50: ptr(5000000),
		RSI14: ptr(50.0),
	}
	result := r.Resolve(context.Background(), indicators)
	if result.Source != "rule-based" {
		t.Fatalf("expected rule-based after clear, got %s", result.Source)
	}
}
```

- [ ] **Step 3: テスト実行 — 失敗を確認**

Run: `cd backend && go test ./internal/usecase/ -run TestRuleBasedStanceResolver -v`
Expected: コンパイルエラー（型が未定義）

- [ ] **Step 4: StanceResolver IF と RuleBasedStanceResolver を実装**

```go
// backend/internal/usecase/stance.go
package usecase

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// StanceResult は方針判定の結果。
type StanceResult struct {
	Stance    entity.MarketStance `json:"stance"`
	Reasoning string              `json:"reasoning"`
	Source    string              `json:"source"` // "override" or "rule-based"
	ExpiresAt *time.Time          `json:"expiresAt,omitempty"`
	UpdatedAt int64               `json:"updatedAt"`
}

// StanceResolver は方針判定のインターフェース。
type StanceResolver interface {
	Resolve(ctx context.Context, indicators entity.IndicatorSet) StanceResult
}

type stanceOverride struct {
	stance    entity.MarketStance
	reasoning string
	setAt     time.Time
	ttl       time.Duration
}

// RuleBasedStanceResolver はルールベースの方針判定 + オーバーライド管理。
type RuleBasedStanceResolver struct {
	mu       sync.RWMutex
	override *stanceOverride
	repo     repository.StanceOverrideRepository
}

const smaConvergenceThreshold = 0.001 // 0.1%

func NewRuleBasedStanceResolver(repo repository.StanceOverrideRepository) *RuleBasedStanceResolver {
	r := &RuleBasedStanceResolver{repo: repo}
	if repo != nil {
		r.restoreOverride()
	}
	return r
}

func (r *RuleBasedStanceResolver) restoreOverride() {
	record, err := r.repo.Load(context.Background())
	if err != nil {
		slog.Warn("failed to load stance override", "error", err)
		return
	}
	if record == nil {
		return
	}
	setAt := time.Unix(record.SetAt, 0)
	ttl := time.Duration(record.TTLSec) * time.Second
	if time.Since(setAt) >= ttl {
		_ = r.repo.Delete(context.Background())
		return
	}
	r.override = &stanceOverride{
		stance:    entity.MarketStance(record.Stance),
		reasoning: record.Reasoning,
		setAt:     setAt,
		ttl:       ttl,
	}
	slog.Info("stance override restored", "stance", record.Stance, "expiresIn", ttl-time.Since(setAt))
}

func (r *RuleBasedStanceResolver) Resolve(ctx context.Context, indicators entity.IndicatorSet) StanceResult {
	r.mu.RLock()
	ov := r.override
	r.mu.RUnlock()

	if ov != nil {
		if time.Since(ov.setAt) < ov.ttl {
			expiresAt := ov.setAt.Add(ov.ttl)
			return StanceResult{
				Stance:    ov.stance,
				Reasoning: ov.reasoning,
				Source:    "override",
				ExpiresAt: &expiresAt,
				UpdatedAt: ov.setAt.Unix(),
			}
		}
		// expired
		r.mu.Lock()
		r.override = nil
		r.mu.Unlock()
		if r.repo != nil {
			_ = r.repo.Delete(ctx)
		}
	}

	return r.resolveByRules(indicators)
}

func (r *RuleBasedStanceResolver) resolveByRules(indicators entity.IndicatorSet) StanceResult {
	now := time.Now().Unix()

	if indicators.SMA20 == nil || indicators.SMA50 == nil || indicators.RSI14 == nil {
		return StanceResult{
			Stance:    entity.MarketStanceHold,
			Reasoning: "insufficient indicator data",
			Source:    "rule-based",
			UpdatedAt: now,
		}
	}

	rsi := *indicators.RSI14
	sma20 := *indicators.SMA20
	sma50 := *indicators.SMA50

	// RSI extreme → CONTRARIAN
	if rsi < 25 || rsi > 75 {
		return StanceResult{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "RSI extreme detected",
			Source:    "rule-based",
			UpdatedAt: now,
		}
	}

	// SMA convergence → HOLD
	divergence := math.Abs(sma20-sma50) / sma50
	if divergence < smaConvergenceThreshold {
		return StanceResult{
			Stance:    entity.MarketStanceHold,
			Reasoning: "SMA20/SMA50 converged, trend unclear",
			Source:    "rule-based",
			UpdatedAt: now,
		}
	}

	// Clear trend → TREND_FOLLOW
	return StanceResult{
		Stance:    entity.MarketStanceTrendFollow,
		Reasoning: "SMA trend detected",
		Source:    "rule-based",
		UpdatedAt: now,
	}
}

// SetOverride はオーバーライドを設定する。
func (r *RuleBasedStanceResolver) SetOverride(stance entity.MarketStance, reasoning string, ttl time.Duration) {
	r.mu.Lock()
	r.override = &stanceOverride{
		stance:    stance,
		reasoning: reasoning,
		setAt:     time.Now(),
		ttl:       ttl,
	}
	r.mu.Unlock()

	if r.repo != nil {
		_ = r.repo.Save(context.Background(), repository.StanceOverrideRecord{
			Stance:    string(stance),
			Reasoning: reasoning,
			SetAt:     time.Now().Unix(),
			TTLSec:    int64(ttl.Seconds()),
		})
	}
}

// ClearOverride はオーバーライドを解除する。
func (r *RuleBasedStanceResolver) ClearOverride() {
	r.mu.Lock()
	r.override = nil
	r.mu.Unlock()

	if r.repo != nil {
		_ = r.repo.Delete(context.Background())
	}
}

// GetOverride は現在のオーバーライドを返す（なければ nil）。
func (r *RuleBasedStanceResolver) GetOverride() *stanceOverride {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.override != nil && time.Since(r.override.setAt) < r.override.ttl {
		return r.override
	}
	return nil
}
```

- [ ] **Step 5: テスト実行 — パスを確認**

Run: `cd backend && go test ./internal/usecase/ -run TestRuleBasedStanceResolver -v`
Expected: 全テスト PASS

- [ ] **Step 6: コミット**

```bash
git add backend/internal/domain/repository/stance_override.go backend/internal/usecase/stance.go backend/internal/usecase/stance_test.go
git commit -m "feat: add StanceResolver interface and RuleBasedStanceResolver"
```

---

## Task 2: StrategyEngine の LLM 依存を StanceResolver に置換

**Files:**
- Modify: `backend/internal/usecase/strategy.go`
- Modify: `backend/internal/usecase/strategy_test.go`

- [ ] **Step 1: strategy_test.go を StanceResolver mock に書き換え**

既存テストの `mockLLMClient` + `NewLLMService` + `NewStrategyEngine(llmSvc)` パターンを `mockStanceResolver` + `NewStrategyEngine(resolver)` に置き換える。

```go
// backend/internal/usecase/strategy_test.go
package usecase

import (
	"context"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type mockStanceResolver struct {
	result StanceResult
}

func (m *mockStanceResolver) Resolve(ctx context.Context, indicators entity.IndicatorSet) StanceResult {
	return m.result
}

func TestStrategyEngine_TrendFollow_BuySignal(t *testing.T) {
	resolver := &mockStanceResolver{result: StanceResult{Stance: entity.MarketStanceTrendFollow}}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(55.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY, got %s", signal.Action)
	}
}

func TestStrategyEngine_TrendFollow_SellSignal(t *testing.T) {
	resolver := &mockStanceResolver{result: StanceResult{Stance: entity.MarketStanceTrendFollow}}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(4900000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(45.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionSell {
		t.Fatalf("expected SELL, got %s", signal.Action)
	}
}

func TestStrategyEngine_TrendFollow_HoldOnOverbought(t *testing.T) {
	resolver := &mockStanceResolver{result: StanceResult{Stance: entity.MarketStanceTrendFollow}}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(75.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %s", signal.Action)
	}
}

func TestStrategyEngine_Contrarian_BuyOnOversold(t *testing.T) {
	resolver := &mockStanceResolver{result: StanceResult{Stance: entity.MarketStanceContrarian}}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(4900000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(25.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY, got %s", signal.Action)
	}
}

func TestStrategyEngine_Contrarian_SellOnOverbought(t *testing.T) {
	resolver := &mockStanceResolver{result: StanceResult{Stance: entity.MarketStanceContrarian}}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(75.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionSell {
		t.Fatalf("expected SELL, got %s", signal.Action)
	}
}

func TestStrategyEngine_Contrarian_HoldInNeutral(t *testing.T) {
	resolver := &mockStanceResolver{result: StanceResult{Stance: entity.MarketStanceContrarian}}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5000000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(50.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5000000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %s", signal.Action)
	}
}

func TestStrategyEngine_HoldStance_AlwaysHold(t *testing.T) {
	resolver := &mockStanceResolver{result: StanceResult{Stance: entity.MarketStanceHold}}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(55.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %s", signal.Action)
	}
}

func TestStrategyEngine_InsufficientIndicators_Hold(t *testing.T) {
	resolver := &mockStanceResolver{result: StanceResult{Stance: entity.MarketStanceTrendFollow}}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{SymbolID: 7}
	signal, err := engine.Evaluate(context.Background(), indicators, 5000000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %s", signal.Action)
	}
}
```

- [ ] **Step 2: strategy.go を StanceResolver 依存に書き換え**

```go
// backend/internal/usecase/strategy.go
package usecase

import (
	"context"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// StrategyEngine はテクニカル指標とStanceResolverの方針を統合して売買シグナルを生成する。
type StrategyEngine struct {
	stanceResolver StanceResolver
}

func NewStrategyEngine(stanceResolver StanceResolver) *StrategyEngine {
	return &StrategyEngine{stanceResolver: stanceResolver}
}

// Evaluate はテクニカル指標と現在価格から売買シグナルを生成する。
func (e *StrategyEngine) Evaluate(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) (*entity.Signal, error) {
	if indicators.SMA20 == nil || indicators.SMA50 == nil || indicators.RSI14 == nil {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "insufficient indicator data",
			Timestamp: time.Now().Unix(),
		}, nil
	}

	result := e.stanceResolver.Resolve(ctx, indicators)

	sma20 := *indicators.SMA20
	sma50 := *indicators.SMA50
	rsi := *indicators.RSI14

	switch result.Stance {
	case entity.MarketStanceTrendFollow:
		return e.evaluateTrendFollow(indicators.SymbolID, sma20, sma50, rsi), nil
	case entity.MarketStanceContrarian:
		return e.evaluateContrarian(indicators.SymbolID, rsi), nil
	default:
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "stance is HOLD",
			Timestamp: time.Now().Unix(),
		}, nil
	}
}

func (e *StrategyEngine) evaluateTrendFollow(symbolID int64, sma20, sma50, rsi float64) *entity.Signal {
	now := time.Now().Unix()
	if sma20 > sma50 && rsi < 70 {
		return &entity.Signal{SymbolID: symbolID, Action: entity.SignalActionBuy, Reason: "trend follow: SMA20 > SMA50, RSI not overbought", Timestamp: now}
	}
	if sma20 < sma50 && rsi > 30 {
		return &entity.Signal{SymbolID: symbolID, Action: entity.SignalActionSell, Reason: "trend follow: SMA20 < SMA50, RSI not oversold", Timestamp: now}
	}
	return &entity.Signal{SymbolID: symbolID, Action: entity.SignalActionHold, Reason: "trend follow: no clear signal", Timestamp: now}
}

func (e *StrategyEngine) evaluateContrarian(symbolID int64, rsi float64) *entity.Signal {
	now := time.Now().Unix()
	if rsi < 30 {
		return &entity.Signal{SymbolID: symbolID, Action: entity.SignalActionBuy, Reason: "contrarian: RSI oversold, expecting bounce", Timestamp: now}
	}
	if rsi > 70 {
		return &entity.Signal{SymbolID: symbolID, Action: entity.SignalActionSell, Reason: "contrarian: RSI overbought, expecting pullback", Timestamp: now}
	}
	return &entity.Signal{SymbolID: symbolID, Action: entity.SignalActionHold, Reason: "contrarian: RSI in neutral zone", Timestamp: now}
}
```

- [ ] **Step 3: テスト実行 — パスを確認**

Run: `cd backend && go test ./internal/usecase/ -run TestStrategyEngine -v`
Expected: 全テスト PASS

- [ ] **Step 4: コミット**

```bash
git add backend/internal/usecase/strategy.go backend/internal/usecase/strategy_test.go
git commit -m "refactor: replace LLMService dependency with StanceResolver interface in StrategyEngine"
```

---

## Task 3: オーバーライド永続化（SQLite）

**Files:**
- Create: `backend/internal/infrastructure/database/stance_override_repo.go`
- Modify: `backend/internal/infrastructure/database/migrations.go`

- [ ] **Step 1: マイグレーションに stance_overrides テーブルを追加**

`backend/internal/infrastructure/database/migrations.go` の `migrations` スライスに以下を追加:

```go
`CREATE TABLE IF NOT EXISTS stance_overrides (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	stance TEXT NOT NULL,
	reasoning TEXT NOT NULL DEFAULT '',
	set_at INTEGER NOT NULL,
	ttl_sec INTEGER NOT NULL,
	CONSTRAINT valid_stance CHECK (stance IN ('TREND_FOLLOW', 'CONTRARIAN', 'HOLD'))
)`,
```

- [ ] **Step 2: StanceOverrideRepo を実装**

```go
// backend/internal/infrastructure/database/stance_override_repo.go
package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

type StanceOverrideRepo struct {
	db *sql.DB
}

func NewStanceOverrideRepo(db *sql.DB) *StanceOverrideRepo {
	return &StanceOverrideRepo{db: db}
}

func (r *StanceOverrideRepo) Save(ctx context.Context, record repository.StanceOverrideRecord) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO stance_overrides (id, stance, reasoning, set_at, ttl_sec) VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET stance = excluded.stance, reasoning = excluded.reasoning, set_at = excluded.set_at, ttl_sec = excluded.ttl_sec`,
		record.Stance, record.Reasoning, record.SetAt, record.TTLSec,
	)
	if err != nil {
		return fmt.Errorf("save stance override: %w", err)
	}
	return nil
}

func (r *StanceOverrideRepo) Load(ctx context.Context) (*repository.StanceOverrideRecord, error) {
	var record repository.StanceOverrideRecord
	err := r.db.QueryRowContext(ctx,
		`SELECT stance, reasoning, set_at, ttl_sec FROM stance_overrides WHERE id = 1`,
	).Scan(&record.Stance, &record.Reasoning, &record.SetAt, &record.TTLSec)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load stance override: %w", err)
	}
	return &record, nil
}

func (r *StanceOverrideRepo) Delete(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM stance_overrides WHERE id = 1`)
	if err != nil {
		return fmt.Errorf("delete stance override: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: ビルド確認**

Run: `cd backend && go build ./...`
Expected: 成功

- [ ] **Step 4: コミット**

```bash
git add backend/internal/infrastructure/database/stance_override_repo.go backend/internal/infrastructure/database/migrations.go
git commit -m "feat: add stance override persistence with SQLite"
```

---

## Task 4: 冪等性キー永続化（SQLite）

**Files:**
- Create: `backend/internal/domain/repository/client_order.go`
- Create: `backend/internal/infrastructure/database/client_order_repo.go`
- Modify: `backend/internal/infrastructure/database/migrations.go`

- [ ] **Step 1: リポジトリ IF を定義**

```go
// backend/internal/domain/repository/client_order.go
package repository

import "context"

type ClientOrderRecord struct {
	ClientOrderID string
	Executed      bool
	OrderID       int64
	CreatedAt     int64
}

type ClientOrderRepository interface {
	Find(ctx context.Context, clientOrderID string) (*ClientOrderRecord, error)
	Save(ctx context.Context, record ClientOrderRecord) error
	DeleteExpired(ctx context.Context, beforeUnix int64) error
}
```

- [ ] **Step 2: マイグレーションに client_orders テーブルを追加**

`backend/internal/infrastructure/database/migrations.go` の `migrations` スライスに以下を追加:

```go
`CREATE TABLE IF NOT EXISTS client_orders (
	client_order_id TEXT PRIMARY KEY,
	executed INTEGER NOT NULL,
	order_id INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
)`,
`CREATE INDEX IF NOT EXISTS idx_client_orders_created
	ON client_orders(created_at)`,
```

- [ ] **Step 3: ClientOrderRepo を実装**

```go
// backend/internal/infrastructure/database/client_order_repo.go
package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

type ClientOrderRepo struct {
	db *sql.DB
}

func NewClientOrderRepo(db *sql.DB) *ClientOrderRepo {
	return &ClientOrderRepo{db: db}
}

func (r *ClientOrderRepo) Find(ctx context.Context, clientOrderID string) (*repository.ClientOrderRecord, error) {
	var record repository.ClientOrderRecord
	err := r.db.QueryRowContext(ctx,
		`SELECT client_order_id, executed, order_id, created_at FROM client_orders WHERE client_order_id = ?`,
		clientOrderID,
	).Scan(&record.ClientOrderID, &record.Executed, &record.OrderID, &record.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find client order: %w", err)
	}
	return &record, nil
}

func (r *ClientOrderRepo) Save(ctx context.Context, record repository.ClientOrderRecord) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO client_orders (client_order_id, executed, order_id, created_at) VALUES (?, ?, ?, ?)`,
		record.ClientOrderID, record.Executed, record.OrderID, record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save client order: %w", err)
	}
	return nil
}

func (r *ClientOrderRepo) DeleteExpired(ctx context.Context, beforeUnix int64) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM client_orders WHERE created_at < ?`, beforeUnix,
	)
	if err != nil {
		return fmt.Errorf("delete expired client orders: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: ビルド確認**

Run: `cd backend && go build ./...`
Expected: 成功

- [ ] **Step 5: コミット**

```bash
git add backend/internal/domain/repository/client_order.go backend/internal/infrastructure/database/client_order_repo.go backend/internal/infrastructure/database/migrations.go
git commit -m "feat: add client order idempotency key persistence"
```

---

## Task 5: Strategy ハンドラー拡張（PUT / DELETE / source 追加）

**Files:**
- Modify: `backend/internal/interfaces/api/handler/strategy.go`

- [ ] **Step 1: StrategyHandler を書き換え**

```go
// backend/internal/interfaces/api/handler/strategy.go
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type StrategyHandler struct {
	stanceResolver *usecase.RuleBasedStanceResolver
}

func NewStrategyHandler(stanceResolver *usecase.RuleBasedStanceResolver) *StrategyHandler {
	return &StrategyHandler{stanceResolver: stanceResolver}
}

func (h *StrategyHandler) GetStrategy(c *gin.Context) {
	indicators := entity.IndicatorSet{} // resolve with empty to get current stance
	result := h.stanceResolver.Resolve(c.Request.Context(), indicators)
	c.JSON(http.StatusOK, result)
}

type setStrategyRequest struct {
	Stance     string `json:"stance" binding:"required"`
	Reasoning  string `json:"reasoning"`
	TTLMinutes int    `json:"ttlMinutes"`
}

func (h *StrategyHandler) SetStrategy(c *gin.Context) {
	var req setStrategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stance := entity.MarketStance(req.Stance)
	if stance != entity.MarketStanceTrendFollow && stance != entity.MarketStanceContrarian && stance != entity.MarketStanceHold {
		c.JSON(http.StatusBadRequest, gin.H{"error": "stance must be TREND_FOLLOW, CONTRARIAN, or HOLD"})
		return
	}

	ttl := req.TTLMinutes
	if ttl <= 0 {
		ttl = 60
	}
	if ttl > 1440 {
		ttl = 1440
	}

	ttlDuration := time.Duration(ttl) * time.Minute
	h.stanceResolver.SetOverride(stance, req.Reasoning, ttlDuration)

	expiresAt := time.Now().Add(ttlDuration)
	c.JSON(http.StatusOK, gin.H{
		"stance":    stance,
		"reasoning": req.Reasoning,
		"source":    "override",
		"expiresAt": expiresAt.Format(time.RFC3339),
	})
}

func (h *StrategyHandler) DeleteOverride(c *gin.Context) {
	h.stanceResolver.ClearOverride()
	c.JSON(http.StatusOK, gin.H{
		"message": "override cleared, using rule-based stance",
	})
}
```

- [ ] **Step 2: ビルド確認**

Run: `cd backend && go build ./...`
Expected: コンパイルエラー（router.go が古い NewStrategyHandler シグネチャを参照）— Task 7 で解消

- [ ] **Step 3: コミット**

```bash
git add backend/internal/interfaces/api/handler/strategy.go
git commit -m "feat: extend strategy handler with PUT/DELETE override endpoints"
```

---

## Task 6: 注文・板情報・ティッカーハンドラー

**Files:**
- Create: `backend/internal/interfaces/api/handler/order.go`
- Create: `backend/internal/interfaces/api/handler/orderbook.go`
- Create: `backend/internal/interfaces/api/handler/ticker.go`

- [ ] **Step 1: 注文ハンドラーを実装**

```go
// backend/internal/interfaces/api/handler/order.go
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type OrderHandler struct {
	orderExecutor   *usecase.OrderExecutor
	clientOrderRepo repository.ClientOrderRepository
}

func NewOrderHandler(orderExecutor *usecase.OrderExecutor, clientOrderRepo repository.ClientOrderRepository) *OrderHandler {
	return &OrderHandler{orderExecutor: orderExecutor, clientOrderRepo: clientOrderRepo}
}

type createOrderRequest struct {
	SymbolID      int64   `json:"symbolId" binding:"required"`
	Side          string  `json:"side" binding:"required"`
	Amount        float64 `json:"amount" binding:"required,gt=0"`
	OrderType     string  `json:"orderType" binding:"required"`
	ClientOrderID string  `json:"clientOrderId" binding:"required"`
}

func (h *OrderHandler) CreateOrder(c *gin.Context) {
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	side := entity.OrderSide(req.Side)
	if side != entity.OrderSideBuy && side != entity.OrderSideSell {
		c.JSON(http.StatusBadRequest, gin.H{"error": "side must be BUY or SELL"})
		return
	}
	if req.OrderType != "MARKET" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only MARKET orders supported"})
		return
	}

	// 冪等性チェック
	existing, err := h.clientOrderRepo.Find(c.Request.Context(), req.ClientOrderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check idempotency"})
		return
	}
	if existing != nil {
		c.JSON(http.StatusOK, gin.H{
			"executed":      existing.Executed,
			"orderId":       existing.OrderID,
			"clientOrderId": existing.ClientOrderID,
			"duplicate":     true,
		})
		return
	}

	action := entity.SignalActionBuy
	if side == entity.OrderSideSell {
		action = entity.SignalActionSell
	}

	signal := entity.Signal{
		SymbolID:  req.SymbolID,
		Action:    action,
		Reason:    "manual order via API",
		Timestamp: time.Now().Unix(),
	}

	result, err := h.orderExecutor.ExecuteSignal(c.Request.Context(), signal, 0, req.Amount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 冪等性キー記録
	_ = h.clientOrderRepo.Save(c.Request.Context(), repository.ClientOrderRecord{
		ClientOrderID: req.ClientOrderID,
		Executed:      result.Executed,
		OrderID:       result.OrderID,
		CreatedAt:     time.Now().Unix(),
	})

	resp := gin.H{
		"executed":      result.Executed,
		"clientOrderId": req.ClientOrderID,
	}
	if result.Executed {
		resp["orderId"] = result.OrderID
		resp["side"] = req.Side
		resp["amount"] = req.Amount
	} else {
		resp["reason"] = result.Reason
	}
	c.JSON(http.StatusOK, resp)
}
```

- [ ] **Step 2: 板情報ハンドラーを実装**

```go
// backend/internal/interfaces/api/handler/orderbook.go
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
)

type OrderbookHandler struct {
	restClient *rakuten.RESTClient
}

func NewOrderbookHandler(restClient *rakuten.RESTClient) *OrderbookHandler {
	return &OrderbookHandler{restClient: restClient}
}

func (h *OrderbookHandler) GetOrderbook(c *gin.Context) {
	symbolID := int64(7)
	if q := c.Query("symbolId"); q != "" {
		if v, err := strconv.ParseInt(q, 10, 64); err == nil {
			symbolID = v
		}
	}

	orderbook, err := h.restClient.GetOrderbook(c.Request.Context(), symbolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, orderbook)
}
```

- [ ] **Step 3: ティッカーハンドラーを実装**

```go
// backend/internal/interfaces/api/handler/ticker.go
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type TickerHandler struct {
	marketDataSvc *usecase.MarketDataService
}

func NewTickerHandler(marketDataSvc *usecase.MarketDataService) *TickerHandler {
	return &TickerHandler{marketDataSvc: marketDataSvc}
}

func (h *TickerHandler) GetTicker(c *gin.Context) {
	symbolID := int64(7)
	if q := c.Query("symbolId"); q != "" {
		if v, err := strconv.ParseInt(q, 10, 64); err == nil {
			symbolID = v
		}
	}

	ticker, err := h.marketDataSvc.GetLatestTicker(c.Request.Context(), symbolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if ticker == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no ticker data available"})
		return
	}
	c.JSON(http.StatusOK, ticker)
}
```

- [ ] **Step 4: ビルド確認**

Run: `cd backend && go build ./...`
Expected: コンパイルエラー（`GetOrderbook` メソッドが未定義の場合）— 楽天 REST Client に既存メソッドがあるか確認し、なければ Task 6.5 で追加

- [ ] **Step 5: コミット**

```bash
git add backend/internal/interfaces/api/handler/order.go backend/internal/interfaces/api/handler/orderbook.go backend/internal/interfaces/api/handler/ticker.go
git commit -m "feat: add order, orderbook, and ticker REST handlers"
```

---

## Task 7: ルーター更新と配線

**Files:**
- Modify: `backend/internal/interfaces/api/router.go`

- [ ] **Step 1: router.go を更新**

Dependencies 構造体を更新し、新しいエンドポイントを追加:

```go
// backend/internal/interfaces/api/router.go
package api

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api/handler"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type PipelineController interface {
	Start()
	Stop()
	Running() bool
}

type Dependencies struct {
	RiskManager         *usecase.RiskManager
	StanceResolver      *usecase.RuleBasedStanceResolver
	IndicatorCalculator *usecase.IndicatorCalculator
	MarketDataService   *usecase.MarketDataService
	RealtimeHub         *usecase.RealtimeHub
	OrderClient         repository.OrderClient
	OrderExecutor       *usecase.OrderExecutor
	Pipeline            PipelineController
	RESTClient          *rakuten.RESTClient
	ClientOrderRepo     repository.ClientOrderRepository
}

func NewRouter(deps Dependencies) *gin.Engine {
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://localhost:33000"},
		AllowMethods:     []string{"GET", "PUT", "POST", "DELETE"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
	}))

	v1 := r.Group("/api/v1")

	statusHandler := handler.NewStatusHandler(deps.RiskManager)
	v1.GET("/status", statusHandler.GetStatus)
	botHandler := handler.NewBotHandler(deps.RiskManager, deps.RealtimeHub, deps.Pipeline)
	v1.POST("/start", botHandler.Start)
	v1.POST("/stop", botHandler.Stop)

	riskHandler := handler.NewRiskHandler(deps.RiskManager, deps.RealtimeHub)
	v1.GET("/config", riskHandler.GetConfig)
	v1.PUT("/config", riskHandler.UpdateConfig)
	v1.GET("/pnl", riskHandler.GetPnL)

	strategyHandler := handler.NewStrategyHandler(deps.StanceResolver)
	v1.GET("/strategy", strategyHandler.GetStrategy)
	v1.PUT("/strategy", strategyHandler.SetStrategy)
	v1.DELETE("/strategy/override", strategyHandler.DeleteOverride)

	indicatorHandler := handler.NewIndicatorHandler(deps.IndicatorCalculator)
	v1.GET("/indicators/:symbol", indicatorHandler.GetIndicators)

	if deps.MarketDataService != nil {
		candleHandler := handler.NewCandleHandler(deps.MarketDataService)
		v1.GET("/candles/:symbol", candleHandler.GetCandles)

		realtimeHandler := handler.NewRealtimeHandler(deps.MarketDataService, deps.RiskManager, deps.RealtimeHub)
		v1.GET("/ws", realtimeHandler.Stream)

		tickerHandler := handler.NewTickerHandler(deps.MarketDataService)
		v1.GET("/ticker", tickerHandler.GetTicker)
	}

	if deps.OrderClient != nil {
		positionHandler := handler.NewPositionHandler(deps.OrderClient)
		v1.GET("/positions", positionHandler.GetPositions)

		tradeHandler := handler.NewTradeHandler(deps.OrderClient)
		v1.GET("/trades", tradeHandler.GetTrades)
	}

	if deps.RESTClient != nil {
		orderbookHandler := handler.NewOrderbookHandler(deps.RESTClient)
		v1.GET("/orderbook", orderbookHandler.GetOrderbook)
	}

	if deps.OrderExecutor != nil && deps.ClientOrderRepo != nil {
		orderHandler := handler.NewOrderHandler(deps.OrderExecutor, deps.ClientOrderRepo)
		v1.POST("/orders", orderHandler.CreateOrder)
	}

	return r
}
```

- [ ] **Step 2: ビルド確認**

Run: `cd backend && go build ./...`
Expected: コンパイルエラー（cmd/main.go が古い依存を参照）— Task 8 で解消

- [ ] **Step 3: コミット**

```bash
git add backend/internal/interfaces/api/router.go
git commit -m "feat: add new API routes for strategy override, orders, orderbook, ticker"
```

---

## Task 8: main.go の配線更新 + LLM 削除

**Files:**
- Modify: `backend/cmd/main.go`
- Delete: `backend/internal/usecase/llm.go`
- Delete: `backend/internal/usecase/llm_test.go`
- Delete: `backend/internal/infrastructure/llm/claude_client.go`

- [ ] **Step 1: cmd/main.go の初期化を更新**

LLM 関連の import と初期化を削除し、`RuleBasedStanceResolver` に置き換える。主な変更箇所:

- import から `llm` パッケージを削除
- `claudeClient` と `llmSvc` の初期化を削除
- `stanceOverrideRepo` と `clientOrderRepo` を追加
- `RuleBasedStanceResolver` を生成
- `NewStrategyEngine(stanceResolver)` に変更
- `NewRouter` の Dependencies を更新（`LLMService` → `StanceResolver`, `OrderExecutor`, `RESTClient`, `ClientOrderRepo` を追加）

`config.Load()` から LLM 設定の参照も削除する（config.go に LLM フィールドが残っていても未使用になるだけでビルドは通る）。

- [ ] **Step 2: LLM 関連ファイルを削除**

```bash
rm backend/internal/usecase/llm.go
rm backend/internal/usecase/llm_test.go
rm backend/internal/infrastructure/llm/claude_client.go
```

- [ ] **Step 3: ビルド確認**

Run: `cd backend && go build ./...`
Expected: 成功

- [ ] **Step 4: 全テスト実行**

Run: `cd backend && go test ./... -v`
Expected: 全テスト PASS

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: replace LLM with RuleBasedStanceResolver, add new API endpoints"
```

---

## Task 9: 楽天 REST Client に GetOrderbook メソッド追加（必要な場合）

**Files:**
- Modify: `backend/internal/infrastructure/rakuten/public_api.go` (or `rest_client.go`)

- [ ] **Step 1: GetOrderbook メソッドが既存か確認**

Run: `cd backend && grep -r "GetOrderbook\|Orderbook" internal/infrastructure/rakuten/`

既に存在すればこの Task はスキップ。存在しなければ、楽天 API の `GET /v1/cfd/orderbook` を呼ぶメソッドを `RESTClient` に追加する。

- [ ] **Step 2: 必要であれば実装**

```go
func (c *RESTClient) GetOrderbook(ctx context.Context, symbolID int64) (*entity.Orderbook, error) {
	return c.publicAPI.GetOrderbook(ctx, symbolID)
}
```

PublicAPI 側にも対応メソッドが必要な場合は追加する。

- [ ] **Step 3: ビルド確認**

Run: `cd backend && go build ./...`
Expected: 成功

- [ ] **Step 4: コミット（変更があった場合のみ）**

```bash
git add backend/internal/infrastructure/rakuten/
git commit -m "feat: add GetOrderbook to REST client"
```

---

## Task 10: Docker 再ビルドと統合テスト

**Files:** なし（手動確認）

- [ ] **Step 1: Docker 再ビルド**

```bash
docker compose up -d --build backend
```

- [ ] **Step 2: ヘルスチェック**

```bash
curl -s localhost:38080/api/v1/status | python3 -m json.tool
```
Expected: `{"status": "running", ...}`

- [ ] **Step 3: ルールベース判定を確認**

```bash
curl -s 'localhost:38080/api/v1/strategy' | python3 -m json.tool
```
Expected: `source` が `"rule-based"` のレスポンス

- [ ] **Step 4: オーバーライドを設定**

```bash
curl -s -X PUT 'localhost:38080/api/v1/strategy' -H 'Content-Type: application/json' -d '{"stance":"TREND_FOLLOW","reasoning":"test override","ttlMinutes":5}' | python3 -m json.tool
```
Expected: `source: "override"`, `expiresAt` が5分後

- [ ] **Step 5: オーバーライド確認**

```bash
curl -s 'localhost:38080/api/v1/strategy' | python3 -m json.tool
```
Expected: `source: "override"`, stance が `TREND_FOLLOW`

- [ ] **Step 6: オーバーライド解除**

```bash
curl -s -X DELETE 'localhost:38080/api/v1/strategy/override' | python3 -m json.tool
```
Expected: `message: "override cleared, using rule-based stance"`

- [ ] **Step 7: ティッカー取得**

```bash
curl -s 'localhost:38080/api/v1/ticker?symbolId=7' | python3 -m json.tool
```
Expected: ティッカーデータ

- [ ] **Step 8: 板情報取得**

```bash
curl -s 'localhost:38080/api/v1/orderbook?symbolId=7' | python3 -m json.tool
```
Expected: 板情報データ

- [ ] **Step 9: Bot 起動して自動売買確認**

```bash
curl -s -X POST 'localhost:38080/api/v1/start' | python3 -m json.tool
```

```bash
docker logs rakuten-api-leverage-exchange-backend-1 2>&1 | grep -vE "^\[GIN\]" | tail -10
```
Expected: `pipeline: signal evaluated` ログに LLM エラーがなく、ルールベースの Stance で動作

- [ ] **Step 10: コミット（最終）**

```bash
git add -A
git commit -m "chore: verify integration with Docker"
```
