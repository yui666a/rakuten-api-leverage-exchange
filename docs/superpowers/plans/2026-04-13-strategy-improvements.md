# Trading Strategy Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve the auto-trading bot's win rate by adding MACD confirmation, take-profit exits, trailing stop-loss, and a consecutive-loss circuit breaker — each as a separate PR on its own branch.

**Architecture:** Each improvement is an isolated, incremental enhancement to the existing strategy/risk/pipeline layers. No new services or external dependencies. Each PR is independently deployable and reversible.

**Tech Stack:** Go 1.22+, existing indicator/entity/usecase/pipeline packages

---

## Branch / PR Strategy

Each task below produces **one branch → one PR**. Branch off `main` each time.

| # | Branch | Summary |
|---|--------|---------|
| 1 | `improve/macd-confirmation` | MACD ヒストグラムによるシグナル確認フィルター |
| 2 | `improve/take-profit` | 利確ロジック（リスクリワード 1:2） |
| 3 | `improve/trailing-stop` | トレーリングストップ |
| 4 | `improve/consecutive-loss-breaker` | 連敗ブレーカー（N連敗で冷却期間） |

---

## Task 1: MACD Confirmation Filter

**Branch:** `improve/macd-confirmation`

**Problem:** 現在 MACD/EMA を計算しているが売買判断に使っていない。SMA 交差だけでは偽シグナルが多い。

**Solution:** TREND_FOLLOW/CONTRARIAN のシグナル生成時に MACD ヒストグラムの方向を確認条件として追加する。

**Files:**
- Modify: `backend/internal/usecase/strategy.go` — Evaluate / evaluateTrendFollow / evaluateContrarian に MACD 確認を追加
- Modify: `backend/internal/usecase/strategy_test.go` — 新しいテストケース追加
- Modify: `backend/internal/domain/entity/indicator.go` — (変更なし、既に MACDLine/SignalLine/Histogram あり)

### Steps

- [ ] **Step 1: strategy_test.go にテスト追加 — MACD が逆方向なら HOLD**

`backend/internal/usecase/strategy_test.go` に以下を追加:

```go
func TestStrategyEngine_TrendFollow_HoldWhenMACDAgainst(t *testing.T) {
	// SMA20 > SMA50 (uptrend) but MACD histogram is negative → HOLD
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
		SymbolID:  7,
		SMA20:     ptr(5100000),
		SMA50:     ptr(5000000),
		RSI14:     ptr(55.0),
		Histogram: ptr(-5.0), // MACD histogram negative = against buy
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when MACD histogram against trend, got %s", signal.Action)
	}
}

func TestStrategyEngine_TrendFollow_SellBlockedByPositiveHistogram(t *testing.T) {
	// SMA20 < SMA50 (downtrend) but MACD histogram is positive → HOLD
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "downtrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(4900000),
		SMA50:     ptr(5000000),
		RSI14:     ptr(45.0),
		Histogram: ptr(3.0), // positive histogram blocks sell
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when MACD histogram against sell, got %s", signal.Action)
	}
}

func TestStrategyEngine_TrendFollow_BuyWithMACDConfirmation(t *testing.T) {
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
		SymbolID:  7,
		SMA20:     ptr(5100000),
		SMA50:     ptr(5000000),
		RSI14:     ptr(55.0),
		Histogram: ptr(10.0), // positive histogram confirms buy
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY with MACD confirmation, got %s", signal.Action)
	}
}

func TestStrategyEngine_Contrarian_HoldWhenMACDAgainst(t *testing.T) {
	// RSI < 30 (oversold) but MACD histogram still strongly negative → HOLD
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "RSI oversold",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(4900000),
		SMA50:     ptr(5000000),
		RSI14:     ptr(25.0),
		Histogram: ptr(-20.0), // strong negative momentum = don't buy the dip
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when MACD strongly against contrarian buy, got %s", signal.Action)
	}
}

func TestStrategyEngine_TrendFollow_NilHistogramStillTrades(t *testing.T) {
	// Histogram が nil(データ不足) の場合は従来通り SMA+RSI だけで判断
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
		// Histogram is nil
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY when histogram nil (fallback), got %s", signal.Action)
	}
}
```

- [ ] **Step 2: テストが FAIL することを確認**

Run: `cd backend && go test ./internal/usecase/ -run "TestStrategyEngine_TrendFollow_HoldWhenMACDAgainst|TestStrategyEngine_TrendFollow_SellBlockedByPositiveHistogram|TestStrategyEngine_TrendFollow_BuyWithMACDConfirmation|TestStrategyEngine_Contrarian_HoldWhenMACDAgainst|TestStrategyEngine_TrendFollow_NilHistogramStillTrades" -v`

Expected: 2 test failures (`HoldWhenMACDAgainst`, `SellBlockedByPositiveHistogram`, `Contrarian_HoldWhenMACDAgainst`) — 現在の実装は MACD を無視して BUY/SELL を返すため。

- [ ] **Step 3: strategy.go の evaluateTrendFollow を修正**

`backend/internal/usecase/strategy.go` の `evaluateTrendFollow` を修正:

```go
func (e *StrategyEngine) evaluateTrendFollow(symbolID int64, sma20, sma50, rsi float64, histogram *float64) *entity.Signal {
	now := time.Now().Unix()

	if sma20 > sma50 && rsi < 70 {
		// MACD ヒストグラムが負なら見送り（momentum が逆方向）
		if histogram != nil && *histogram < 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "trend follow: MACD histogram negative, skipping buy",
				Timestamp: now,
			}
		}
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionBuy,
			Reason:    "trend follow: SMA20 > SMA50, RSI not overbought, MACD confirmed",
			Timestamp: now,
		}
	}
	if sma20 < sma50 && rsi > 30 {
		// MACD ヒストグラムが正なら見送り（momentum が逆方向）
		if histogram != nil && *histogram > 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "trend follow: MACD histogram positive, skipping sell",
				Timestamp: now,
			}
		}
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionSell,
			Reason:    "trend follow: SMA20 < SMA50, RSI not oversold, MACD confirmed",
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
```

- [ ] **Step 4: evaluateContrarian を修正**

```go
func (e *StrategyEngine) evaluateContrarian(symbolID int64, rsi float64, histogram *float64) *entity.Signal {
	now := time.Now().Unix()

	if rsi < 30 {
		// ヒストグラムがまだ強く下落中なら、底を打っていないのでスキップ
		if histogram != nil && *histogram < -10 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "contrarian: RSI oversold but MACD momentum still strongly negative",
				Timestamp: now,
			}
		}
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionBuy,
			Reason:    "contrarian: RSI oversold, MACD momentum easing",
			Timestamp: now,
		}
	}
	if rsi > 70 {
		if histogram != nil && *histogram > 10 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "contrarian: RSI overbought but MACD momentum still strongly positive",
				Timestamp: now,
			}
		}
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionSell,
			Reason:    "contrarian: RSI overbought, MACD momentum easing",
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

- [ ] **Step 5: Evaluate メソッドのシグネチャ更新**

`Evaluate` 内の呼び出しを更新し、`indicators.Histogram` を渡す:

```go
switch result.Stance {
case entity.MarketStanceTrendFollow:
	return e.evaluateTrendFollow(indicators.SymbolID, sma20, sma50, rsi, indicators.Histogram), nil
case entity.MarketStanceContrarian:
	return e.evaluateContrarian(indicators.SymbolID, rsi, indicators.Histogram), nil
default:
	// ...
}
```

- [ ] **Step 6: 全テスト PASS を確認**

Run: `cd backend && go test ./internal/usecase/ -v -run TestStrategy`
Expected: ALL PASS（既存テストは Histogram=nil で従来動作を維持）

- [ ] **Step 7: 全体テスト PASS を確認**

Run: `cd backend && go test ./...`
Expected: ALL PASS

- [ ] **Step 8: コミット → PR**

```bash
git checkout main && git pull
git checkout -b improve/macd-confirmation
git add backend/internal/usecase/strategy.go backend/internal/usecase/strategy_test.go
git commit -m "feat(strategy): add MACD histogram confirmation filter

TREND_FOLLOW: skip buy when histogram < 0, skip sell when histogram > 0
CONTRARIAN: skip entry when MACD momentum is strongly against direction
Nil histogram (insufficient data) falls back to previous SMA+RSI logic"
git push -u origin improve/macd-confirmation
gh pr create --base main --title "feat(strategy): add MACD histogram confirmation filter" --body "$(cat <<'EOF'
## Summary
- TREND_FOLLOW: MACD ヒストグラムがトレンド方向と逆の場合はエントリーをスキップ
- CONTRARIAN: MACD モメンタムが強く逆方向の場合はエントリーをスキップ
- Histogram が nil（データ不足）の場合は従来の SMA+RSI ロジックにフォールバック

## Why
現状 MACD/EMA を計算しているが売買判断に使っていない。SMA 交差だけでは偽シグナルが多く、毎回負けている原因の一つ。

## Test plan
- [ ] `go test ./internal/usecase/ -run TestStrategy -v` — 全テスト PASS
- [ ] `go test ./...` — 既存テスト regression なし

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Task 2: Take-Profit Logic

**Branch:** `improve/take-profit`

**Problem:** ストップロス（-5%）しかなく利確ロジックがない。含み益が出てもいずれ損切りで終わる。

**Solution:** RiskManager に `CheckTakeProfit` を追加し、ストップロスモニタと同じ経路で利確を実行する。デフォルト利確ラインはストップロスの2倍（10%）。

**Files:**
- Modify: `backend/internal/domain/entity/risk.go` — `TakeProfitPercent` フィールド追加
- Modify: `backend/config/config.go` — `TakeProfitPercent` 環境変数読み込み
- Modify: `backend/internal/usecase/risk.go` — `CheckTakeProfit` メソッド追加
- Modify: `backend/internal/usecase/risk_test.go` — テスト追加
- Modify: `backend/cmd/pipeline.go` — ストップロスモニタに利確チェック追加

### Steps

- [ ] **Step 1: entity/risk.go に TakeProfitPercent 追加**

```go
type RiskConfig struct {
	MaxPositionAmount float64 `json:"maxPositionAmount"`
	MaxDailyLoss      float64 `json:"maxDailyLoss"`
	StopLossPercent   float64 `json:"stopLossPercent"`
	TakeProfitPercent float64 `json:"takeProfitPercent"` // 利確ライン（%）
	InitialCapital    float64 `json:"initialCapital"`
}
```

- [ ] **Step 2: config.go に環境変数追加**

```go
Risk: RiskConfig{
	MaxPositionAmount: getEnvFloat("RISK_MAX_POSITION_AMOUNT", 5000),
	MaxDailyLoss:      getEnvFloat("RISK_MAX_DAILY_LOSS", 5000),
	StopLossPercent:   getEnvFloat("RISK_STOP_LOSS_PERCENT", 5),
	TakeProfitPercent: getEnvFloat("RISK_TAKE_PROFIT_PERCENT", 10), // デフォルト: SLの2倍
	InitialCapital:    getEnvFloat("RISK_INITIAL_CAPITAL", 10000),
},
```

- [ ] **Step 3: risk_test.go にテスト追加**

```go
func TestRiskManager_CheckTakeProfit_BuyPosition(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.TakeProfitPercent = 10
	rm := NewRiskManager(cfg)
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// 10% profit: 5000000 * 1.10 = 5500000
	targets := rm.CheckTakeProfit(7, 5500000)
	if len(targets) != 1 {
		t.Fatalf("expected 1 take-profit position, got %d", len(targets))
	}
}

func TestRiskManager_CheckTakeProfit_NoTrigger(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.TakeProfitPercent = 10
	rm := NewRiskManager(cfg)
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// Only 5% profit → not enough
	targets := rm.CheckTakeProfit(7, 5250000)
	if len(targets) != 0 {
		t.Fatalf("expected 0 take-profit positions, got %d", len(targets))
	}
}

func TestRiskManager_CheckTakeProfit_SellPosition(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.TakeProfitPercent = 10
	rm := NewRiskManager(cfg)
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideSell, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// Sell position profit when price drops: (5000000 - 4500000) / 5000000 = 10%
	targets := rm.CheckTakeProfit(7, 4500000)
	if len(targets) != 1 {
		t.Fatalf("expected 1 take-profit position, got %d", len(targets))
	}
}

func TestRiskManager_CheckTakeProfit_ZeroConfig_NeverTriggers(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.TakeProfitPercent = 0 // disabled
	rm := NewRiskManager(cfg)
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	targets := rm.CheckTakeProfit(7, 99999999)
	if len(targets) != 0 {
		t.Fatalf("expected 0 when take-profit disabled, got %d", len(targets))
	}
}
```

- [ ] **Step 4: テスト FAIL 確認**

Run: `cd backend && go test ./internal/usecase/ -run TestRiskManager_CheckTakeProfit -v`
Expected: compilation error (`CheckTakeProfit` not defined)

- [ ] **Step 5: risk.go に CheckTakeProfit 実装**

```go
func (rm *RiskManager) CheckTakeProfit(symbolID int64, currentPrice float64) []entity.Position {
	if rm.config.TakeProfitPercent <= 0 {
		return nil
	}

	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var result []entity.Position
	for _, pos := range rm.positions {
		if pos.SymbolID != symbolID {
			continue
		}
		var profitPercent float64
		if pos.OrderSide == entity.OrderSideBuy {
			profitPercent = (currentPrice - pos.Price) / pos.Price * 100
		} else {
			profitPercent = (pos.Price - currentPrice) / pos.Price * 100
		}
		if profitPercent >= rm.config.TakeProfitPercent {
			result = append(result, pos)
		}
	}
	return result
}
```

- [ ] **Step 6: テスト PASS 確認**

Run: `cd backend && go test ./internal/usecase/ -run TestRiskManager_CheckTakeProfit -v`
Expected: ALL PASS

- [ ] **Step 7: pipeline.go の runStopLossMonitor に利確チェック追加**

`runStopLossMonitor` 内、stop-loss チェックの後に以下を追加:

```go
// Take-profit チェック
tpTargets := p.riskMgr.CheckTakeProfit(t.SymbolID, t.Last)
for _, pos := range tpTargets {
	slog.Info("pipeline: take-profit triggered",
		"positionID", pos.ID, "side", pos.OrderSide, "entryPrice", pos.Price, "currentPrice", t.Last)

	clientOrderID := newAgentClientOrderID("takeprofit")
	result, err := p.orderExecutor.ClosePosition(ctx, clientOrderID, pos, t.Last)
	if err != nil {
		slog.Error("pipeline: take-profit close failed", "error", err)
		continue
	}
	if result.Executed {
		slog.Info("pipeline: take-profit closed", "orderID", result.OrderID)
		closeSide := string(entity.OrderSideSell)
		if pos.OrderSide == entity.OrderSideSell {
			closeSide = string(entity.OrderSideBuy)
		}
		p.recordTrade(ctx, pos.SymbolID, result.OrderID, closeSide, "close", t.Last, pos.RemainingAmount, "take-profit", false)
		p.persistRiskState(ctx)
	}
}
```

- [ ] **Step 8: risk_test.go の defaultRiskConfig を更新**

```go
func defaultRiskConfig() entity.RiskConfig {
	return entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		TakeProfitPercent: 10,
		InitialCapital:    10000,
	}
}
```

- [ ] **Step 9: 全体テスト PASS 確認**

Run: `cd backend && go test ./...`
Expected: ALL PASS

- [ ] **Step 10: コミット → PR**

```bash
git checkout main && git pull
git checkout -b improve/take-profit
git add backend/internal/domain/entity/risk.go backend/config/config.go backend/internal/usecase/risk.go backend/internal/usecase/risk_test.go backend/cmd/pipeline.go
git commit -m "feat(risk): add take-profit exit logic

Add CheckTakeProfit to RiskManager with configurable RISK_TAKE_PROFIT_PERCENT (default 10%).
Integrate into stop-loss monitor loop for real-time profit taking.
Risk/reward ratio is now 1:2 (5% SL : 10% TP) by default."
git push -u origin improve/take-profit
gh pr create --base main --title "feat(risk): add take-profit exit logic (R:R 1:2)" --body "$(cat <<'EOF'
## Summary
- RiskManager に `CheckTakeProfit` メソッドを追加
- `RISK_TAKE_PROFIT_PERCENT` 環境変数（デフォルト 10%）で利確ラインを設定
- ストップロスモニタループに利確チェックを統合
- デフォルトのリスクリワード比は 1:2（SL 5% : TP 10%）

## Why
利確ロジックがなく、含み益が出てもいずれ損切りで終わる非対称なリスクプロファイルが負け続ける原因。

## Test plan
- [ ] `go test ./internal/usecase/ -run TestRiskManager_CheckTakeProfit -v` — 全テスト PASS
- [ ] `go test ./...` — 既存テスト regression なし

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Task 3: Trailing Stop-Loss

**Branch:** `improve/trailing-stop`

**Problem:** 固定5%ストップロスでは含み益を守れない。利益が出た後に反転して損切りになる。

**Solution:** RiskManager にポジションごとの最高値/最安値を追跡するフィールドを追加し、トレーリングストップを実装する。

**Files:**
- Modify: `backend/internal/usecase/risk.go` — 高値追跡 + `UpdateHighWaterMark` + `CheckTrailingStop`
- Modify: `backend/internal/usecase/risk_test.go` — テスト追加
- Modify: `backend/cmd/pipeline.go` — モニタループにトレーリングストップチェック追加

### Steps

- [ ] **Step 1: risk_test.go にテスト追加**

```go
func TestRiskManager_TrailingStop_BuyPosition(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// Price goes up to 5500000 then drops
	rm.UpdateHighWaterMark(1, 5500000)
	// Trail = 5% of high water mark: 5500000 * 0.95 = 5225000
	// Current price 5200000 < 5225000 → trigger
	targets := rm.CheckTrailingStop(7, 5200000)
	if len(targets) != 1 {
		t.Fatalf("expected 1 trailing stop position, got %d", len(targets))
	}
}

func TestRiskManager_TrailingStop_NoTriggerAboveTrail(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	rm.UpdateHighWaterMark(1, 5500000)
	// Trail = 5225000; price 5300000 > 5225000 → no trigger
	targets := rm.CheckTrailingStop(7, 5300000)
	if len(targets) != 0 {
		t.Fatalf("expected 0 trailing stop positions, got %d", len(targets))
	}
}

func TestRiskManager_TrailingStop_OnlyActivatesAfterProfit(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// No high water mark set (or entry price = high water mark)
	// Trailing stop should not activate until position is in profit
	// Price drops to 4800000 — this should be caught by regular stop-loss, not trailing
	targets := rm.CheckTrailingStop(7, 4800000)
	if len(targets) != 0 {
		t.Fatalf("expected 0 — trailing stop should not activate before profit, got %d", len(targets))
	}
}

func TestRiskManager_TrailingStop_SellPosition(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideSell, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// Sell position: track low water mark (price went down to 4500000)
	rm.UpdateHighWaterMark(1, 4500000)
	// Trail for sell = 5% above low: 4500000 * 1.05 = 4725000
	// Price bounces to 4750000 → trigger
	targets := rm.CheckTrailingStop(7, 4750000)
	if len(targets) != 1 {
		t.Fatalf("expected 1 trailing stop for sell position, got %d", len(targets))
	}
}
```

- [ ] **Step 2: テスト FAIL 確認 (compilation error)**

- [ ] **Step 3: risk.go にトレーリングストップ実装**

`RiskManager` 構造体にフィールド追加:

```go
type RiskManager struct {
	config     entity.RiskConfig
	mu         sync.RWMutex
	balance    float64
	dailyLoss  float64
	positions  []entity.Position
	manualStop bool
	highWaterMarks map[int64]float64 // positionID → best price (high for buy, low for sell)
}
```

`NewRiskManager` で初期化:

```go
func NewRiskManager(config entity.RiskConfig) *RiskManager {
	return &RiskManager{
		config:         config,
		balance:        config.InitialCapital,
		highWaterMarks: make(map[int64]float64),
	}
}
```

新メソッド:

```go
// UpdateHighWaterMark はポジションの最良価格を更新する。
// buy ポジション: 最高値を追跡。sell ポジション: 最安値を追跡。
func (rm *RiskManager) UpdateHighWaterMark(positionID int64, currentPrice float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	existing, ok := rm.highWaterMarks[positionID]
	if !ok {
		rm.highWaterMarks[positionID] = currentPrice
		return
	}

	// ポジションの方向を判定
	var isBuy bool
	for _, pos := range rm.positions {
		if pos.ID == positionID {
			isBuy = pos.OrderSide == entity.OrderSideBuy
			break
		}
	}

	if isBuy && currentPrice > existing {
		rm.highWaterMarks[positionID] = currentPrice
	} else if !isBuy && currentPrice < existing {
		rm.highWaterMarks[positionID] = currentPrice
	}
}

// CheckTrailingStop はトレーリングストップに達したポジションを返す。
// エントリー価格より利益が出ていない場合は発動しない（通常のストップロスに委ねる）。
func (rm *RiskManager) CheckTrailingStop(symbolID int64, currentPrice float64) []entity.Position {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var result []entity.Position
	for _, pos := range rm.positions {
		if pos.SymbolID != symbolID {
			continue
		}

		hwm, ok := rm.highWaterMarks[pos.ID]
		if !ok {
			continue
		}

		if pos.OrderSide == entity.OrderSideBuy {
			// 利益が出ていない場合はスキップ
			if hwm <= pos.Price {
				continue
			}
			// トレーリングストップライン = 最高値 × (1 - SL%)
			trailLine := hwm * (1 - rm.config.StopLossPercent/100)
			if currentPrice <= trailLine {
				result = append(result, pos)
			}
		} else {
			// sell: 利益が出ていない場合はスキップ
			if hwm >= pos.Price {
				continue
			}
			// トレーリングストップライン = 最安値 × (1 + SL%)
			trailLine := hwm * (1 + rm.config.StopLossPercent/100)
			if currentPrice >= trailLine {
				result = append(result, pos)
			}
		}
	}
	return result
}
```

`UpdatePositions` でクリーンアップ追加:

```go
func (rm *RiskManager) UpdatePositions(positions []entity.Position) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.positions = positions

	// 閉じたポジションの high water mark をクリーンアップ
	active := make(map[int64]bool, len(positions))
	for _, pos := range positions {
		active[pos.ID] = true
	}
	for id := range rm.highWaterMarks {
		if !active[id] {
			delete(rm.highWaterMarks, id)
		}
	}
}
```

- [ ] **Step 4: テスト PASS 確認**

Run: `cd backend && go test ./internal/usecase/ -run TestRiskManager_TrailingStop -v`
Expected: ALL PASS

- [ ] **Step 5: pipeline.go のストップロスモニタに統合**

`runStopLossMonitor` 内のティッカー受信ループに追加:

```go
// High water mark 更新
positions, posErr := p.restClient.GetPositions(ctx, snap.symbolID)
if posErr == nil {
	for _, pos := range positions {
		p.riskMgr.UpdateHighWaterMark(pos.ID, t.Last)
	}
}

// Trailing stop チェック（通常 SL より先にチェック）
trailTargets := p.riskMgr.CheckTrailingStop(t.SymbolID, t.Last)
for _, pos := range trailTargets {
	slog.Info("pipeline: trailing stop triggered",
		"positionID", pos.ID, "side", pos.OrderSide, "entryPrice", pos.Price, "currentPrice", t.Last)

	clientOrderID := newAgentClientOrderID("trailstop")
	result, err := p.orderExecutor.ClosePosition(ctx, clientOrderID, pos, t.Last)
	if err != nil {
		slog.Error("pipeline: trailing stop close failed", "error", err)
		continue
	}
	if result.Executed {
		slog.Info("pipeline: trailing stop closed", "orderID", result.OrderID)
		closeSide := string(entity.OrderSideSell)
		if pos.OrderSide == entity.OrderSideSell {
			closeSide = string(entity.OrderSideBuy)
		}
		p.recordTrade(ctx, pos.SymbolID, result.OrderID, closeSide, "close", t.Last, pos.RemainingAmount, "trailing-stop", false)
		p.persistRiskState(ctx)
	}
}
```

- [ ] **Step 6: 全体テスト PASS 確認**

Run: `cd backend && go test ./...`
Expected: ALL PASS

- [ ] **Step 7: コミット → PR**

```bash
git checkout main && git pull
git checkout -b improve/trailing-stop
git add backend/internal/usecase/risk.go backend/internal/usecase/risk_test.go backend/cmd/pipeline.go
git commit -m "feat(risk): add trailing stop-loss

Track high water mark per position. Once position is in profit,
trail stop at StopLossPercent from the best price reached.
Positions not yet in profit fall through to regular stop-loss."
git push -u origin improve/trailing-stop
gh pr create --base main --title "feat(risk): add trailing stop-loss to protect profits" --body "$(cat <<'EOF'
## Summary
- ポジションごとの最高値/最安値（high water mark）を追跡
- 含み益が出たポジションに対してトレーリングストップを適用
- まだ含み益が出ていないポジションは従来の固定ストップロスで保護
- ポジションクローズ時に自動クリーンアップ

## Why
固定5%ストップロスでは含み益を守れない。利益が出た後に反転して損切りになるケースが多い。

## Test plan
- [ ] `go test ./internal/usecase/ -run TestRiskManager_TrailingStop -v` — 全テスト PASS
- [ ] `go test ./...` — 既存テスト regression なし

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Task 4: Consecutive Loss Breaker

**Branch:** `improve/consecutive-loss-breaker`

**Problem:** 連敗しても止まらず、日次損失上限に達するまで負け続ける。

**Solution:** RiskManager に連敗カウンターを追加し、N連敗（デフォルト3）で冷却期間（デフォルト30分）に入る。

**Files:**
- Modify: `backend/config/config.go` — `MaxConsecutiveLosses` と `CooldownMinutes` 追加
- Modify: `backend/internal/domain/entity/risk.go` — 設定フィールド追加
- Modify: `backend/internal/usecase/risk.go` — 連敗追跡 + cooldown チェック
- Modify: `backend/internal/usecase/risk_test.go` — テスト追加

### Steps

- [ ] **Step 1: entity/risk.go に設定フィールド追加**

```go
type RiskConfig struct {
	MaxPositionAmount    float64 `json:"maxPositionAmount"`
	MaxDailyLoss         float64 `json:"maxDailyLoss"`
	StopLossPercent      float64 `json:"stopLossPercent"`
	TakeProfitPercent    float64 `json:"takeProfitPercent"`
	InitialCapital       float64 `json:"initialCapital"`
	MaxConsecutiveLosses int     `json:"maxConsecutiveLosses"` // 連敗上限（0=無効）
	CooldownMinutes      int     `json:"cooldownMinutes"`      // 冷却期間（分）
}
```

- [ ] **Step 2: config.go に環境変数追加**

```go
Risk: RiskConfig{
	// ...existing...
	MaxConsecutiveLosses: getEnvInt("RISK_MAX_CONSECUTIVE_LOSSES", 3),
	CooldownMinutes:     getEnvInt("RISK_COOLDOWN_MINUTES", 30),
},
```

- [ ] **Step 3: risk_test.go にテスト追加**

```go
func TestRiskManager_ConsecutiveLossBreaker_BlocksAfterNLosses(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.MaxConsecutiveLosses = 3
	cfg.CooldownMinutes = 30
	rm := NewRiskManager(cfg)

	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()

	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if result.Approved {
		t.Fatal("order should be rejected after 3 consecutive losses")
	}
	if result.Reason == "" {
		t.Fatal("expected rejection reason")
	}
}

func TestRiskManager_ConsecutiveLossBreaker_ResetOnWin(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.MaxConsecutiveLosses = 3
	cfg.CooldownMinutes = 30
	rm := NewRiskManager(cfg)

	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()
	rm.ResetConsecutiveLosses() // win resets counter

	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("order should be approved after win resets counter: %s", result.Reason)
	}
}

func TestRiskManager_ConsecutiveLossBreaker_DisabledWhenZero(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.MaxConsecutiveLosses = 0 // disabled
	rm := NewRiskManager(cfg)

	for i := 0; i < 10; i++ {
		rm.RecordConsecutiveLoss()
	}

	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("should be approved when breaker disabled: %s", result.Reason)
	}
}

func TestRiskManager_ConsecutiveLossBreaker_CloseOrdersAllowed(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.MaxConsecutiveLosses = 3
	cfg.CooldownMinutes = 30
	rm := NewRiskManager(cfg)

	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()

	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideSell, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000, IsClose: true,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("close orders should be allowed even during cooldown: %s", result.Reason)
	}
}
```

- [ ] **Step 4: テスト FAIL 確認 (compilation error)**

- [ ] **Step 5: risk.go に連敗ロジック実装**

`RiskManager` にフィールド追加:

```go
type RiskManager struct {
	config           entity.RiskConfig
	mu               sync.RWMutex
	balance          float64
	dailyLoss        float64
	positions        []entity.Position
	manualStop       bool
	highWaterMarks   map[int64]float64
	consecutiveLosses int
	cooldownUntil    time.Time
}
```

新メソッド:

```go
func (rm *RiskManager) RecordConsecutiveLoss() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.consecutiveLosses++

	if rm.config.MaxConsecutiveLosses > 0 && rm.consecutiveLosses >= rm.config.MaxConsecutiveLosses {
		rm.cooldownUntil = time.Now().Add(time.Duration(rm.config.CooldownMinutes) * time.Minute)
	}
}

func (rm *RiskManager) ResetConsecutiveLosses() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.consecutiveLosses = 0
	rm.cooldownUntil = time.Time{}
}
```

`CheckOrder` に cooldown チェック追加（`manualStop` チェックの後、`dailyLoss` チェックの前）:

```go
if rm.config.MaxConsecutiveLosses > 0 && !rm.cooldownUntil.IsZero() && time.Now().Before(rm.cooldownUntil) {
	return entity.RiskCheckResult{
		Approved: false,
		Reason:   fmt.Sprintf("cooldown: %d consecutive losses, trading paused until %s", rm.consecutiveLosses, rm.cooldownUntil.Format("15:04")),
	}
}
```

- [ ] **Step 6: pipeline.go でストップロス時に RecordConsecutiveLoss を呼ぶ**

`runStopLossMonitor` のストップロス実行後:

```go
if result.Executed {
	// ...existing logging/recording...
	p.riskMgr.RecordConsecutiveLoss()
}
```

利確実行後:

```go
if result.Executed {
	// ...existing logging/recording...
	p.riskMgr.ResetConsecutiveLosses()
}
```

- [ ] **Step 7: テスト PASS 確認**

Run: `cd backend && go test ./internal/usecase/ -run TestRiskManager_ConsecutiveLoss -v`
Expected: ALL PASS

- [ ] **Step 8: 全体テスト PASS 確認**

Run: `cd backend && go test ./...`
Expected: ALL PASS

- [ ] **Step 9: コミット → PR**

```bash
git checkout main && git pull
git checkout -b improve/consecutive-loss-breaker
git add backend/internal/domain/entity/risk.go backend/config/config.go backend/internal/usecase/risk.go backend/internal/usecase/risk_test.go backend/cmd/pipeline.go
git commit -m "feat(risk): add consecutive loss breaker with cooldown

Block new orders after N consecutive stop-losses (default 3).
Cooldown period of M minutes (default 30) before resuming.
Winning trades (take-profit) reset the counter.
Close orders are always allowed during cooldown."
git push -u origin improve/consecutive-loss-breaker
gh pr create --base main --title "feat(risk): consecutive loss breaker with cooldown" --body "$(cat <<'EOF'
## Summary
- N連敗（デフォルト3）後に新規注文をブロック
- M分間（デフォルト30）の冷却期間を設ける
- 利確成功時にカウンターをリセット
- 決済注文は冷却期間中も許可
- `RISK_MAX_CONSECUTIVE_LOSSES` / `RISK_COOLDOWN_MINUTES` 環境変数で設定

## Why
連敗しても止まらず日次損失上限に達するまで負け続ける問題を解決。相場が不利な時に一旦停止して損失を最小化する。

## Test plan
- [ ] `go test ./internal/usecase/ -run TestRiskManager_ConsecutiveLoss -v` — 全テスト PASS
- [ ] `go test ./...` — 既存テスト regression なし

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Dependency Note

Task 1 (MACD confirmation) は独立。Task 2 (take-profit) は独立。Task 3 (trailing stop) は Task 2 の `TakeProfitPercent` フィールドが entity/risk.go に追加された前提。Task 4 (loss breaker) は Task 2 + 3 のフィールドが追加された前提。

**推奨マージ順序:** Task 1 → Task 2 → Task 3 → Task 4

各 PR を main にマージしてから次のブランチを切ること。
