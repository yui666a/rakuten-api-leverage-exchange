# Breakout + Volume Strategy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** BB スクイーズ後のブレイクアウトを出来高で確認する新 Stance `BREAKOUT` と、全 Stance 共通の低出来高フィルターを追加する。

**Architecture:** (1) Volume インジケーター計算関数を追加、(2) IndicatorSet に Volume + RecentSqueeze フィールドを追加、(3) StanceResolver に BREAKOUT 判定を追加（`lastPrice` パラメータ追加）、(4) StrategyEngine に evaluateBreakout + 低出来高フィルターを追加、(5) API handler + バックテストを対応。各タスクは TDD で進める。

**Tech Stack:** Go 1.25, existing indicator/entity/usecase packages

**Design Doc:** `docs/design/2026-04-15-breakout-volume-strategy-design.md`

---

## File Structure

### 新規ファイル
- `backend/internal/infrastructure/indicator/volume.go` — VolumeSMA / VolumeRatio 計算関数
- `backend/internal/infrastructure/indicator/volume_test.go` — テスト

### 変更ファイル
- `backend/internal/domain/entity/indicator.go` — VolumeSMA20, VolumeRatio, RecentSqueeze フィールド追加
- `backend/internal/domain/entity/strategy.go` — MarketStanceBreakout 定数追加
- `backend/internal/usecase/indicator.go` — Volume + RecentSqueeze 計算追加
- `backend/internal/usecase/stance.go` — StanceResolver インターフェース + applyRules に BREAKOUT 追加
- `backend/internal/usecase/stance_test.go` — BREAKOUT テスト + 既存テストのシグネチャ更新
- `backend/internal/usecase/strategy.go` — evaluateBreakout, 低出来高フィルター, BB スクイーズフィルター削除, MTF 例外
- `backend/internal/usecase/strategy_test.go` — mockStanceResolver 更新 + BREAKOUT テスト + フィルターテスト
- `backend/internal/interfaces/api/handler/strategy.go` — Resolve に lastPrice 追加, BREAKOUT バリデーション
- `backend/internal/interfaces/api/handler/handler_test.go` — BREAKOUT バリデーションテスト
- `backend/internal/usecase/backtest/handler.go` — calculateIndicatorSet に Volume + RecentSqueeze 追加

---

## Task 1: Volume インジケーター計算関数

**Files:**
- Create: `backend/internal/infrastructure/indicator/volume.go`
- Create: `backend/internal/infrastructure/indicator/volume_test.go`

- [ ] **Step 1: volume_test.go にテスト作成**

```go
// backend/internal/infrastructure/indicator/volume_test.go
package indicator

import (
	"math"
	"testing"
)

func TestVolumeSMA_InsufficientData(t *testing.T) {
	result := VolumeSMA([]float64{100, 200}, 20)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for insufficient data, got %f", result)
	}
}

func TestVolumeSMA_ExactPeriod(t *testing.T) {
	volumes := make([]float64, 20)
	for i := range volumes {
		volumes[i] = 100.0
	}
	result := VolumeSMA(volumes, 20)
	if result != 100.0 {
		t.Fatalf("expected 100.0, got %f", result)
	}
}

func TestVolumeSMA_UsesLastNPeriod(t *testing.T) {
	// 30 candles, last 20 are all 200, first 10 are all 100
	volumes := make([]float64, 30)
	for i := range volumes {
		if i < 10 {
			volumes[i] = 100.0
		} else {
			volumes[i] = 200.0
		}
	}
	result := VolumeSMA(volumes, 20)
	if result != 200.0 {
		t.Fatalf("expected 200.0, got %f", result)
	}
}

func TestVolumeRatio_Normal(t *testing.T) {
	// Current volume = 300, SMA = 100 → ratio = 3.0
	result := VolumeRatio(300, 100)
	if result != 3.0 {
		t.Fatalf("expected 3.0, got %f", result)
	}
}

func TestVolumeRatio_ZeroSMA(t *testing.T) {
	result := VolumeRatio(300, 0)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for zero SMA, got %f", result)
	}
}

func TestVolumeRatio_ZeroVolume(t *testing.T) {
	result := VolumeRatio(0, 100)
	if result != 0.0 {
		t.Fatalf("expected 0.0, got %f", result)
	}
}
```

- [ ] **Step 2: テストが FAIL することを確認**

Run: `cd backend && go test ./internal/infrastructure/indicator/ -run "TestVolumeSMA|TestVolumeRatio" -v`
Expected: compilation error（VolumeSMA / VolumeRatio が未定義）

- [ ] **Step 3: volume.go を実装**

```go
// backend/internal/infrastructure/indicator/volume.go
package indicator

import "math"

// VolumeSMA computes the simple moving average of volume over the last `period` candles.
// Returns NaN if len(volumes) < period.
func VolumeSMA(volumes []float64, period int) float64 {
	if len(volumes) < period {
		return math.NaN()
	}
	sum := 0.0
	for _, v := range volumes[len(volumes)-period:] {
		sum += v
	}
	return sum / float64(period)
}

// VolumeRatio computes currentVolume / sma.
// Returns NaN if sma is zero.
func VolumeRatio(currentVolume, sma float64) float64 {
	if sma == 0 {
		return math.NaN()
	}
	return currentVolume / sma
}
```

- [ ] **Step 4: テスト PASS を確認**

Run: `cd backend && go test ./internal/infrastructure/indicator/ -run "TestVolumeSMA|TestVolumeRatio" -v`
Expected: ALL PASS

- [ ] **Step 5: コミット**

```bash
git add backend/internal/infrastructure/indicator/volume.go backend/internal/infrastructure/indicator/volume_test.go
git commit -m "feat(indicator): add VolumeSMA and VolumeRatio calculation functions"
```

---

## Task 2: IndicatorSet + MarketStance エンティティ拡張

**Files:**
- Modify: `backend/internal/domain/entity/indicator.go`
- Modify: `backend/internal/domain/entity/strategy.go`

- [ ] **Step 1: IndicatorSet にフィールド追加**

`backend/internal/domain/entity/indicator.go` の `ATR14` フィールドの後に追加:

```go
	VolumeSMA20   *float64 `json:"volumeSma20"`   // 出来高20期間SMA
	VolumeRatio   *float64 `json:"volumeRatio"`   // 最新出来高 / VolumeSMA20
	RecentSqueeze *bool    `json:"recentSqueeze"` // 直近5本以内に BBBandwidth < 0.02
```

- [ ] **Step 2: MarketStanceBreakout 定数追加**

`backend/internal/domain/entity/strategy.go` の `MarketStanceHold` の後に追加:

```go
	MarketStanceBreakout MarketStance = "BREAKOUT"
```

- [ ] **Step 3: ビルド確認**

Run: `cd backend && go build ./...`
Expected: エラーなし

- [ ] **Step 4: コミット**

```bash
git add backend/internal/domain/entity/indicator.go backend/internal/domain/entity/strategy.go
git commit -m "feat(entity): add Volume/RecentSqueeze fields and BREAKOUT stance"
```

---

## Task 3: IndicatorCalculator に Volume + RecentSqueeze 計算を追加

**Files:**
- Modify: `backend/internal/usecase/indicator.go`
- Modify: `backend/internal/usecase/backtest/handler.go`

- [ ] **Step 1: usecase/indicator.go に Volume 計算を追加**

`backend/internal/usecase/indicator.go` の `Calculate` 関数内、`result.ATR14 = ...` の後に追加:

```go
	// Volume indicators
	volumes := make([]float64, n)
	for i, cd := range candles {
		volumes[n-1-i] = cd.Volume
	}
	volSMA := indicator.VolumeSMA(volumes, 20)
	result.VolumeSMA20 = toPtr(volSMA)
	if !math.IsNaN(volSMA) && volSMA > 0 && n > 0 {
		vr := indicator.VolumeRatio(volumes[n-1], volSMA)
		result.VolumeRatio = toPtr(vr)
	}

	// RecentSqueeze: check if any of the last 5 candles had BBBandwidth < 0.02
	if n >= 20 {
		recentSqueeze := false
		lookback := 5
		if lookback > n-19 {
			lookback = n - 19
		}
		for i := 0; i < lookback; i++ {
			offset := n - 1 - i
			windowPrices := prices[:offset+1]
			_, _, _, bw := indicator.BollingerBands(windowPrices, 20, 2.0)
			if !math.IsNaN(bw) && bw < 0.02 {
				recentSqueeze = true
				break
			}
		}
		result.RecentSqueeze = &recentSqueeze
	}
```

- [ ] **Step 2: backtest/handler.go の calculateIndicatorSet にも同様に追加**

`backend/internal/usecase/backtest/handler.go` の `calculateIndicatorSet` 関数内、`result.ATR14 = ...` の後に追加:

```go
	// Volume indicators
	volumes := make([]float64, n)
	for i, c := range candles {
		volumes[i] = c.Volume
	}
	volSMA := indicator.VolumeSMA(volumes, 20)
	result.VolumeSMA20 = floatToPtr(volSMA)
	if !math.IsNaN(volSMA) && volSMA > 0 && n > 0 {
		vr := indicator.VolumeRatio(volumes[n-1], volSMA)
		result.VolumeRatio = floatToPtr(vr)
	}

	// RecentSqueeze: check if any of the last 5 candles had BBBandwidth < 0.02
	if n >= 20 {
		recentSqueeze := false
		lookback := 5
		if lookback > n-19 {
			lookback = n - 19
		}
		for i := 0; i < lookback; i++ {
			offset := n - 1 - i
			windowCloses := closes[:offset+1]
			_, _, _, bw := indicator.BollingerBands(windowCloses, 20, 2.0)
			if !math.IsNaN(bw) && bw < 0.02 {
				recentSqueeze = true
				break
			}
		}
		result.RecentSqueeze = &recentSqueeze
	}
```

- [ ] **Step 3: ビルド確認**

Run: `cd backend && go build ./...`
Expected: エラーなし

- [ ] **Step 4: 全テスト PASS 確認**

Run: `cd backend && go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 5: コミット**

```bash
git add backend/internal/usecase/indicator.go backend/internal/usecase/backtest/handler.go
git commit -m "feat(indicator): compute Volume and RecentSqueeze in IndicatorCalculator and backtest"
```

---

## Task 4: StanceResolver に BREAKOUT 判定と lastPrice パラメータ追加

**Files:**
- Modify: `backend/internal/usecase/stance.go`
- Modify: `backend/internal/usecase/stance_test.go`

- [ ] **Step 1: stance_test.go に BREAKOUT テストを追加**

`backend/internal/usecase/stance_test.go` の末尾に追加:

```go
func boolPtr(b bool) *bool { return &b }

func TestRuleBasedStanceResolver_Breakout_UpwardWithVolume(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// RecentSqueeze=true, price > BBUpper, VolumeRatio >= 1.5
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(4900000),
		RSI14:         ptr(55.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.015),
		VolumeRatio:   ptr(2.0),
		RecentSqueeze: boolPtr(true),
	}, 5200000)
	if result.Stance != entity.MarketStanceBreakout {
		t.Fatalf("expected BREAKOUT for upward breakout with volume, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_Breakout_DownwardWithVolume(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(5100000),
		RSI14:         ptr(45.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.015),
		VolumeRatio:   ptr(1.8),
		RecentSqueeze: boolPtr(true),
	}, 4800000)
	if result.Stance != entity.MarketStanceBreakout {
		t.Fatalf("expected BREAKOUT for downward breakout with volume, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_Squeeze_NoBreakout_Hold(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// RecentSqueeze=true, but price is between bands → HOLD
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(5000400),
		RSI14:         ptr(50.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.01),
		VolumeRatio:   ptr(2.0),
		RecentSqueeze: boolPtr(true),
	}, 5050000)
	if result.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD for squeeze without breakout, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_Breakout_LowVolume_Hold(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// Price > BBUpper + RecentSqueeze, but VolumeRatio < 1.5 → HOLD
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(5000400),
		RSI14:         ptr(50.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.01),
		VolumeRatio:   ptr(1.0),
		RecentSqueeze: boolPtr(true),
	}, 5200000)
	if result.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD for breakout without volume confirmation, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_Breakout_NoRecentSqueeze_TrendFollow(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// Price > BBUpper + high volume, but RecentSqueeze=false → not a breakout
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5100000),
		SMA50:         ptr(5000000),
		RSI14:         ptr(55.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.05),
		VolumeRatio:   ptr(2.0),
		RecentSqueeze: boolPtr(false),
	}, 5200000)
	if result.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW without recent squeeze, got %s", result.Stance)
	}
}

func TestRuleBasedStanceResolver_RSI_Contrarian_OverridesBreakout(t *testing.T) {
	resolver := NewRuleBasedStanceResolver(nil)
	// RSI < 25 takes priority over breakout conditions
	result := resolver.Resolve(context.Background(), entity.IndicatorSet{
		SMA20:         ptr(5000000),
		SMA50:         ptr(4900000),
		RSI14:         ptr(20.0),
		BBUpper:       ptr(5100000),
		BBLower:       ptr(4900000),
		BBBandwidth:   ptr(0.01),
		VolumeRatio:   ptr(2.0),
		RecentSqueeze: boolPtr(true),
	}, 5200000)
	if result.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN to override BREAKOUT when RSI extreme, got %s", result.Stance)
	}
}
```

- [ ] **Step 2: テストが FAIL することを確認**

Run: `cd backend && go test ./internal/usecase/ -run "TestRuleBasedStanceResolver_Breakout|TestRuleBasedStanceResolver_Squeeze|TestRuleBasedStanceResolver_RSI_Contrarian_Overrides" -v`
Expected: compilation error（`Resolve` のシグネチャが変わっている）

- [ ] **Step 3: StanceResolver インターフェースと RuleBasedStanceResolver を更新**

`backend/internal/usecase/stance.go` を修正:

**インターフェース変更（24-27行）:**

```go
// StanceResolver はマーケットスタンスを解決するインターフェース。
type StanceResolver interface {
	Resolve(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) StanceResult
	ResolveAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult
}
```

**Resolve メソッド変更（93-95行）:**

```go
func (r *RuleBasedStanceResolver) Resolve(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) StanceResult {
	return r.ResolveAt(ctx, indicators, lastPrice, time.Now())
}
```

**ResolveAt メソッド変更（98行）:**

```go
func (r *RuleBasedStanceResolver) ResolveAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult {
```

`applyRules` の呼び出し元を修正（104, 128行）:

```go
	return r.applyRules(indicators, lastPrice, now)
```

**applyRules を更新（131行〜）:**

```go
func (r *RuleBasedStanceResolver) applyRules(indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult {
	// 1. インジケータ不足チェック
	if indicators.SMA20 == nil || indicators.SMA50 == nil || indicators.RSI14 == nil {
		return StanceResult{
			Stance:    entity.MarketStanceHold,
			Reasoning: "insufficient indicator data",
			Source:    "rule-based",
			UpdatedAt: now.Unix(),
		}
	}

	sma20 := *indicators.SMA20
	sma50 := *indicators.SMA50
	rsi := *indicators.RSI14

	// 2. RSI極端値 → CONTRARIAN（最優先）
	if rsi < 25 {
		return StanceResult{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "RSI oversold",
			Source:    "rule-based",
			UpdatedAt: now.Unix(),
		}
	}
	if rsi > 75 {
		return StanceResult{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "RSI overbought",
			Source:    "rule-based",
			UpdatedAt: now.Unix(),
		}
	}

	// 3. RecentSqueeze → BREAKOUT or HOLD
	if indicators.RecentSqueeze != nil && *indicators.RecentSqueeze {
		if indicators.BBUpper != nil && indicators.BBLower != nil && indicators.VolumeRatio != nil {
			volRatio := *indicators.VolumeRatio
			if lastPrice > *indicators.BBUpper && volRatio >= 1.5 {
				return StanceResult{
					Stance:    entity.MarketStanceBreakout,
					Reasoning: "BB breakout upward with volume confirmation",
					Source:    "rule-based",
					UpdatedAt: now.Unix(),
				}
			}
			if lastPrice < *indicators.BBLower && volRatio >= 1.5 {
				return StanceResult{
					Stance:    entity.MarketStanceBreakout,
					Reasoning: "BB breakout downward with volume confirmation",
					Source:    "rule-based",
					UpdatedAt: now.Unix(),
				}
			}
		}
		// スクイーズ中だがブレイクアウト未発生
		return StanceResult{
			Stance:    entity.MarketStanceHold,
			Reasoning: "BB squeeze without breakout",
			Source:    "rule-based",
			UpdatedAt: now.Unix(),
		}
	}

	// 4. SMA収束 → HOLD
	divergence := math.Abs(sma20-sma50) / sma50
	if divergence < 0.001 {
		return StanceResult{
			Stance:    entity.MarketStanceHold,
			Reasoning: "SMA converged",
			Source:    "rule-based",
			UpdatedAt: now.Unix(),
		}
	}

	// 5. それ以外 → TREND_FOLLOW
	reasoning := "SMA uptrend"
	if sma20 < sma50 {
		reasoning = "SMA downtrend"
	}
	return StanceResult{
		Stance:    entity.MarketStanceTrendFollow,
		Reasoning: reasoning,
		Source:    "rule-based",
		UpdatedAt: now.Unix(),
	}
}
```

- [ ] **Step 4: 既存 stance_test.go の Resolve 呼び出しに lastPrice を追加**

既存のすべての `resolver.Resolve(context.Background(), entity.IndicatorSet{...})` 呼び出しに第3引数 `0` を追加する。既存テストは BB フィールドが nil なので BREAKOUT にはならない。

例（各テストすべて同様に変更）:

```go
// Before:
result := resolver.Resolve(context.Background(), entity.IndicatorSet{...})
// After:
result := resolver.Resolve(context.Background(), entity.IndicatorSet{...}, 0)
```

同様に `resolver.ResolveAt(ctx, indicators, now)` → `resolver.ResolveAt(ctx, indicators, 0, now)` に変更。

- [ ] **Step 5: テスト PASS を確認**

Run: `cd backend && go test ./internal/usecase/ -run TestRuleBasedStanceResolver -v`
Expected: ALL PASS（既存 + 新規）

- [ ] **Step 6: コミット**

```bash
git add backend/internal/usecase/stance.go backend/internal/usecase/stance_test.go
git commit -m "feat(stance): add BREAKOUT detection with RecentSqueeze + volume confirmation"
```

---

## Task 5: StrategyEngine に evaluateBreakout + フィルター追加

**Files:**
- Modify: `backend/internal/usecase/strategy.go`
- Modify: `backend/internal/usecase/strategy_test.go`

- [ ] **Step 1: strategy_test.go の mockStanceResolver シグネチャを更新**

`backend/internal/usecase/strategy_test.go` の `mockStanceResolver` を更新:

```go
func (m *mockStanceResolver) Resolve(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) StanceResult {
	return m.result
}

func (m *mockStanceResolver) ResolveAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult {
	return m.result
}
```

- [ ] **Step 2: BREAKOUT テストを追加**

`backend/internal/usecase/strategy_test.go` の末尾に追加:

```go
func TestStrategyEngine_Breakout_BuySignal(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceBreakout,
			Reasoning: "BB breakout upward with volume confirmation",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:    7,
		SMA20:       ptr(5000000),
		SMA50:       ptr(4900000),
		RSI14:       ptr(55.0),
		BBUpper:     ptr(5100000),
		BBMiddle:    ptr(5000000),
		BBLower:     ptr(4900000),
		VolumeRatio: ptr(2.0),
		Histogram:   ptr(5.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5200000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY for upward breakout, got %s", signal.Action)
	}
	if signal.Confidence <= 0 {
		t.Fatal("expected positive confidence")
	}
}

func TestStrategyEngine_Breakout_SellSignal(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceBreakout,
			Reasoning: "BB breakout downward with volume confirmation",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:    7,
		SMA20:       ptr(5000000),
		SMA50:       ptr(5100000),
		RSI14:       ptr(45.0),
		BBUpper:     ptr(5100000),
		BBMiddle:    ptr(5000000),
		BBLower:     ptr(4900000),
		VolumeRatio: ptr(1.8),
		Histogram:   ptr(-5.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4800000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionSell {
		t.Fatalf("expected SELL for downward breakout, got %s", signal.Action)
	}
}

func TestStrategyEngine_Breakout_HoldWhenMACDAgainst(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceBreakout,
			Reasoning: "BB breakout upward",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:    7,
		SMA20:       ptr(5000000),
		SMA50:       ptr(4900000),
		RSI14:       ptr(55.0),
		BBUpper:     ptr(5100000),
		BBMiddle:    ptr(5000000),
		BBLower:     ptr(4900000),
		VolumeRatio: ptr(2.0),
		Histogram:   ptr(-5.0), // MACD against buy
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5200000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when MACD against breakout buy, got %s", signal.Action)
	}
}

func TestStrategyEngine_Breakout_MissingBBData_Hold(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceBreakout,
			Reasoning: "BB breakout",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5000000),
		SMA50:    ptr(4900000),
		RSI14:    ptr(55.0),
		// BB fields nil
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5200000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when BB data missing for breakout, got %s", signal.Action)
	}
}

func TestStrategyEngine_LowVolume_FiltersSignal(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:    7,
		SMA20:       ptr(5100000),
		SMA50:       ptr(5000000),
		RSI14:       ptr(55.0),
		VolumeRatio: ptr(0.2), // Very low volume
	}
	signal, err := engine.EvaluateWithHigherTF(context.Background(), indicators, nil, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD for low volume, got %s", signal.Action)
	}
}

func TestStrategyEngine_LowVolume_NilRatio_NoFilter(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(55.0),
		// VolumeRatio nil → フィルターは適用されない
	}
	signal, err := engine.EvaluateWithHigherTF(context.Background(), indicators, nil, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY when VolumeRatio is nil (no filter), got %s", signal.Action)
	}
}
```

- [ ] **Step 3: テストが FAIL することを確認**

Run: `cd backend && go test ./internal/usecase/ -run "TestStrategyEngine_Breakout|TestStrategyEngine_LowVolume" -v`
Expected: FAIL（evaluateBreakout が未定義、mockStanceResolver のシグネチャ不一致）

- [ ] **Step 4: strategy.go の resolveAt を更新**

`backend/internal/usecase/strategy.go` の `resolveAt` メソッド（145-147行）を変更:

```go
func (e *StrategyEngine) resolveAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult {
	return e.stanceResolver.ResolveAt(ctx, indicators, lastPrice, now)
}
```

- [ ] **Step 5: EvaluateAt の resolveAt 呼び出しを更新**

`EvaluateAt`（108行付近）と `EvaluateWithHigherTFAt`（39行付近）内の `resolveAt` 呼び出しを更新:

```go
// EvaluateAt 内（124行付近）:
result := e.resolveAt(ctx, indicators, lastPrice, now)

// EvaluateWithHigherTFAt 内（44行付近）:
result := e.resolveAt(ctx, indicators, lastPrice, now)
```

- [ ] **Step 6: EvaluateAt に BREAKOUT case を追加**

`EvaluateAt` の switch 文（130行付近）に追加:

```go
	case entity.MarketStanceBreakout:
		return e.evaluateBreakout(indicators.SymbolID, lastPrice, indicators.BBUpper, indicators.BBLower, indicators.BBMiddle, indicators.VolumeRatio, indicators.Histogram, nowUnix), nil
```

- [ ] **Step 7: evaluateBreakout と breakoutConfidence を実装**

`backend/internal/usecase/strategy.go` の `contrarianConfidence` の後に追加:

```go
func (e *StrategyEngine) evaluateBreakout(symbolID int64, lastPrice float64, bbUpper, bbLower, bbMiddle, volumeRatio, histogram *float64, nowUnix int64) *entity.Signal {
	if bbUpper == nil || bbLower == nil || bbMiddle == nil || volumeRatio == nil {
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionHold,
			Reason:    "breakout: insufficient BB/volume data",
			Timestamp: nowUnix,
		}
	}

	if lastPrice > *bbUpper && *volumeRatio >= 1.5 {
		// MACD histogram confirmation
		if histogram != nil && *histogram < 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "breakout: MACD histogram negative, skipping buy",
				Timestamp: nowUnix,
			}
		}
		return &entity.Signal{
			SymbolID:   symbolID,
			Action:     entity.SignalActionBuy,
			Confidence: breakoutConfidence(lastPrice, *bbUpper, *bbMiddle, *volumeRatio, histogram, true),
			Reason:     "breakout: price above BB upper with volume confirmation",
			Timestamp:  nowUnix,
		}
	}

	if lastPrice < *bbLower && *volumeRatio >= 1.5 {
		if histogram != nil && *histogram > 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "breakout: MACD histogram positive, skipping sell",
				Timestamp: nowUnix,
			}
		}
		return &entity.Signal{
			SymbolID:   symbolID,
			Action:     entity.SignalActionSell,
			Confidence: breakoutConfidence(lastPrice, *bbLower, *bbMiddle, *volumeRatio, histogram, false),
			Reason:     "breakout: price below BB lower with volume confirmation",
			Timestamp:  nowUnix,
		}
	}

	return &entity.Signal{
		SymbolID:  symbolID,
		Action:    entity.SignalActionHold,
		Reason:    "breakout: no clear breakout signal",
		Timestamp: nowUnix,
	}
}

// breakoutConfidence computes a 0.0–1.0 confidence score for breakout signals.
// Factors: volume strength (40%), breakout depth (30%), MACD confirmation (30%).
func breakoutConfidence(lastPrice, bandEdge, bbMiddle, volumeRatio float64, histogram *float64, isBuy bool) float64 {
	// Volume strength: (VolumeRatio - 1.0) / 2.0, capped at 1.0
	volStrength := math.Min((volumeRatio-1.0)/2.0, 1.0)
	if volStrength < 0 {
		volStrength = 0
	}

	// Breakout depth: distance from band edge normalized by BBMiddle
	var depth float64
	if bbMiddle > 0 {
		if isBuy {
			depth = (lastPrice - bandEdge) / bbMiddle
		} else {
			depth = (bandEdge - lastPrice) / bbMiddle
		}
	}
	depth = math.Max(0, math.Min(depth*50, 1.0)) // 2% deviation = 1.0

	// MACD confirmation
	macdConfirm := 0.5
	if histogram != nil {
		macdConfirm = math.Min(math.Abs(*histogram)/10, 1.0)
	}

	return volStrength*0.4 + depth*0.3 + macdConfirm*0.3
}
```

- [ ] **Step 8: EvaluateWithHigherTFAt に低出来高フィルター + BB スクイーズフィルター削除 + MTF BREAKOUT 例外を追加**

`backend/internal/usecase/strategy.go` の `EvaluateWithHigherTFAt` を更新:

BB スクイーズフィルター（48-55行付近）を削除し、低出来高フィルターと MTF BREAKOUT 例外を追加:

```go
func (e *StrategyEngine) EvaluateWithHigherTFAt(
	ctx context.Context,
	indicators entity.IndicatorSet,
	higherTF *entity.IndicatorSet,
	lastPrice float64,
	now time.Time,
) (*entity.Signal, error) {
	signal, err := e.EvaluateAt(ctx, indicators, lastPrice, now)
	if err != nil || signal.Action == entity.SignalActionHold {
		return signal, err
	}

	result := e.resolveAt(ctx, indicators, lastPrice, now)

	// Low volume filter: reject all signals when volume is extremely low
	if indicators.VolumeRatio != nil && *indicators.VolumeRatio < 0.3 {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "volume filter: volume ratio too low, signal unreliable",
			Timestamp: signal.Timestamp,
		}, nil
	}

	// BB position can boost/penalize confidence for contrarian
	if result.Stance == entity.MarketStanceContrarian && indicators.BBLower != nil && indicators.BBUpper != nil {
		if signal.Action == entity.SignalActionBuy && lastPrice <= *indicators.BBLower {
			signal.Confidence = math.Min(signal.Confidence+0.1, 1.0)
		} else if signal.Action == entity.SignalActionSell && lastPrice >= *indicators.BBUpper {
			signal.Confidence = math.Min(signal.Confidence+0.1, 1.0)
		}
	}

	if higherTF == nil || higherTF.SMA20 == nil || higherTF.SMA50 == nil {
		return signal, nil
	}

	// Contrarian and Breakout signals are intentionally allowed against higher TF
	if result.Stance == entity.MarketStanceContrarian || result.Stance == entity.MarketStanceBreakout {
		return signal, nil
	}

	higherUptrend := *higherTF.SMA20 > *higherTF.SMA50

	if signal.Action == entity.SignalActionBuy && !higherUptrend {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "MTF filter: higher timeframe downtrend blocks buy",
			Timestamp: signal.Timestamp,
		}, nil
	}
	if signal.Action == entity.SignalActionSell && higherUptrend {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "MTF filter: higher timeframe uptrend blocks sell",
			Timestamp: signal.Timestamp,
		}, nil
	}

	signal.Confidence = math.Min(signal.Confidence+0.1, 1.0)
	return signal, nil
}
```

- [ ] **Step 9: 全テスト PASS を確認**

Run: `cd backend && go test ./internal/usecase/ -v -run "TestStrategy"`
Expected: ALL PASS（既存 + 新規）

- [ ] **Step 10: 全体テスト PASS を確認**

Run: `cd backend && go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 11: コミット**

```bash
git add backend/internal/usecase/strategy.go backend/internal/usecase/strategy_test.go
git commit -m "feat(strategy): add BREAKOUT evaluation, low-volume filter, and MTF breakout exception"
```

---

## Task 6: API handler の更新

**Files:**
- Modify: `backend/internal/interfaces/api/handler/strategy.go`
- Modify: `backend/internal/interfaces/api/handler/handler_test.go`

- [ ] **Step 1: strategy.go の Resolve 呼び出しに lastPrice 追加**

`backend/internal/interfaces/api/handler/strategy.go` の `GetStrategy`（20-24行）を更新:

```go
func (h *StrategyHandler) GetStrategy(c *gin.Context) {
	indicators := entity.IndicatorSet{}
	result := h.stanceResolver.Resolve(c.Request.Context(), indicators, 0)
	c.JSON(http.StatusOK, result)
}
```

- [ ] **Step 2: SetStrategy の BREAKOUT バリデーション追加**

`SetStrategy` のバリデーション（40-41行）を更新:

```go
	if stance != entity.MarketStanceTrendFollow && stance != entity.MarketStanceContrarian && stance != entity.MarketStanceHold && stance != entity.MarketStanceBreakout {
		c.JSON(http.StatusBadRequest, gin.H{"error": "stance must be TREND_FOLLOW, CONTRARIAN, HOLD, or BREAKOUT"})
		return
	}
```

- [ ] **Step 3: handler_test.go のバリデーションテスト更新を確認**

既存のテストで BREAKOUT が invalid として扱われていないか確認。もし `TestSetStrategy_InvalidStance` のようなテストで BREAKOUT をテストしている場合は更新が必要。

Run: `cd backend && go test ./internal/interfaces/api/handler/ -v -run TestStrategy`

- [ ] **Step 4: ビルド + テスト PASS 確認**

Run: `cd backend && go build ./... && go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 5: コミット**

```bash
git add backend/internal/interfaces/api/handler/strategy.go backend/internal/interfaces/api/handler/handler_test.go
git commit -m "feat(api): update strategy handler for BREAKOUT stance and lastPrice parameter"
```

---

## Task 7: 全体テスト + PR 作成

**Files:** なし（テスト実行のみ）

- [ ] **Step 1: Backend テスト**

Run: `cd backend && go test ./... -race -count=1`
Expected: ALL PASS

- [ ] **Step 2: Frontend テスト**

Run: `cd frontend && pnpm test`
Expected: ALL PASS（Frontend に変更なし）

- [ ] **Step 3: Docker ビルド確認**

Run: `docker compose up --build -d && docker compose logs backend --tail=20`
Expected: backend が正常起動

- [ ] **Step 4: コミット → PR**

```bash
git push -u origin improve/breakout-volume-strategy
gh pr create --base main --title "feat(strategy): add BB breakout stance with volume confirmation" --body "$(cat <<'EOF'
## Summary
- 新 Stance `BREAKOUT`: BB スクイーズ後のブレイクアウトを出来高で確認してシグナル生成
- Volume インジケーター追加: VolumeSMA20, VolumeRatio, RecentSqueeze
- 低出来高フィルター: VolumeRatio < 0.3 で全 Stance のシグナルを抑制
- BREAKOUT は MTF フィルターの例外扱い（CONTRARIAN と同様）
- BB スクイーズフィルターの責務を strategy.go → stance.go に移動

## Design Doc
docs/design/2026-04-15-breakout-volume-strategy-design.md

## Test plan
- [ ] `go test ./... -race -count=1` — 全テスト PASS
- [ ] `pnpm test` — Frontend テスト PASS
- [ ] Docker ビルド + 起動確認

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
