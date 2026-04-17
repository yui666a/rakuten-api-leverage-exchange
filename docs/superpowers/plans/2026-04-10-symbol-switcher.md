# Symbol Switcher (フロント画面からの取引銘柄切替) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** フロントエンドのダッシュボードから取引対象銘柄（BTC/JPY, ETH/JPY 等）を切り替えられるようにする。銘柄変更時はパイプライン・WebSocket 購読も動的に追従する。

**Architecture:** バックエンドに (1) 銘柄一覧 API、(2) 取引設定 GET/PUT API を追加し、TradingPipeline に symbolID のランタイム切替メソッドを実装する。フロントエンドではグローバルな `symbolId` state を React Context で管理し、ダッシュボード上部にセレクタを配置して全 hook に伝播させる。

**Tech Stack:** Go (Gin), React 19 (TanStack Router / React Query), TypeScript, Tailwind CSS v4

---

## File Structure

### Backend (新規)
- `backend/internal/interfaces/api/handler/symbol.go` — `GET /api/v1/symbols` ハンドラ
- `backend/internal/interfaces/api/handler/trading_config.go` — `GET /PUT /api/v1/trading-config` ハンドラ

### Backend (変更)
- `backend/internal/interfaces/api/router.go` — 新エンドポイント登録 + Dependencies に RESTClient 利用
- `backend/cmd/pipeline.go` — `SwitchSymbol()` メソッド追加
- `backend/cmd/main.go` — `startMarketRelay` を symbolID 切替対応に変更

### Frontend (新規)
- `frontend/src/hooks/useSymbols.ts` — `GET /symbols` フック
- `frontend/src/hooks/useTradingConfig.ts` — `GET/PUT /trading-config` フック
- `frontend/src/contexts/SymbolContext.tsx` — 選択 symbolId の Context + Provider
- `frontend/src/components/SymbolSelector.tsx` — 銘柄セレクタ UI

### Frontend (変更)
- `frontend/src/lib/api.ts` — `Symbol`, `TradingConfig` 型追加
- `frontend/src/routes/__root.tsx` — SymbolProvider ラップ
- `frontend/src/routes/index.tsx` — ハードコード `7` → Context 経由
- `frontend/src/routes/settings.tsx` — ハードコード `7` → Context 経由
- `frontend/src/routes/history.tsx` — ハードコード `7` → Context 経由
- `frontend/src/components/AppFrame.tsx` — SymbolSelector 配置
- `frontend/src/components/LiveTickerCard.tsx` — ハードコード "BTC/JPY" → 動的表示

### Backend テスト (新規)
- `backend/cmd/pipeline_test.go` — `SwitchSymbol` と `Start`/`Stop` の並行性テスト（`go test -race` 前提）

---

## レビュー反映履歴 (2026-04-11)

2人のレビュワーからの指摘を反映済み:

- **C-NEW1 (Reviewer 1):** `main.go` の `ctx, cancel` 定義と `onSymbolSwitch` を `NewRouter` 呼び出しより前に移動（Task 4 Step 2）
- **Codex #1 High:** `SwitchSymbol` と `Stop` の競合 → `Start`/`Stop` に `switchMu` を拡張（Task 2 Step 2）
- **Codex #2 High:** `SwitchSymbol` の bootstrap 中に `Start` が割り込む問題 → 同上修正で解消（Task 2 Step 2）
- **M-NEW2 (Reviewer 1):** `tradeAmount` 誤上書き → `tradingConfig` 未ロード時は `switchSymbol` を disabled（Task 7/8）
- **Codex #3 Medium:** 無効銘柄フォールバック → 失敗時は `symbols` から最初の有効銘柄を選ぶ（Task 7）
- **Codex #4 Medium:** テストギャップ → 並行性ユニットテスト追加（Task 2 Step 9）+ 統合手動確認追加（Task 11 Step 9）
- **m3 (Reviewer 1):** Task 11 Step 4 の symbolId を Step 2 の結果から動的に取得

---

## Task 1: Backend — GET /api/v1/symbols エンドポイント

**Files:**
- Create: `backend/internal/interfaces/api/handler/symbol.go`
- Modify: `backend/internal/interfaces/api/router.go`

- [ ] **Step 1: SymbolHandler を作成**

```go
// backend/internal/interfaces/api/handler/symbol.go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
)

type SymbolHandler struct {
	restClient *rakuten.RESTClient
}

func NewSymbolHandler(restClient *rakuten.RESTClient) *SymbolHandler {
	return &SymbolHandler{restClient: restClient}
}

// GetSymbols handles GET /api/v1/symbols.
func (h *SymbolHandler) GetSymbols(c *gin.Context) {
	symbols, err := h.restClient.GetSymbols(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, symbols)
}
```

- [ ] **Step 2: ルーターに登録**

`backend/internal/interfaces/api/router.go` の `NewRouter` 関数内、`orderHandler` 登録ブロックの後に追加:

```go
	if deps.RESTClient != nil {
		symbolHandler := handler.NewSymbolHandler(deps.RESTClient)
		v1.GET("/symbols", symbolHandler.GetSymbols)
	}
```

- [ ] **Step 3: ビルド確認**

Run: `cd backend && go build ./...`
Expected: エラーなし

- [ ] **Step 4: Commit**

```bash
git add backend/internal/interfaces/api/handler/symbol.go backend/internal/interfaces/api/router.go
git commit -m "feat: add GET /api/v1/symbols endpoint for listing tradable pairs"
```

---

## Task 2: Backend — TradingPipeline を RWMutex 化 + SwitchSymbol メソッド

> **Review fix (H1):** `evaluate`/`runStopLossMonitor` がロックなしで `symbolID`/`tradeAmount` を読んでいるデータレースを修正。`sync.Mutex` → `sync.RWMutex` に変更し、読み取り側はスナップショットを取得。`SwitchSymbol` は停止→更新→起動を一貫してロック内で実行する。
> **注意:** `interval` フィールドは SwitchSymbol で変更しないため snapshot 対象外（不変フィールドとして扱う）。

**Files:**
- Modify: `backend/cmd/pipeline.go`

- [ ] **Step 1: Mutex を RWMutex に変更**

`backend/cmd/pipeline.go` の `TradingPipeline` struct の `mu` フィールドを変更:

```go
type TradingPipeline struct {
	mu     sync.RWMutex
	cancel context.CancelFunc

	symbolID    int64
	interval    time.Duration
	tradeAmount float64
	// ... (以降のフィールドは変更なし)
```

- [ ] **Step 2: Start/Stop/Running のロックを RWMutex + switchMu に変更**

> **Review fix (Codex #1/#2):** `Start()` / `Stop()` にも `switchMu` を取得させて `SwitchSymbol` 全体と直列化する。これにより「SwitchSymbol の onSwitch 実行中に Stop が来て握りつぶされる」「bootstrap 完了前に Start が入ってローソク足不足で評価が走る」問題を防ぐ。
> **ロック順序:** 必ず `switchMu` → `mu` の順で取得する。逆順で取るコードパスを作らないこと。

`Start()` / `Stop()` を以下に変更。内部実装を `startLocked` / `stopLocked` に分離し、`SwitchSymbol` からは `switchMu` 保持したまま呼べるようにする。

**ロック順序の原則:** 常に `switchMu` → `mu` の順。逆順で取得するコードパスを作らない。

```go
// Start はパイプラインを開始する。すでに実行中なら何もしない。
// switchMu で SwitchSymbol との並行実行を防ぐ。
func (p *TradingPipeline) Start() {
	p.switchMu.Lock()
	defer p.switchMu.Unlock()
	p.startLocked()
}

// startLocked は switchMu を保持した状態で呼ぶこと。
// SwitchSymbol から再利用するために分離されている。
func (p *TradingPipeline) startLocked() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		return // already running
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go p.runTradingLoop(ctx)
	go p.runStopLossMonitor(ctx)

	slog.Info("trading pipeline started")
}

// Stop はパイプラインを停止する。
// switchMu で SwitchSymbol との並行実行を防ぐ。
func (p *TradingPipeline) Stop() {
	p.switchMu.Lock()
	defer p.switchMu.Unlock()
	p.stopLocked()
}

// stopLocked は switchMu を保持した状態で呼ぶこと。
func (p *TradingPipeline) stopLocked() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return
	}

	p.cancel()
	p.cancel = nil

	slog.Info("trading pipeline stopped")
}
```

`Running()` は読み取りのみなので `switchMu` は不要。`RLock` に変更:

```go
func (p *TradingPipeline) Running() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cancel != nil
}
```

- [ ] **Step 3: SymbolID / TradeAmount getter を追加**

`Running()` の後に追加:

```go
// SymbolID は現在の取引対象シンボルIDを返す。
func (p *TradingPipeline) SymbolID() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.symbolID
}

// TradeAmount は現在の1回あたりの注文金額を返す。
func (p *TradingPipeline) TradeAmount() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tradeAmount
}
```

- [ ] **Step 4: スナップショット取得用の内部メソッドを追加**

```go
// tradingSnapshot は evaluate / stopLoss ループの冒頭でロック下にコピーを取るための構造体。
type tradingSnapshot struct {
	symbolID    int64
	tradeAmount float64
}

func (p *TradingPipeline) snapshot() tradingSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return tradingSnapshot{
		symbolID:    p.symbolID,
		tradeAmount: p.tradeAmount,
	}
}
```

- [ ] **Step 5: evaluate を snapshot 経由に変更**

`evaluate` メソッドの冒頭で snapshot を取得し、`p.symbolID` / `p.tradeAmount` の直接参照をすべて置換:

```go
func (p *TradingPipeline) evaluate(ctx context.Context) {
	snap := p.snapshot()

	// 1. 最新ティッカーを取得
	latestTicker, err := p.marketDataSvc.GetLatestTicker(ctx, snap.symbolID)
	if err != nil || latestTicker == nil {
		slog.Warn("pipeline: failed to get latest ticker", "error", err)
		return
	}

	// 2. テクニカル指標を計算
	indicators, err := p.indicatorCalc.Calculate(ctx, snap.symbolID, "15min")
	if err != nil {
		slog.Warn("pipeline: failed to calculate indicators", "error", err)
		return
	}

	// 3. 戦略判定
	signal, err := p.strategyEngine.Evaluate(ctx, *indicators, latestTicker.Last)
	if err != nil {
		slog.Warn("pipeline: failed to evaluate strategy", "error", err)
		return
	}

	slog.Info("pipeline: signal evaluated", "action", signal.Action, "reason", signal.Reason, "price", latestTicker.Last)

	if signal.Action == entity.SignalActionHold {
		return
	}

	// 4. 同一方向のポジションを保持中ならスキップ
	positions, err := p.restClient.GetPositions(ctx, snap.symbolID)
	if err != nil {
		slog.Warn("pipeline: failed to get positions", "error", err)
		return
	}

	side := entity.OrderSideBuy
	if signal.Action == entity.SignalActionSell {
		side = entity.OrderSideSell
	}

	for _, pos := range positions {
		if pos.OrderSide == side && pos.RemainingAmount > 0 {
			slog.Info("pipeline: skip, already holding position", "action", signal.Action, "side", side, "positionID", pos.ID)
			return
		}
	}

	// 5. 注文数量を計算
	price := latestTicker.BestAsk
	if signal.Action == entity.SignalActionSell {
		price = latestTicker.BestBid
	}
	if price <= 0 {
		slog.Warn("pipeline: invalid price, skip", "price", price)
		return
	}

	amount := snap.tradeAmount / price
	amount = math.Floor(amount*10000) / 10000
	if amount <= 0 {
		slog.Warn("pipeline: calculated amount is 0, skip", "tradeAmount", snap.tradeAmount, "price", price)
		return
	}

	// 6. 注文実行
	result, err := p.orderExecutor.ExecuteSignal(ctx, *signal, price, amount)
	if err != nil {
		slog.Error("pipeline: order execution failed", "error", err)
		return
	}

	if result.Executed {
		slog.Info("pipeline: order executed", "orderID", result.OrderID, "side", side, "amount", amount, "price", price)
		p.recordTrade(ctx, snap.symbolID, result.OrderID, string(side), "open", price, amount, signal.Reason, false)
		p.syncState(ctx)
		p.persistRiskState(ctx)
	} else {
		slog.Info("pipeline: order not executed", "reason", result.Reason)
	}
}
```

- [ ] **Step 6: runStopLossMonitor を snapshot 経由に変更**

`runStopLossMonitor` 内の `p.symbolID` 参照を snapshot 経由にする:

```go
func (p *TradingPipeline) runStopLossMonitor(ctx context.Context) {
	tickerCh := p.marketDataSvc.SubscribeTicker()
	defer p.marketDataSvc.UnsubscribeTicker(tickerCh)

	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-tickerCh:
			if !ok {
				return
			}
			snap := p.snapshot()
			if t.SymbolID != snap.symbolID {
				continue
			}

			targets := p.riskMgr.CheckStopLoss(t.SymbolID, t.Last)
			for _, pos := range targets {
				slog.Warn("pipeline: stop-loss triggered",
					"positionID", pos.ID, "side", pos.OrderSide, "entryPrice", pos.Price, "currentPrice", t.Last)

				result, err := p.orderExecutor.ClosePosition(ctx, pos, t.Last)
				if err != nil {
					slog.Error("pipeline: stop-loss close failed", "error", err)
					continue
				}
				if result.Executed {
					slog.Info("pipeline: stop-loss closed", "orderID", result.OrderID)
					loss := math.Abs(pos.FloatingProfit)
					p.riskMgr.RecordLoss(loss)
					closeSide := string(entity.OrderSideSell)
					if pos.OrderSide == entity.OrderSideSell {
						closeSide = string(entity.OrderSideBuy)
					}
					p.recordTrade(ctx, pos.SymbolID, result.OrderID, closeSide, "close", t.Last, pos.RemainingAmount, "stop-loss", true)
					p.persistRiskState(ctx)
				}
			}
		}
	}
}
```

- [ ] **Step 7: syncState を snapshot 経由に変更**

```go
func (p *TradingPipeline) syncState(ctx context.Context) {
	snap := p.snapshot()
	positions, err := p.restClient.GetPositions(ctx, snap.symbolID)
	if err != nil {
		slog.Warn("pipeline: failed to sync positions", "error", err)
	} else {
		p.riskMgr.UpdatePositions(positions)
	}

	assets, err := p.restClient.GetAssets(ctx)
	if err != nil {
		slog.Warn("pipeline: failed to sync assets", "error", err)
	} else {
		for _, a := range assets {
			if a.Currency == "JPY" {
				if balance, err := strconv.ParseFloat(a.OnhandAmount, 64); err == nil {
					p.riskMgr.UpdateBalance(balance)
				}
			}
		}
	}
}
```

- [ ] **Step 8: switchMu フィールドと SwitchSymbol メソッドを追加**

> **Review fix (H1 連続切替):** `SwitchSymbol` を pipeline 内の専用 mutex (`switchMu`) でシリアライズする。連続切替時も前の切替処理（bootstrap 含む）が完了してから次の切替が始まるため、逆順適用のリスクがなくなる。
> **Review fix (H-hang 再考):** `switchMu` で順序保証するため `onSwitch` を同期実行する。HTTP handler は 1リクエストあたり bootstrap API 1回 + WS切替を待つが、bootstrap は約 1秒程度で完了するため許容範囲。
> **Review fix (Codex #1/#2):** `Start()` / `Stop()` も Step 2 で `switchMu` を取得するよう変更済み。これにより `SwitchSymbol` の onSwitch 実行中に Stop/Start が割り込まず、停止要求の握りつぶしや bootstrap 完了前の Start を防ぐ。
> **方針:** `Start()` を直接呼ぶと `switchMu` の二重取得でデッドロックするため、SwitchSymbol 内では内部実装の `startLocked()`（ロックを取らない版）を呼ぶ。

struct に専用 mutex を追加:

```go
type TradingPipeline struct {
	mu       sync.RWMutex   // symbolID/tradeAmount/cancel 用
	switchMu sync.Mutex     // SwitchSymbol 全体をシリアライズ
	cancel   context.CancelFunc

	symbolID    int64
	interval    time.Duration
	tradeAmount float64
	// ... (以降のフィールドは変更なし)
```

SwitchSymbol メソッドを追加:

```go
// SwitchSymbol は取引対象シンボルを切り替える。
// switchMu でシリアライズすることで:
//   - 連続切替の順序保証（逆順適用を防ぐ）
//   - SwitchSymbol 実行中の Start/Stop 割込みを防ぐ（Codex #1/#2 対応）
//
// 処理順序: 停止 → フィールド更新 → onSwitch（bootstrap + WS切替）→ 再開
// onSwitch は同期実行されるため HTTP レスポンスは bootstrap 完了まで待つ。
//
// ロック順序: switchMu → mu（startLocked/stopLocked 内部で mu を取る）
func (p *TradingPipeline) SwitchSymbol(symbolID int64, tradeAmount float64, onSwitch func(oldID, newID int64)) {
	p.switchMu.Lock()
	defer p.switchMu.Unlock()

	// 現在の状態を読み取り（switchMu 保持中なので Start/Stop は進行できない）
	p.mu.RLock()
	oldID := p.symbolID
	wasRunning := p.cancel != nil
	p.mu.RUnlock()

	// 停止（switchMu 保持済みなので stopLocked を使う）
	if wasRunning {
		p.stopLocked()
	}

	// フィールド更新
	p.mu.Lock()
	p.symbolID = symbolID
	if tradeAmount > 0 {
		p.tradeAmount = tradeAmount
	}
	p.mu.Unlock()

	// onSwitch（bootstrapCandles + WS切替）を同期実行
	// switchMu で順序保証されているため、連続切替でも逆順適用されない
	// この間 Start/Stop は switchMu 待ちでブロックされるので、
	// bootstrap 完了前にパイプラインが動き出すことはない
	if onSwitch != nil {
		onSwitch(oldID, symbolID)
	}

	// 再開（switchMu 保持済みなので startLocked を使う）
	// bootstrap 完了後に再開することで、新シンボルの指標計算が即座に可能になる
	if wasRunning {
		p.startLocked()
	}
}
```

- [ ] **Step 9: 並行性ユニットテストを追加（Codex #4 対応）**

> **Review fix (Codex #4):** `SwitchSymbol` と `Start`/`Stop` の並行呼び出しで状態が一貫することを `-race` で検証する。実際の `marketDataSvc` や `restClient` は評価ループが走り出すと必要だが、Start 直後に Stop すれば `evaluate` が間に合わず nil 参照しないため、ダミー依存でテストできる（goroutine が spawn されても最初の `time.NewTicker` は走るが、`evaluate` が1回走る前に Stop できるよう interval を十分大きくする）。
> **方針:** ユニットテストは `backend/cmd` パッケージ内（`pipeline_test.go`）に置く。race 検出が目的なので「panic せず、最終状態が決定論的」であることを確認する。

`backend/cmd/pipeline_test.go` を新規作成:

```go
package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestPipeline はテスト用に最小構成の TradingPipeline を返す。
// 実際の評価ループが走らないよう interval は十分長く設定する。
// evaluate / runStopLossMonitor 内で nil 参照しないよう、必要な依存だけ nil でない値を渡す。
func newTestPipeline(t *testing.T) *TradingPipeline {
	t.Helper()
	return &TradingPipeline{
		symbolID:    7,
		interval:    1 * time.Hour, // 評価ループが走らない程度に長く
		tradeAmount: 1000,
		// marketDataSvc / indicatorCalc 等は nil のまま。
		// Start が goroutine を起動した直後に Stop するので、
		// evaluate が1回走る前にキャンセルされる前提。
		//
		// ただし runTradingLoop は起動直後に p.evaluate(ctx) を呼ぶため、
		// nil 参照で panic する。テストでは evaluate を呼ばない版の内部メソッドに置き換えるか、
		// 最小限のスタブを用意する必要がある。
		//
		// 実装ヒント: pipeline.go 側で `runTradingLoop` を直接触らず、
		// テスト対象を `SwitchSymbol` / `Start` / `Stop` の「ロック挙動」に絞るため、
		// runTradingLoop を差し替え可能にするか、下記のようにテスト用コンストラクタを用意する。
	}
}

// TestSwitchSymbol_ConcurrentStartStop は SwitchSymbol と Start/Stop が
// 並行実行されても panic せず、最終状態が一貫することを検証する。
// go test -race で実行すること。
func TestSwitchSymbol_ConcurrentStartStop(t *testing.T) {
	p := newTestPipelineForConcurrency(t)

	var wg sync.WaitGroup
	var switchCount atomic.Int64

	// 100回並行して Switch/Start/Stop を呼ぶ
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			p.SwitchSymbol(int64(7+i%3), 1000, func(oldID, newID int64) {
				// bootstrap の代わりに短い sleep で実処理を模擬
				time.Sleep(100 * time.Microsecond)
			})
			switchCount.Add(1)
		}(i)
		go func() {
			defer wg.Done()
			p.Start()
		}()
		go func() {
			defer wg.Done()
			p.Stop()
		}()
	}

	wg.Wait()

	// 最終的に Stop しておく
	p.Stop()
	if p.Running() {
		t.Errorf("pipeline should be stopped after final Stop, got Running=true")
	}
	if switchCount.Load() != 100 {
		t.Errorf("expected 100 switches, got %d", switchCount.Load())
	}
}

// TestSwitchSymbol_StopDuringSwitch は SwitchSymbol の onSwitch 実行中に
// Stop が来ても、最終的に停止状態になることを検証する（Codex #1 対応）。
func TestSwitchSymbol_StopDuringSwitch(t *testing.T) {
	p := newTestPipelineForConcurrency(t)

	p.Start()
	if !p.Running() {
		t.Fatal("pipeline should be running after Start")
	}

	// onSwitch が走っている最中に Stop を呼ぶ
	stopCalled := make(chan struct{})
	onSwitch := func(oldID, newID int64) {
		go func() {
			p.Stop()
			close(stopCalled)
		}()
		// Stop が switchMu 待ちでブロックされることを確認するため、
		// onSwitch 内で少し待つ
		time.Sleep(10 * time.Millisecond)
	}

	p.SwitchSymbol(8, 1000, onSwitch)

	// SwitchSymbol 完了後、Stop が switchMu を取得して走る
	<-stopCalled

	if p.Running() {
		t.Error("pipeline should be stopped, got Running=true")
	}
}
```

**補足:** `newTestPipelineForConcurrency` は `runTradingLoop` / `runStopLossMonitor` が nil 依存で panic しないよう、最小スタブを差し込んで pipeline を生成するヘルパー。pipeline.go 側で `runTradingLoop` を即 return するテスト用ビルドタグを切るか、`marketDataSvc` 等をインターフェース化してスタブを渡す。**実装時に現状の pipeline.go 構造を踏まえて最小限のリファクタを行うこと**（インターフェース化は影響範囲が大きいため、テスト用に `evaluate` をスタブ差し替え可能にする方法を推奨）。

- [ ] **Step 10: ビルド + race テスト確認**

Run: `cd backend && go build ./... && go test -race -run TestSwitchSymbol ./cmd`
Expected: エラーなし、race 検出なし

- [ ] **Step 11: Commit**

```bash
git add backend/cmd/pipeline.go backend/cmd/pipeline_test.go
git commit -m "feat: RWMutex + snapshot + SwitchSymbol with Start/Stop serialization"
```

---

## Task 3: Backend — GET/PUT /api/v1/trading-config エンドポイント

> **Review fix (C1/M4):** handler 側に別インターフェースを定義せず、`router.go` の `PipelineController` を直接使う。
> **Review fix (M1):** `PUT /trading-config` で `GetSymbols` を呼び、`enabled && !viewOnly && !closeOnly` を検証する。
> **Review fix (M-amount):** `tradeAmount` が 0 以下の場合は 400 を返す。
> **Review fix (M1 onSwitch 冗長):** handler は `onSwitch` を保持せず、router 側でクロージャで `SwitchSymbol` と `onSwitch` を包んで `switchSymbol func(id, amt)` として渡す。handler の責務が「switch する」ことだけに限定される。

**Files:**
- Create: `backend/internal/interfaces/api/handler/trading_config.go`
- Modify: `backend/internal/interfaces/api/router.go`

- [ ] **Step 1: PipelineController インターフェースを拡張**

`backend/internal/interfaces/api/router.go` の `PipelineController` インターフェースに追加:

```go
type PipelineController interface {
	Start()
	Stop()
	Running() bool
	SymbolID() int64
	TradeAmount() float64
	SwitchSymbol(symbolID int64, tradeAmount float64, onSwitch func(oldID, newID int64))
}
```

- [ ] **Step 2: TradingConfigHandler を作成（PipelineController を直接使用、別インターフェース不要）**

> **Review fix (C1):** handler 独自のインターフェースは定義しない。`PipelineController` は Go の implicit interface satisfaction により、handler パッケージから `api` パッケージを import せずに使える。handler は必要なメソッドだけを引数の型として受け取る。

```go
// backend/internal/interfaces/api/handler/trading_config.go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/rakuten"
)

// TradingConfigHandler は取引設定の取得・切替を行うハンドラ。
// M1 修正: onSwitch は router 側でクロージャとして switchSymbol に包まれるため、
// handler は onSwitch を知らない。
type TradingConfigHandler struct {
	getSymbolID    func() int64
	getTradeAmount func() float64
	switchSymbol   func(symbolID int64, tradeAmount float64)
	restClient     *rakuten.RESTClient
}

func NewTradingConfigHandler(
	getSymbolID func() int64,
	getTradeAmount func() float64,
	switchSymbol func(symbolID int64, tradeAmount float64),
	restClient *rakuten.RESTClient,
) *TradingConfigHandler {
	return &TradingConfigHandler{
		getSymbolID:    getSymbolID,
		getTradeAmount: getTradeAmount,
		switchSymbol:   switchSymbol,
		restClient:     restClient,
	}
}

type tradingConfigResponse struct {
	SymbolID    int64   `json:"symbolId"`
	TradeAmount float64 `json:"tradeAmount"`
}

type updateTradingConfigRequest struct {
	SymbolID    int64   `json:"symbolId"`
	TradeAmount float64 `json:"tradeAmount"`
}

// GetTradingConfig handles GET /api/v1/trading-config.
func (h *TradingConfigHandler) GetTradingConfig(c *gin.Context) {
	c.JSON(http.StatusOK, tradingConfigResponse{
		SymbolID:    h.getSymbolID(),
		TradeAmount: h.getTradeAmount(),
	})
}

// UpdateTradingConfig handles PUT /api/v1/trading-config.
func (h *TradingConfigHandler) UpdateTradingConfig(c *gin.Context) {
	var req updateTradingConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.SymbolID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbolId must be positive"})
		return
	}

	if req.TradeAmount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tradeAmount must be positive"})
		return
	}

	// シンボルの存在確認と取引可否の検証
	symbols, err := h.restClient.GetSymbols(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch symbols"})
		return
	}

	var found bool
	for _, s := range symbols {
		if s.ID == req.SymbolID {
			if !s.Enabled {
				c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is disabled"})
				return
			}
			if s.ViewOnly {
				c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is view-only"})
				return
			}
			if s.CloseOnly {
				c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is close-only"})
				return
			}
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown symbolId"})
		return
	}

	h.switchSymbol(req.SymbolID, req.TradeAmount)

	c.JSON(http.StatusOK, tradingConfigResponse{
		SymbolID:    h.getSymbolID(),
		TradeAmount: h.getTradeAmount(),
	})
}
```

- [ ] **Step 3: Dependencies に OnSymbolSwitch コールバックを追加**

`backend/internal/interfaces/api/router.go` の `Dependencies` struct に追加:

```go
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
	OnSymbolSwitch      func(oldID, newID int64)
}
```

- [ ] **Step 4: ルーターに trading-config エンドポイントを登録**

`NewRouter` 関数内、`symbolHandler` 登録の後に追加:

```go
	if deps.Pipeline != nil && deps.RESTClient != nil {
		// M1 修正: onSwitch を含む switchSymbol をクロージャで包んで handler に渡す
		pipelineForHandler := deps.Pipeline
		onSwitchForHandler := deps.OnSymbolSwitch
		switchSymbolFn := func(symbolID int64, tradeAmount float64) {
			pipelineForHandler.SwitchSymbol(symbolID, tradeAmount, onSwitchForHandler)
		}
		tradingConfigHandler := handler.NewTradingConfigHandler(
			deps.Pipeline.SymbolID,
			deps.Pipeline.TradeAmount,
			switchSymbolFn,
			deps.RESTClient,
		)
		v1.GET("/trading-config", tradingConfigHandler.GetTradingConfig)
		v1.PUT("/trading-config", tradingConfigHandler.UpdateTradingConfig)
	}
```

- [ ] **Step 5: ビルド確認**

Run: `cd backend && go build ./...`
Expected: エラーなし

- [ ] **Step 6: Commit**

```bash
git add backend/internal/interfaces/api/handler/trading_config.go backend/internal/interfaces/api/router.go
git commit -m "feat: add GET/PUT /api/v1/trading-config with symbol validation"
```

---

## Task 4: Backend — WebSocket 購読の動的切替 + ローソク足 bootstrap

> **Review fix (H2):** `symbolSwitchCh` の `default` ドロップを廃止。古い値を drain してから新しい値を送信する上書き方式にする。
> **Review fix (H3):** シンボル切替時に `bootstrapCandles` を実行し、指標データ不足で HOLD 固定になる問題を防ぐ。
> **Review fix (M2):** Subscribe 失敗時の `continue` が内側ループにしか効かない既存バグを修正。
> **Review fix (M3):** WS の Unsubscribe/Subscribe エラーをログ出力し、Subscribe 失敗時は reconnect する。

**Files:**
- Modify: `backend/cmd/main.go`

- [ ] **Step 1: startMarketRelay を全面書き換え**

`backend/cmd/main.go` の既存 `startMarketRelay` 関数を以下で置き換える:

```go
func startMarketRelay(ctx context.Context, wsClient *rakuten.WSClient, marketDataSvc *usecase.MarketDataService, realtimeHub *usecase.RealtimeHub, initialSymbolID int64, symbolSwitchCh <-chan [2]int64) {
	if wsClient == nil || marketDataSvc == nil {
		return
	}

	currentSymbolID := initialSymbolID
	backoff := wsInitialBackoff

	for {
		select {
		case <-ctx.Done():
			_ = wsClient.Close()
			return
		default:
		}

		msgCh, err := wsClient.Connect(ctx)
		if err != nil {
			slog.Warn("market websocket connect failed", "error", err, "retryIn", backoff)
			waitFor(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		// Subscribe — 失敗時は外側ループで reconnect する（M2 修正）
		subscribeOK := true
		for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
			if err := wsClient.Subscribe(ctx, currentSymbolID, dataType); err != nil {
				slog.Warn("market websocket subscribe failed", "dataType", dataType, "error", err)
				subscribeOK = false
				break
			}
		}
		if !subscribeOK {
			_ = wsClient.Close()
			waitFor(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		slog.Info("market websocket subscribed", "symbolID", currentSymbolID)
		backoff = wsInitialBackoff

		sessionTimer := time.NewTimer(wsMaxSessionDuration)

		reconnect := false
		for !reconnect {
			select {
			case <-ctx.Done():
				sessionTimer.Stop()
				_ = wsClient.Close()
				return
			case <-sessionTimer.C:
				slog.Info("market websocket session approaching 2h limit, reconnecting proactively")
				reconnect = true
			case ids := <-symbolSwitchCh:
				oldID, newID := ids[0], ids[1]
				slog.Info("switching websocket symbol subscription", "from", oldID, "to", newID)

				// Unsubscribe（エラーはログのみ、M3 修正）
				for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
					if err := wsClient.Unsubscribe(ctx, oldID, dataType); err != nil {
						slog.Warn("market websocket unsubscribe failed", "dataType", dataType, "error", err)
					}
				}

				// Subscribe（エラー時は reconnect、M3 修正）
				// C3 修正: Subscribe 成功時のみ currentSymbolID を更新。
				// 失敗時は reconnect で currentSymbolID（=newID）を使って再接続する。
				switchOK := true
				for _, dataType := range []rakuten.DataType{rakuten.DataTypeTicker, rakuten.DataTypeOrderbook, rakuten.DataTypeTrades} {
					if err := wsClient.Subscribe(ctx, newID, dataType); err != nil {
						slog.Error("market websocket re-subscribe failed, will reconnect", "dataType", dataType, "error", err)
						switchOK = false
						break
					}
				}
				// パイプライン側は既に newID に切り替え済みなので、
				// Subscribe 成否に関わらず currentSymbolID を newID にして
				// reconnect 時に新シンボルで再接続する
				currentSymbolID = newID
				if !switchOK {
					reconnect = true
				}
			case raw, ok := <-msgCh:
				if !ok {
					reconnect = true
					break
				}
				handleMarketMessage(ctx, raw, marketDataSvc, realtimeHub)
			}
		}

		sessionTimer.Stop()
		slog.Info("market websocket disconnected, reconnecting")
		_ = wsClient.Close()
		waitFor(ctx, wsInitialBackoff)
	}
}
```

- [ ] **Step 2: main 関数を再構成 — ctx/NewRouter 順序変更 + symbolSwitchCh + bootstrapCandles コールバック**

> **Review fix (C-NEW1):** 現状 `main.go:95` で `NewRouter` を呼び、`main.go:109` で `ctx, cancel` を定義している。`NewRouter` に `OnSymbolSwitch` を渡すためには、`onSymbolSwitch` が参照する `ctx` が先に定義されている必要がある。そのため `ctx, cancel` 定義と `symbolSwitchCh` / `onSymbolSwitch` 定義を `NewRouter` 呼び出しより**前**に移動する。
> **Review fix (H3 ctx):** `bootstrapCandles` には `main` の `ctx` をクロージャでキャプチャして渡す。アプリ終了時にキャンセルされる。

**Step 2a: `ctx, cancel := context.WithCancel(...)` を `pipeline.syncStateInitial(...)` の直後、`api.NewRouter(...)` の前に移動する。**

現状の `main.go:109-110` の以下の行を削除:

```go
	// --- Graceful Shutdown ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
```

そして `pipeline.syncStateInitial(context.Background())`（現 `main.go:92`）の**直後**、`// --- REST API ---` コメントの**前**に配置する:

```go
	// 起動時にポジション・残高を同期
	pipeline.syncStateInitial(context.Background())

	// --- Graceful Shutdown context（onSymbolSwitch と startMarketRelay で使用）---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Symbol Switch channel + callback ---
	symbolSwitchCh := make(chan [2]int64, 1)

	onSymbolSwitch := func(oldID, newID int64) {
		// H3 修正: 新シンボルのローソク足を bootstrap（main の ctx を使う）
		if err := bootstrapCandles(ctx, restClient, marketDataSvc, newID, "15min", "PT15M", 500); err != nil {
			slog.Warn("candle bootstrap for new symbol failed", "symbolID", newID, "error", err)
		}

		// H2 修正: 古い値を drain してから送信（上書き方式）
		// SwitchSymbol は pipeline の switchMu でシリアライズされているため、
		// この関数が並行実行されることはない
		select {
		case <-symbolSwitchCh:
			// 古い値を破棄
		default:
		}
		select {
		case symbolSwitchCh <- [2]int64{oldID, newID}:
		case <-ctx.Done():
		}
	}

	// --- REST API ---
	router := api.NewRouter(api.Dependencies{
		RiskManager:         riskMgr,
		StanceResolver:      stanceResolver,
		IndicatorCalculator: indicatorCalc,
		MarketDataService:   marketDataSvc,
		RealtimeHub:         realtimeHub,
		OrderClient:         restClient,
		OrderExecutor:       orderExecutor,
		Pipeline:            pipeline,
		RESTClient:          restClient,
		ClientOrderRepo:     clientOrderRepo,
		OnSymbolSwitch:      onSymbolSwitch,
	})
```

**Step 2b: `startMarketRelay` の呼び出しを更新:**

`main.go:130` の以下を:

```go
	go startMarketRelay(ctx, wsClient, marketDataSvc, realtimeHub, symbolID)
```

次のように変更:

```go
	go startMarketRelay(ctx, wsClient, marketDataSvc, realtimeHub, symbolID, symbolSwitchCh)
```

**Step 2c: 旧 `ctx, cancel :=` が `sigCh` 前にもう残っていないことを確認する（削除漏れチェック）。**

- [ ] **Step 3: ビルド確認**

Run: `cd backend && go build ./...`
Expected: エラーなし

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/main.go
git commit -m "feat: dynamic WS symbol switching with candle bootstrap and error handling"
```

---

## Task 5: Frontend — TradableSymbol 型と TradingConfig 型の追加

> **Review fix (C3):** TypeScript グローバルの `Symbol`（ES Symbol プリミティブ）と衝突を避けるため `TradableSymbol` にリネーム。

**Files:**
- Modify: `frontend/src/lib/api.ts`

- [ ] **Step 1: 型定義を追加**

`frontend/src/lib/api.ts` の `RiskConfig` 型定義の後（`BotControlResponse` の前）に追加:

```typescript
export type TradableSymbol = {
  id: number
  authority: string
  tradeType: string
  currencyPair: string
  baseCurrency: string
  quoteCurrency: string
  baseScale: number
  quoteScale: number
  baseStepAmount: number
  minOrderAmount: number
  maxOrderAmount: number
  makerTradeFeePercent: number
  takerTradeFeePercent: number
  closeOnly: boolean
  viewOnly: boolean
  enabled: boolean
}

export type TradingConfig = {
  symbolId: number
  tradeAmount: number
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/api.ts
git commit -m "feat: add TradableSymbol and TradingConfig types to frontend API layer"
```

---

## Task 6: Frontend — useSymbols / useTradingConfig hooks

**Files:**
- Create: `frontend/src/hooks/useSymbols.ts`
- Create: `frontend/src/hooks/useTradingConfig.ts`

- [ ] **Step 1: useSymbols フックを作成**

```typescript
// frontend/src/hooks/useSymbols.ts
import { useQuery } from '@tanstack/react-query'
import { fetchApi, type TradableSymbol } from '../lib/api'

export function useSymbols() {
  return useQuery({
    queryKey: ['symbols'],
    queryFn: () => fetchApi<TradableSymbol[]>('/symbols'),
    staleTime: 5 * 60 * 1000, // 銘柄一覧は滅多に変更されないため5分キャッシュ
  })
}
```

- [ ] **Step 2: useTradingConfig フックを作成**

```typescript
// frontend/src/hooks/useTradingConfig.ts
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchApi, sendApi, type TradingConfig } from '../lib/api'

export function useTradingConfig() {
  return useQuery({
    queryKey: ['trading-config'],
    queryFn: () => fetchApi<TradingConfig>('/trading-config'),
  })
}

export function useUpdateTradingConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: TradingConfig) =>
      sendApi<TradingConfig, TradingConfig>('/trading-config', 'PUT', config),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['trading-config'] })
    },
  })
}
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useSymbols.ts frontend/src/hooks/useTradingConfig.ts
git commit -m "feat: add useSymbols and useTradingConfig hooks"
```

---

## Task 7: Frontend — SymbolContext (グローバル symbolId 管理)

> **Review fix (C3):** `Symbol` → `TradableSymbol` リネーム。
> **Review fix (H2 API エラー無限ローディング):** `isError` ケースのフォールバックを追加。
> **Review fix (Codex #3 無効銘柄フォールバック):** trading-config 取得失敗時、ハードコード `DEFAULT_SYMBOL_ID = 7` ではなく、`symbols` から最初の有効銘柄（`enabled && !viewOnly && !closeOnly`）を選ぶ。両方とも取得できない極端なケースのみ 7 に退避。
> **Review fix (M-NEW2 tradeAmount 誤上書き防止):** `tradingConfig` がロード完了する前は `switchSymbol` を実行不可にする。`isSwitchAllowed` フラグを Context で公開し、SymbolSelector 側で `disabled` に反映する。これにより「設定取得失敗時に tradeAmount=1000 を誤って PUT する」問題を防ぐ。
> **Review fix (C2 ticker リセット):** 本ファイルでは `symbolId` state の更新のみ。ticker state のリセットは Task 10 で `useMarketTickerStream.ts` 側に `setTicker(null)` を追加することで対応する。

**Files:**
- Create: `frontend/src/contexts/SymbolContext.tsx`

- [ ] **Step 1: SymbolContext を作成**

```typescript
// frontend/src/contexts/SymbolContext.tsx
import { createContext, useContext, useState, useCallback, useEffect, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTradingConfig, useUpdateTradingConfig } from '../hooks/useTradingConfig'
import { useSymbols } from '../hooks/useSymbols'
import type { TradableSymbol } from '../lib/api'

// 両 API が失敗した極端ケースのみ使う最終退避値
const FALLBACK_SYMBOL_ID = 7

type SymbolContextValue = {
  symbolId: number
  symbols: TradableSymbol[]
  currentSymbol: TradableSymbol | undefined
  switchSymbol: (symbolId: number) => void
  isSwitching: boolean
  // tradingConfig がロード完了するまで false。switchSymbol の実行可否を示す。
  // Codex M-NEW2 対応: 未ロード時に fallback tradeAmount で PUT して誤上書きするのを防ぐ。
  isSwitchAllowed: boolean
}

const SymbolContext = createContext<SymbolContextValue | null>(null)

function pickFirstTradableId(symbols: TradableSymbol[] | undefined): number | null {
  if (!symbols || symbols.length === 0) return null
  const found = symbols.find((s) => s.enabled && !s.viewOnly && !s.closeOnly)
  return found?.id ?? null
}

export function SymbolProvider({ children }: { children: ReactNode }) {
  const {
    data: tradingConfig,
    isLoading: isConfigLoading,
    isError: isConfigError,
  } = useTradingConfig()
  const { data: symbols, isLoading: isSymbolsLoading } = useSymbols()
  const updateConfig = useUpdateTradingConfig()
  const queryClient = useQueryClient()

  const [symbolId, setSymbolId] = useState<number | null>(null)

  // 初期化:
  //   1. tradingConfig 取得成功 → その値を使う
  //   2. tradingConfig 失敗 + symbols あり → symbols から最初の有効銘柄（Codex #3）
  //   3. 両方失敗 → FALLBACK_SYMBOL_ID
  useEffect(() => {
    if (symbolId !== null) return
    if (tradingConfig) {
      setSymbolId(tradingConfig.symbolId)
      return
    }
    if (isConfigError) {
      const fallbackFromSymbols = pickFirstTradableId(symbols)
      if (fallbackFromSymbols !== null) {
        setSymbolId(fallbackFromSymbols)
      } else if (!isSymbolsLoading) {
        // symbols も取れない（エラー or 空配列）— 最終退避
        setSymbolId(FALLBACK_SYMBOL_ID)
      }
    }
  }, [tradingConfig, isConfigError, symbols, isSymbolsLoading, symbolId])

  // tradingConfig がロード完了している時だけ switchSymbol を許可（M-NEW2 対応）
  // これにより「未ロード時にデフォルト tradeAmount を PUT して誤上書きする」問題を防ぐ
  const isSwitchAllowed = tradingConfig !== undefined

  const switchSymbol = useCallback(
    (newSymbolId: number) => {
      if (!tradingConfig) {
        // 未ロード時は何もしない（UI 側でも disabled にしているが、防御的にガード）
        return
      }
      const prevSymbolId = symbolId
      setSymbolId(newSymbolId)
      updateConfig.mutate(
        { symbolId: newSymbolId, tradeAmount: tradingConfig.tradeAmount },
        {
          onSuccess: () => {
            void queryClient.invalidateQueries({ queryKey: ['candles'] })
            void queryClient.invalidateQueries({ queryKey: ['indicators'] })
            void queryClient.invalidateQueries({ queryKey: ['positions'] })
            void queryClient.invalidateQueries({ queryKey: ['trades'] })
            void queryClient.invalidateQueries({ queryKey: ['status'] })
            void queryClient.invalidateQueries({ queryKey: ['pnl'] })
          },
          onError: () => {
            setSymbolId(prevSymbolId)
          },
        },
      )
    },
    [symbolId, tradingConfig, updateConfig, queryClient],
  )

  // 初期化完了までは Loading 表示
  if (symbolId === null) {
    if (isConfigLoading || isSymbolsLoading) {
      return (
        <div className="flex min-h-screen items-center justify-center">
          <p className="text-sm text-text-secondary">Loading...</p>
        </div>
      )
    }
    // 両 API 失敗直後、useEffect が走る前の1フレーム
    return (
      <div className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-text-secondary">Loading...</p>
      </div>
    )
  }

  // symbols は取得失敗時も空配列で動作継続（SymbolSelector 側で空時は非表示）
  const safeSymbols = symbols ?? []
  const currentSymbol = safeSymbols.find((s) => s.id === symbolId)

  return (
    <SymbolContext.Provider
      value={{
        symbolId,
        symbols: safeSymbols,
        currentSymbol,
        switchSymbol,
        isSwitching: updateConfig.isPending,
        isSwitchAllowed,
      }}
    >
      {children}
    </SymbolContext.Provider>
  )
}

export function useSymbolContext() {
  const ctx = useContext(SymbolContext)
  if (!ctx) throw new Error('useSymbolContext must be used within SymbolProvider')
  return ctx
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/contexts/SymbolContext.tsx
git commit -m "feat: add SymbolContext for global symbol state management"
```

---

## Task 8: Frontend — SymbolSelector コンポーネント

**Files:**
- Create: `frontend/src/components/SymbolSelector.tsx`

- [ ] **Step 1: SymbolSelector を作成**

```typescript
// frontend/src/components/SymbolSelector.tsx
import { useSymbolContext } from '../contexts/SymbolContext'

export function SymbolSelector() {
  const { symbolId, symbols, switchSymbol, isSwitching, isSwitchAllowed } = useSymbolContext()

  const tradableSymbols = symbols.filter((s) => s.enabled && !s.viewOnly && !s.closeOnly)

  if (tradableSymbols.length === 0) {
    return null
  }

  // isSwitchAllowed=false は tradingConfig 未ロード時（Review fix M-NEW2）
  // → 誤って fallback tradeAmount で PUT しないよう disabled にする
  const disabled = isSwitching || !isSwitchAllowed

  return (
    <div className="flex items-center gap-2">
      <label
        htmlFor="symbol-select"
        className="text-xs uppercase tracking-[0.18em] text-text-secondary"
      >
        銘柄
      </label>
      <select
        id="symbol-select"
        value={symbolId}
        onChange={(e) => switchSymbol(Number(e.target.value))}
        disabled={disabled}
        className="rounded-full border border-white/10 bg-white/6 px-4 py-2 text-sm font-medium text-white outline-none transition focus:border-cyan-200 disabled:opacity-50"
      >
        {tradableSymbols.map((s) => (
          <option key={s.id} value={s.id} className="bg-bg-card text-white">
            {s.currencyPair.replace('_', '/')}
          </option>
        ))}
      </select>
      {isSwitching && (
        <span className="text-xs text-cyan-200">切替中...</span>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/SymbolSelector.tsx
git commit -m "feat: add SymbolSelector dropdown component"
```

---

## Task 9: Frontend — SymbolProvider をルートに配置 + AppFrame にセレクタ追加

**Files:**
- Modify: `frontend/src/routes/__root.tsx`
- Modify: `frontend/src/components/AppFrame.tsx`

- [ ] **Step 1: __root.tsx を読んで現状を確認**

Run: `cat frontend/src/routes/__root.tsx`

- [ ] **Step 2: __root.tsx に SymbolProvider をラップ**

`frontend/src/routes/__root.tsx` のルートコンポーネント内で、`<Outlet />` を `<SymbolProvider>` でラップする。

import を追加:
```typescript
import { SymbolProvider } from '../contexts/SymbolContext'
```

`<Outlet />` を以下に変更:
```tsx
<SymbolProvider>
  <Outlet />
</SymbolProvider>
```

- [ ] **Step 3: AppFrame にセレクタを配置**

`frontend/src/components/AppFrame.tsx` を変更:

import を追加:
```typescript
import { SymbolSelector } from './SymbolSelector'
```

ヘッダー部分の `<nav>` の前に `SymbolSelector` を追加:

```tsx
<div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
  <div>
    <p className="text-[0.7rem] uppercase tracking-[0.35em] text-cyan-200/70">Rakuten CFD Bot</p>
    <h1 className="mt-2 text-3xl font-semibold tracking-tight text-white sm:text-4xl">{title}</h1>
    <p className="mt-2 max-w-2xl text-sm text-slate-300">{subtitle}</p>
  </div>
  <div className="flex flex-col items-end gap-3">
    <SymbolSelector />
    <nav className="flex flex-wrap gap-2">
      {navItems.map((item) => (
        <Link
          key={item.to}
          to={item.to}
          activeProps={{ className: 'bg-white text-slate-950 shadow-lg' }}
          inactiveProps={{ className: 'bg-white/8 text-slate-200 hover:bg-white/14' }}
          className="rounded-full px-4 py-2 text-sm font-medium transition"
        >
          {item.label}
        </Link>
      ))}
    </nav>
  </div>
</div>
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/routes/__root.tsx frontend/src/components/AppFrame.tsx
git commit -m "feat: integrate SymbolProvider and SymbolSelector into app shell"
```

---

## Task 10: Frontend — ハードコード symbolId=7 をすべて Context 経由に変更 + ticker リセット

> **Review fix (C2):** `useMarketTickerStream` の `useEffect` 冒頭に `setTicker(null)` を追加し、`symbolId` が変わった瞬間に ticker local state をクリアする。これにより「新ペア名 + 旧ペア価格」の一時表示を防ぐ。

**Files:**
- Modify: `frontend/src/hooks/useMarketTickerStream.ts`
- Modify: `frontend/src/routes/index.tsx`
- Modify: `frontend/src/routes/settings.tsx`
- Modify: `frontend/src/routes/history.tsx`
- Modify: `frontend/src/components/LiveTickerCard.tsx`

- [ ] **Step 0: useMarketTickerStream.ts に ticker リセットを追加 (C2 修正)**

`frontend/src/hooks/useMarketTickerStream.ts` の `useEffect` 冒頭（`let active = true` の前）に `setTicker(null)` を追加:

```typescript
  useEffect(() => {
    // symbolId 変更時に旧シンボルの価格が残らないよう、即座にリセット
    setTicker(null)

    let active = true
    let socket: WebSocket | null = null
    let retryTimer: ReturnType<typeof setTimeout> | null = null

    const connect = () => {
      // ... (既存コードそのまま)
```

`useEffect` の依存配列は既に `[queryClient, symbolId]` なので、`symbolId` 変更時にこのコードが再実行される。

- [ ] **Step 1: index.tsx を更新**

`frontend/src/routes/index.tsx` の `Dashboard` 関数を変更:

import を追加:
```typescript
import { useSymbolContext } from '../contexts/SymbolContext'
```

関数冒頭に追加:
```typescript
const { symbolId } = useSymbolContext()
```

ハードコード `7` をすべて `symbolId` に置き換える:
```typescript
const { data: indicators } = useIndicators(symbolId)
const { data: candles } = useCandles(symbolId)
const { data: positions } = usePositions(symbolId)
const { ticker, connectionState } = useMarketTickerStream(symbolId)
```

- [ ] **Step 2: settings.tsx を更新**

`frontend/src/routes/settings.tsx` の `SettingsPage` 関数を変更:

import を追加:
```typescript
import { useSymbolContext } from '../contexts/SymbolContext'
```

関数冒頭に追加:
```typescript
const { symbolId } = useSymbolContext()
```

`useMarketTickerStream(7)` を `useMarketTickerStream(symbolId)` に変更。

- [ ] **Step 3: history.tsx を更新**

`frontend/src/routes/history.tsx` の `HistoryPage` 関数を変更:

import を追加:
```typescript
import { useSymbolContext } from '../contexts/SymbolContext'
```

関数冒頭に追加:
```typescript
const { symbolId } = useSymbolContext()
```

以下を変更:
```typescript
useMarketTickerStream(symbolId)
const { data: trades } = useTradeHistory(symbolId)
```

- [ ] **Step 4: LiveTickerCard.tsx を更新**

`frontend/src/components/LiveTickerCard.tsx` を変更:

props に `currencyPair` を追加:
```typescript
type LiveTickerCardProps = {
  ticker: LiveTicker | null
  connectionState: 'connecting' | 'connected' | 'disconnected'
  currencyPair?: string
}

export function LiveTickerCard({ ticker, connectionState, currencyPair }: LiveTickerCardProps) {
```

ハードコード `"BTC/JPY ライブ価格"` を動的に:
```tsx
<h2 className="mt-2 text-xl font-semibold text-white">{currencyPair ?? 'BTC/JPY'} ライブ価格</h2>
```

`frontend/src/routes/index.tsx` の `LiveTickerCard` 呼び出しを更新:

import を追加（既に Step 1 で追加済み）:
```typescript
import { useSymbolContext } from '../contexts/SymbolContext'
```

props に追加:
```tsx
<LiveTickerCard
  ticker={ticker}
  connectionState={connectionState}
  currencyPair={currentSymbol?.currencyPair?.replace('_', '/')}
/>
```

`useSymbolContext` の destructure に `currentSymbol` を追加:
```typescript
const { symbolId, currentSymbol } = useSymbolContext()
```

- [ ] **Step 5: ビルド確認**

Run: `cd frontend && npx tsc --noEmit`
Expected: エラーなし

- [ ] **Step 6: Commit**

```bash
git add frontend/src/hooks/useMarketTickerStream.ts frontend/src/routes/index.tsx frontend/src/routes/settings.tsx frontend/src/routes/history.tsx frontend/src/components/LiveTickerCard.tsx
git commit -m "feat: replace hardcoded symbolId=7 with SymbolContext and reset ticker on switch"
```

---

## Task 11: 動作確認

- [ ] **Step 1: バックエンド起動確認**

Run: `cd backend && go build -o main ./cmd && ./main`
Expected: サーバーが起動する

- [ ] **Step 2: 銘柄一覧 API 確認 + 切替先 ID を決定**

Run: `curl -s localhost:8080/api/v1/symbols | python3 -m json.tool | head -60`
Expected: Symbol 配列が返る（id, currencyPair, enabled 等のフィールド）

**重要 (m3 対応):** このレスポンスから **現在の symbolId 以外で `enabled=true && viewOnly=false && closeOnly=false` の銘柄 ID を1つ選ぶ**。以降の Step 4 ではその ID を使う。

例: ETH_JPY の id が 8 なら Step 4 で `symbolId:8` を指定する。以下シェル変数 `TARGET_SYMBOL_ID` に入れておくと便利:

```bash
# 現在の symbolId を取得
CURRENT_SYMBOL_ID=$(curl -s localhost:8080/api/v1/trading-config | python3 -c 'import sys, json; print(json.load(sys.stdin)["symbolId"])')
# 現在以外で取引可能な最初の symbol id を取得
TARGET_SYMBOL_ID=$(curl -s localhost:8080/api/v1/symbols | python3 -c "
import sys, json
current = $CURRENT_SYMBOL_ID
for s in json.load(sys.stdin):
    if s['id'] != current and s['enabled'] and not s['viewOnly'] and not s['closeOnly']:
        print(s['id']); break
")
echo "TARGET_SYMBOL_ID=$TARGET_SYMBOL_ID"
```

- [ ] **Step 3: trading-config API 確認**

Run: `curl -s localhost:8080/api/v1/trading-config | python3 -m json.tool`
Expected: 200 で `symbolId` と `tradeAmount` を含むオブジェクトが返る（具体的な値は起動時の環境変数次第）

- [ ] **Step 4: trading-config 切替確認（Step 2 で決めた ID を使う）**

Run: `curl -s -X PUT localhost:8080/api/v1/trading-config -H 'Content-Type: application/json' -d "{\"symbolId\":$TARGET_SYMBOL_ID,\"tradeAmount\":1000}" | python3 -m json.tool`
Expected: 200 で `{"symbolId": <TARGET_SYMBOL_ID>, "tradeAmount": 1000}` が返る

- [ ] **Step 5: 無効シンボルのバリデーション確認 (M1)**

Run: `curl -s -X PUT localhost:8080/api/v1/trading-config -H 'Content-Type: application/json' -d '{"symbolId":99999,"tradeAmount":1000}'`
Expected: `{"error": "unknown symbolId"}` で 400 が返る

- [ ] **Step 6: tradeAmount バリデーション確認 (M-amount)**

Run: `curl -s -X PUT localhost:8080/api/v1/trading-config -H 'Content-Type: application/json' -d "{\"symbolId\":$CURRENT_SYMBOL_ID,\"tradeAmount\":0}"`
Expected: `{"error": "tradeAmount must be positive"}` で 400 が返る

- [ ] **Step 7: フロントエンド起動確認**

Run: `cd frontend && npm run dev`
Expected: localhost:3000 で画面が表示される

- [ ] **Step 8: 画面上で銘柄セレクタを操作**

ブラウザで localhost:3000 を開き、ヘッダーの銘柄セレクタから別の通貨ペアを選択。
Expected: ティッカー、チャート、インジケーター、ポジションの表示が選択した銘柄に切り替わる。切替直後にティッカー値は「Loading...」状態になり、旧ペアの価格は表示されない。

- [ ] **Step 9: Start/Stop と切替の並行呼び出しを確認（Codex #1/#2 対応）**

> **目的:** ユニットテスト（Task 2 Step 9）で検証済みの並行性を、実際の統合環境でも手動で再確認する。

`/api/v1/start` でパイプラインを開始した直後に、連続して銘柄切替を叩く:

```bash
curl -X POST localhost:8080/api/v1/start
curl -X PUT localhost:8080/api/v1/trading-config -H 'Content-Type: application/json' -d "{\"symbolId\":$TARGET_SYMBOL_ID,\"tradeAmount\":1000}" &
curl -X POST localhost:8080/api/v1/stop &
wait
curl -s localhost:8080/api/v1/status
```

Expected:
- サーバーログに panic / deadlock の気配がない
- 最終的な `status` が `running=false`（Stop が効いている。Codex #1 対応）
- ログに `trading pipeline stopped` が記録されている

もし `running=true` になっている場合は Codex #1 の不具合が残っているので Task 2 の実装を確認する。
