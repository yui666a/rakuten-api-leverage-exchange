# Plan 5: Strategy Engine + LLM連携 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** テクニカル指標とLLM（Claude API）の判断を統合し、売買シグナルを生成するStrategy Engineを構築する。フェーズ1（戦略立案者）として、LLMは15分ごとに相場状況を分析して戦略方針を返し、Strategy Engineがその方針に基づいてテクニカル指標からエントリー/イグジットシグナルを生成する。

**Architecture:** Strategy Engineはユースケース層に配置。LLM Serviceはインフラ層にClaude APIクライアントとして実装し、インターフェース（`LLMClient`）経由でStrategy Engineに注入する。LLM呼び出しは非同期で15分ごとに実行し、戦略方針をキャッシュ。Strategy Engineはテクニカル指標を受け取るたびに、キャッシュ済みの戦略方針に基づいてシグナル判定を行い、`OrderProposal`を生成してRisk Managerへ渡す。

**Tech Stack:** Go 1.21, `github.com/anthropics/anthropic-sdk-go`, sync, context

---

## ファイル構成

```
backend/
├── config/
│   └── config.go                                    # LLMConfig追加
├── internal/
│   ├── domain/
│   │   └── entity/
│   │       ├── signal.go                           # 売買シグナル
│   │       └── strategy.go                         # 戦略方針
│   ├── usecase/
│   │   ├── strategy.go                             # Strategy Engine
│   │   ├── strategy_test.go
│   │   ├── llm.go                                  # LLM Service (usecase)
│   │   └── llm_test.go
│   └── infrastructure/
│       └── llm/
│           └── claude_client.go                    # Claude APIクライアント
```

---

### Task 1: 戦略方針・シグナルのエンティティ定義

**Files:**
- Create: `backend/internal/domain/entity/signal.go`
- Create: `backend/internal/domain/entity/strategy.go`

- [ ] **Step 1: signal.go を作成**

```go
package entity

// SignalAction は売買シグナルのアクション種別。
type SignalAction string

const (
	SignalActionBuy  SignalAction = "BUY"
	SignalActionSell SignalAction = "SELL"
	SignalActionHold SignalAction = "HOLD"
)

// Signal はStrategy Engineが生成する売買シグナル。
type Signal struct {
	SymbolID  int64        `json:"symbolId"`
	Action    SignalAction `json:"action"`
	Reason    string       `json:"reason"`
	Timestamp int64        `json:"timestamp"`
}
```

- [ ] **Step 2: strategy.go を作成**

```go
package entity

// MarketStance はLLMが判断する相場の戦略方針。
type MarketStance string

const (
	MarketStanceTrendFollow MarketStance = "TREND_FOLLOW"
	MarketStanceContrarian  MarketStance = "CONTRARIAN"
	MarketStanceHold        MarketStance = "HOLD"
)

// StrategyAdvice はLLM Serviceが返す戦略アドバイス。
type StrategyAdvice struct {
	Stance    MarketStance `json:"stance"`
	Reasoning string       `json:"reasoning"`
	UpdatedAt int64        `json:"updatedAt"`
}

// MarketContext はLLMに渡す相場コンテキスト情報。
type MarketContext struct {
	SymbolID   int64        `json:"symbolId"`
	LastPrice  float64      `json:"lastPrice"`
	Indicators IndicatorSet `json:"indicators"`
}
```

- [ ] **Step 3: ビルド確認**

```bash
cd backend && go build ./...
```

- [ ] **Step 4: コミット**

```bash
git add backend/internal/domain/entity/signal.go backend/internal/domain/entity/strategy.go
git commit -m "feat: add Signal and StrategyAdvice entities for strategy engine"
```

---

### Task 2: LLMClient インターフェースと LLM Service

**Files:**
- Create: `backend/internal/usecase/llm.go`
- Create: `backend/internal/usecase/llm_test.go`

- [ ] **Step 1: llm_test.go を書く**

```go
package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// mockLLMClient はテスト用のLLMクライアント。
type mockLLMClient struct {
	response *entity.StrategyAdvice
	err      error
}

func (m *mockLLMClient) AnalyzeMarket(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestLLMService_GetAdvice_ReturnsCachedAdvice(t *testing.T) {
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend detected",
			UpdatedAt: time.Now().Unix(),
		},
	}
	svc := NewLLMService(mock, 15*time.Minute)

	// 初回はキャッシュなし → LLM呼び出し
	marketCtx := entity.MarketContext{
		SymbolID:  7,
		LastPrice: 5000000,
		Indicators: entity.IndicatorSet{
			SymbolID: 7,
		},
	}
	advice, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advice.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW, got %s", advice.Stance)
	}

	// 2回目はキャッシュから返る（mockを変更してもキャッシュが使われる）
	mock.response = &entity.StrategyAdvice{
		Stance:    entity.MarketStanceContrarian,
		Reasoning: "should not be used",
		UpdatedAt: time.Now().Unix(),
	}
	advice2, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advice2.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected cached TREND_FOLLOW, got %s", advice2.Stance)
	}
}

func TestLLMService_GetAdvice_ExpiredCacheRefreshes(t *testing.T) {
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	// TTLを0にして毎回リフレッシュさせる
	svc := NewLLMService(mock, 0)

	marketCtx := entity.MarketContext{SymbolID: 7, LastPrice: 5000000}
	_, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// mockを変更 → TTL=0なのでリフレッシュされる
	mock.response = &entity.StrategyAdvice{
		Stance:    entity.MarketStanceContrarian,
		Reasoning: "reversal",
		UpdatedAt: time.Now().Unix(),
	}

	advice, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advice.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN after refresh, got %s", advice.Stance)
	}
}

func TestLLMService_GetAdvice_FallbackToHoldOnError(t *testing.T) {
	mock := &mockLLMClient{
		err: context.DeadlineExceeded,
	}
	svc := NewLLMService(mock, 15*time.Minute)

	marketCtx := entity.MarketContext{SymbolID: 7, LastPrice: 5000000}
	advice, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("should not return error, got: %v", err)
	}
	if advice.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD fallback on error, got %s", advice.Stance)
	}
}

func TestLLMService_GetAdvice_UseStaleCacheOnError(t *testing.T) {
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	// TTL=0で毎回リフレッシュ
	svc := NewLLMService(mock, 0)

	marketCtx := entity.MarketContext{SymbolID: 7, LastPrice: 5000000}
	_, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// LLMがエラーを返すようにする
	mock.err = context.DeadlineExceeded
	mock.response = nil

	// キャッシュが古くてもフォールバックで返す
	advice, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("should not return error: %v", err)
	}
	if advice.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected stale cache TREND_FOLLOW, got %s", advice.Stance)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend && go test ./internal/usecase/ -v -run TestLLMService
```

Expected: コンパイルエラー（`NewLLMService` が未定義）

- [ ] **Step 3: llm.go を実装**

```go
package usecase

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// LLMClient はLLMプロバイダーへのインターフェース。
type LLMClient interface {
	AnalyzeMarket(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error)
}

// LLMService はLLM呼び出しをキャッシュ付きで管理する。
type LLMService struct {
	client   LLMClient
	cacheTTL time.Duration

	mu     sync.RWMutex
	cached *entity.StrategyAdvice
	cachedAt time.Time
}

func NewLLMService(client LLMClient, cacheTTL time.Duration) *LLMService {
	return &LLMService{
		client:   client,
		cacheTTL: cacheTTL,
	}
}

// GetAdvice はキャッシュされた戦略アドバイスを返す。
// キャッシュが期限切れの場合はLLMに問い合わせて更新する。
// LLMエラー時は古いキャッシュを返すか、キャッシュがなければHOLDを返す。
func (s *LLMService) GetAdvice(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error) {
	s.mu.RLock()
	if s.cached != nil && time.Since(s.cachedAt) < s.cacheTTL {
		cached := s.cached
		s.mu.RUnlock()
		return cached, nil
	}
	stale := s.cached
	s.mu.RUnlock()

	advice, err := s.client.AnalyzeMarket(ctx, marketCtx)
	if err != nil {
		log.Printf("LLM error, using fallback: %v", err)
		if stale != nil {
			return stale, nil
		}
		return &entity.StrategyAdvice{
			Stance:    entity.MarketStanceHold,
			Reasoning: "LLM unavailable, defaulting to HOLD",
			UpdatedAt: time.Now().Unix(),
		}, nil
	}

	s.mu.Lock()
	s.cached = advice
	s.cachedAt = time.Now()
	s.mu.Unlock()

	return advice, nil
}

// GetCachedAdvice はキャッシュされたアドバイスを直接返す（LLM呼び出しなし）。
// キャッシュがなければnilを返す。
func (s *LLMService) GetCachedAdvice() *entity.StrategyAdvice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cached
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend && go test ./internal/usecase/ -v -run TestLLMService
```

Expected: 全テストPASS

- [ ] **Step 5: コミット**

```bash
git add backend/internal/usecase/llm.go backend/internal/usecase/llm_test.go
git commit -m "feat: add LLM Service with cache and fallback logic"
```

---

### Task 3: Strategy Engine（テクニカル指標 → シグナル生成）

**Files:**
- Create: `backend/internal/usecase/strategy.go`
- Create: `backend/internal/usecase/strategy_test.go`

- [ ] **Step 1: strategy_test.go を書く**

```go
package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func ptr(f float64) *float64 { return &f }

func TestStrategyEngine_TrendFollow_BuySignal(t *testing.T) {
	// TREND_FOLLOW: SMA20 > SMA50 かつ RSI < 70 → BUY
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	llmSvc := NewLLMService(mock, 15*time.Minute)
	engine := NewStrategyEngine(llmSvc)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000), // SMA20 > SMA50 → 上昇トレンド
		SMA50:    ptr(5000000),
		RSI14:    ptr(55),
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
	// TREND_FOLLOW: SMA20 < SMA50 かつ RSI > 30 → SELL
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "downtrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	llmSvc := NewLLMService(mock, 15*time.Minute)
	engine := NewStrategyEngine(llmSvc)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(4900000), // SMA20 < SMA50 → 下降トレンド
		SMA50:    ptr(5000000),
		RSI14:    ptr(45),
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
	// TREND_FOLLOW: SMA20 > SMA50 だがRSI >= 70 → HOLD（買われすぎ）
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	llmSvc := NewLLMService(mock, 15*time.Minute)
	engine := NewStrategyEngine(llmSvc)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(75), // 買われすぎ
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
	// CONTRARIAN: RSI < 30 → BUY（売られすぎ → 反発狙い）
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "oversold bounce expected",
			UpdatedAt: time.Now().Unix(),
		},
	}
	llmSvc := NewLLMService(mock, 15*time.Minute)
	engine := NewStrategyEngine(llmSvc)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(4900000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(25), // 売られすぎ
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY on oversold contrarian, got %s", signal.Action)
	}
}

func TestStrategyEngine_Contrarian_SellOnOverbought(t *testing.T) {
	// CONTRARIAN: RSI > 70 → SELL（買われすぎ → 反落狙い）
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "overbought reversal",
			UpdatedAt: time.Now().Unix(),
		},
	}
	llmSvc := NewLLMService(mock, 15*time.Minute)
	engine := NewStrategyEngine(llmSvc)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(75), // 買われすぎ
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionSell {
		t.Fatalf("expected SELL on overbought contrarian, got %s", signal.Action)
	}
}

func TestStrategyEngine_Contrarian_HoldInNeutral(t *testing.T) {
	// CONTRARIAN: RSI 30-70 → HOLD（中立圏、逆張りシグナルなし）
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "range bound",
			UpdatedAt: time.Now().Unix(),
		},
	}
	llmSvc := NewLLMService(mock, 15*time.Minute)
	engine := NewStrategyEngine(llmSvc)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5000000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(50), // 中立圏
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
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceHold,
			Reasoning: "uncertain market",
			UpdatedAt: time.Now().Unix(),
		},
	}
	llmSvc := NewLLMService(mock, 15*time.Minute)
	engine := NewStrategyEngine(llmSvc)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(55),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD for HOLD stance, got %s", signal.Action)
	}
}

func TestStrategyEngine_InsufficientIndicators_Hold(t *testing.T) {
	// 指標データが不足している場合はHOLD
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	llmSvc := NewLLMService(mock, 15*time.Minute)
	engine := NewStrategyEngine(llmSvc)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		// SMA20, SMA50, RSI14 が全部nil
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5000000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD for insufficient indicators, got %s", signal.Action)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend && go test ./internal/usecase/ -v -run TestStrategyEngine
```

Expected: コンパイルエラー（`NewStrategyEngine` が未定義）

- [ ] **Step 3: strategy.go を実装**

```go
package usecase

import (
	"context"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// StrategyEngine はテクニカル指標とLLMの戦略方針を統合して売買シグナルを生成する。
type StrategyEngine struct {
	llmService *LLMService
}

func NewStrategyEngine(llmService *LLMService) *StrategyEngine {
	return &StrategyEngine{
		llmService: llmService,
	}
}

// Evaluate はテクニカル指標と現在価格から売買シグナルを生成する。
// LLMの戦略方針（TREND_FOLLOW/CONTRARIAN/HOLD）に基づいて判定ロジックを切り替える。
func (e *StrategyEngine) Evaluate(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) (*entity.Signal, error) {
	marketCtx := entity.MarketContext{
		SymbolID:   indicators.SymbolID,
		LastPrice:  lastPrice,
		Indicators: indicators,
	}
	advice, err := e.llmService.GetAdvice(ctx, marketCtx)
	if err != nil {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "LLM error, defaulting to HOLD",
			Timestamp: time.Now().Unix(),
		}, nil
	}

	// 指標データが不足している場合はHOLD
	if indicators.SMA20 == nil || indicators.SMA50 == nil || indicators.RSI14 == nil {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "insufficient indicator data",
			Timestamp: time.Now().Unix(),
		}, nil
	}

	sma20 := *indicators.SMA20
	sma50 := *indicators.SMA50
	rsi := *indicators.RSI14

	switch advice.Stance {
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

// evaluateTrendFollow はトレンドフォロー戦略でシグナルを判定する。
// SMA20 > SMA50 かつ RSI < 70 → BUY
// SMA20 < SMA50 かつ RSI > 30 → SELL
// それ以外 → HOLD
func (e *StrategyEngine) evaluateTrendFollow(symbolID int64, sma20, sma50, rsi float64) *entity.Signal {
	now := time.Now().Unix()

	if sma20 > sma50 && rsi < 70 {
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionBuy,
			Reason:    "trend follow: SMA20 > SMA50, RSI not overbought",
			Timestamp: now,
		}
	}
	if sma20 < sma50 && rsi > 30 {
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionSell,
			Reason:    "trend follow: SMA20 < SMA50, RSI not oversold",
			Timestamp: now,
		}
	}
	return &entity.Signal{
		SymbolID:  symbolID,
		Action:    entity.SignalActionHold,
		Reason:    "trend follow: no clear signal",
		Timestamp: now,
	}
}

// evaluateContrarian は逆張り戦略でシグナルを判定する。
// RSI < 30 → BUY（売られすぎ → 反発狙い）
// RSI > 70 → SELL（買われすぎ → 反落狙い）
// それ以外 → HOLD
func (e *StrategyEngine) evaluateContrarian(symbolID int64, rsi float64) *entity.Signal {
	now := time.Now().Unix()

	if rsi < 30 {
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionBuy,
			Reason:    "contrarian: RSI oversold, expecting bounce",
			Timestamp: now,
		}
	}
	if rsi > 70 {
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionSell,
			Reason:    "contrarian: RSI overbought, expecting pullback",
			Timestamp: now,
		}
	}
	return &entity.Signal{
		SymbolID:  symbolID,
		Action:    entity.SignalActionHold,
		Reason:    "contrarian: RSI in neutral zone",
		Timestamp: now,
	}
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend && go test ./internal/usecase/ -v -run TestStrategyEngine
```

Expected: 全テストPASS

- [ ] **Step 5: 全テストを実行して回帰がないことを確認**

```bash
cd backend && go test ./... -v
```

Expected: 全テストPASS

- [ ] **Step 6: コミット**

```bash
git add backend/internal/usecase/strategy.go backend/internal/usecase/strategy_test.go
git commit -m "feat: add Strategy Engine with trend-follow and contrarian signal generation"
```

---

### Task 4: Claude APIクライアント（LLMClient実装）

**Files:**
- Create: `backend/internal/infrastructure/llm/claude_client.go`
- Modify: `backend/config/config.go`
- Modify: `backend/.env.example`

- [ ] **Step 1: anthropic-sdk-go を追加**

```bash
cd backend && go get github.com/anthropics/anthropic-sdk-go
```

- [ ] **Step 2: config.go に LLMConfig を追加**

`Config` structに追加:

```go
type Config struct {
	Server   ServerConfig
	Rakuten  RakutenConfig
	Database DatabaseConfig
	Risk     RiskConfig
	LLM      LLMConfig
}

type LLMConfig struct {
	APIKey        string
	Model         string
	MaxTokens     int64
	CacheTTLMin   int
}
```

`Load()` に追加:

```go
LLM: LLMConfig{
	APIKey:      getEnv("ANTHROPIC_API_KEY", ""),
	Model:       getEnv("LLM_MODEL", "claude-haiku-3-5-latest"),
	MaxTokens:   int64(getEnvInt("LLM_MAX_TOKENS", 1024)),
	CacheTTLMin: getEnvInt("LLM_CACHE_TTL_MIN", 15),
},
```

`getEnvInt` ヘルパーを追加:

```go
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}
```

- [ ] **Step 3: .env.example に追記**

```
# LLM (Anthropic Claude)
ANTHROPIC_API_KEY=your_anthropic_api_key_here
LLM_MODEL=claude-haiku-3-5-latest
LLM_MAX_TOKENS=1024
LLM_CACHE_TTL_MIN=15
```

- [ ] **Step 4: claude_client.go を実装**

```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ClaudeClient はAnthropic Claude APIを使ったLLMClient実装。
type ClaudeClient struct {
	client    *anthropic.Client
	model     anthropic.Model
	maxTokens int64
}

func NewClaudeClient(apiKey string, model string, maxTokens int64) *ClaudeClient {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	return &ClaudeClient{
		client:    anthropic.NewClient(opts...),
		model:     anthropic.Model(model),
		maxTokens: maxTokens,
	}
}

const systemPrompt = `You are a cryptocurrency trading strategy advisor.
Analyze the given market data and return a JSON object with your strategic recommendation.

Response format (JSON only, no other text):
{
  "stance": "TREND_FOLLOW" | "CONTRARIAN" | "HOLD",
  "reasoning": "Brief explanation of why you chose this stance"
}

Rules:
- TREND_FOLLOW: When there is a clear directional trend (up or down). The system will follow the trend using moving average crossovers.
- CONTRARIAN: When the market appears overextended (overbought/oversold). The system will look for reversal opportunities using RSI extremes.
- HOLD: When the market is unclear, choppy, or too risky to trade.
- Be conservative. When in doubt, choose HOLD.
- Consider volatility: high volatility with no clear direction → HOLD.`

// AnalyzeMarket はClaude APIに相場データを送り、戦略方針を取得する。
func (c *ClaudeClient) AnalyzeMarket(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error) {
	userMsg := buildUserMessage(marketCtx)

	message, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMsg)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("claude API error: %w", err)
	}

	if len(message.Content) == 0 {
		return nil, fmt.Errorf("claude returned empty content")
	}

	responseText := message.Content[0].Text

	var parsed struct {
		Stance    string `json:"stance"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(responseText), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse claude response: %w (raw: %s)", err, responseText)
	}

	stance := entity.MarketStance(parsed.Stance)
	switch stance {
	case entity.MarketStanceTrendFollow, entity.MarketStanceContrarian, entity.MarketStanceHold:
		// valid
	default:
		stance = entity.MarketStanceHold
	}

	return &entity.StrategyAdvice{
		Stance:    stance,
		Reasoning: parsed.Reasoning,
	}, nil
}

func buildUserMessage(mc entity.MarketContext) string {
	msg := fmt.Sprintf("Symbol ID: %d\nCurrent Price: %.2f\n\nIndicators:\n", mc.SymbolID, mc.LastPrice)

	if mc.Indicators.SMA20 != nil {
		msg += fmt.Sprintf("- SMA20: %.2f\n", *mc.Indicators.SMA20)
	}
	if mc.Indicators.SMA50 != nil {
		msg += fmt.Sprintf("- SMA50: %.2f\n", *mc.Indicators.SMA50)
	}
	if mc.Indicators.EMA12 != nil {
		msg += fmt.Sprintf("- EMA12: %.2f\n", *mc.Indicators.EMA12)
	}
	if mc.Indicators.EMA26 != nil {
		msg += fmt.Sprintf("- EMA26: %.2f\n", *mc.Indicators.EMA26)
	}
	if mc.Indicators.RSI14 != nil {
		msg += fmt.Sprintf("- RSI14: %.2f\n", *mc.Indicators.RSI14)
	}
	if mc.Indicators.MACDLine != nil {
		msg += fmt.Sprintf("- MACD Line: %.6f\n", *mc.Indicators.MACDLine)
	}
	if mc.Indicators.SignalLine != nil {
		msg += fmt.Sprintf("- Signal Line: %.6f\n", *mc.Indicators.SignalLine)
	}
	if mc.Indicators.Histogram != nil {
		msg += fmt.Sprintf("- Histogram: %.6f\n", *mc.Indicators.Histogram)
	}

	msg += "\nWhat is your strategic recommendation?"
	return msg
}
```

- [ ] **Step 5: ビルド確認**

```bash
cd backend && go build ./...
```

- [ ] **Step 6: コミット**

```bash
git add backend/internal/infrastructure/llm/claude_client.go backend/config/config.go backend/.env.example backend/go.mod backend/go.sum
git commit -m "feat: add Claude API client for LLM strategy analysis"
```

---

### Task 5: 全テスト実行 & 設計書更新

**Files:**
- Modify: `docs/design/2026-04-02-auto-trading-system-design.md`

- [ ] **Step 1: 全テスト実行**

```bash
cd backend && go test ./... -v
```

Expected: 全テストPASS

- [ ] **Step 2: 設計書の実装進捗を更新**

`docs/design/2026-04-02-auto-trading-system-design.md` の実装進捗テーブルのPlan 5行を更新:

```markdown
| Plan 5 | Strategy Engine + LLM連携 | #TBD | merged | `usecase/strategy.go`, `usecase/llm.go`, `infrastructure/llm/` |
```

（PR番号はPR作成後に更新する）

- [ ] **Step 3: コミット**

```bash
git add docs/design/2026-04-02-auto-trading-system-design.md
git commit -m "docs: update implementation progress for Plan 5"
```
