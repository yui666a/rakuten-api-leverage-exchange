package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// newAgentClientOrderID は pipeline (自動売買エージェント) からの注文に使う
// clientOrderId を採番する。形式は "agent-<intent>-<unix>-<rand8>"。
//
// rand を使うのは、同一秒内に複数注文が走った場合の衝突回避のため。
// pipeline は単一プロセスで動く想定だが、stop-loss と open が同時刻に走るケースが
// ありうるので intent を区別子として埋める。
func newAgentClientOrderID(intent string) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand 失敗時は時刻ナノ秒で代替 (極めてまれ)
		return fmt.Sprintf("agent-%s-%d", intent, time.Now().UnixNano())
	}
	return fmt.Sprintf("agent-%s-%d-%s", intent, time.Now().Unix(), hex.EncodeToString(b[:]))
}

// TradingPipeline は自動売買パイプラインを管理する。
// POST /start で Start、POST /stop で Stop を呼ぶ。
//
// Locking strategy:
//   - switchMu: SwitchSymbol / Start / Stop を直列化する（これら3つが並行すると
//     停止要求の握りつぶしや bootstrap 完了前の再開が発生するため）。
//   - mu: symbolID / tradeAmount / cancel のフィールドアクセスを保護する。
//     読み取りは snapshot() 経由でスナップショットを取る。
//
// ロック順序の原則: 必ず switchMu → mu の順で取得する。逆順で取るパスを作らないこと。
type TradingPipeline struct {
	switchMu sync.Mutex   // SwitchSymbol / Start / Stop を直列化
	mu       sync.RWMutex // フィールド保護（snapshot 経由で読む）
	cancel   context.CancelFunc

	symbolID    int64
	interval    time.Duration
	tradeAmount float64

	restClient       repository.OrderClient
	marketDataSvc    *usecase.MarketDataService
	indicatorCalc    *usecase.IndicatorCalculator
	strategyEngine   *usecase.StrategyEngine
	orderExecutor    *usecase.OrderExecutor
	riskMgr          *usecase.RiskManager
	tradeHistoryRepo repository.TradeHistoryRepository
	riskStateRepo    repository.RiskStateRepository
}

// tradingSnapshot は evaluate / stopLoss ループで使う、ロック下にコピーしたフィールド束。
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

// TradingPipelineConfig はパイプラインの設定。
type TradingPipelineConfig struct {
	SymbolID    int64
	Interval    time.Duration
	TradeAmount float64
}

func NewTradingPipeline(
	cfg TradingPipelineConfig,
	restClient repository.OrderClient,
	marketDataSvc *usecase.MarketDataService,
	indicatorCalc *usecase.IndicatorCalculator,
	strategyEngine *usecase.StrategyEngine,
	orderExecutor *usecase.OrderExecutor,
	riskMgr *usecase.RiskManager,
	tradeHistoryRepo repository.TradeHistoryRepository,
	riskStateRepo repository.RiskStateRepository,
) *TradingPipeline {
	return &TradingPipeline{
		symbolID:         cfg.SymbolID,
		interval:         cfg.Interval,
		tradeAmount:      cfg.TradeAmount,
		restClient:       restClient,
		marketDataSvc:    marketDataSvc,
		indicatorCalc:    indicatorCalc,
		strategyEngine:   strategyEngine,
		orderExecutor:    orderExecutor,
		riskMgr:          riskMgr,
		tradeHistoryRepo: tradeHistoryRepo,
		riskStateRepo:    riskStateRepo,
	}
}

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

// Running はパイプラインが実行中かどうかを返す。
func (p *TradingPipeline) Running() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cancel != nil
}

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

// SwitchSymbol は取引対象シンボルを切り替える。
// switchMu でシリアライズすることで:
//   - 連続切替の順序保証（逆順適用を防ぐ）
//   - SwitchSymbol 実行中の Start/Stop 割込みを防ぐ
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

// runTradingLoop は一定間隔で指標計算→戦略判定→注文実行を行う。
func (p *TradingPipeline) runTradingLoop(ctx context.Context) {
	// テスト時に依存が nil の場合は評価ループを回さない（ロック挙動のみ検証する用途）
	if p.marketDataSvc == nil || p.indicatorCalc == nil || p.strategyEngine == nil || p.restClient == nil || p.orderExecutor == nil {
		<-ctx.Done()
		return
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// 起動直後に1回実行
	p.evaluate(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.evaluate(ctx)
		}
	}
}

func (p *TradingPipeline) evaluate(ctx context.Context) {
	snap := p.snapshot()

	// 1. 最新ティッカーを取得
	latestTicker, err := p.marketDataSvc.GetLatestTicker(ctx, snap.symbolID)
	if err != nil || latestTicker == nil {
		slog.Warn("pipeline: failed to get latest ticker", "error", err)
		return
	}

	// 2. テクニカル指標を計算
	indicators, err := p.indicatorCalc.Calculate(ctx, snap.symbolID, "PT15M")
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
	// 楽天の最小注文単位に丸める（BTC_JPY は 0.0001 BTC）
	amount = math.Floor(amount*10000) / 10000
	if amount <= 0 {
		slog.Warn("pipeline: calculated amount is 0, skip", "tradeAmount", snap.tradeAmount, "price", price)
		return
	}

	// 6. 注文実行
	clientOrderID := newAgentClientOrderID("open")
	result, err := p.orderExecutor.ExecuteSignal(ctx, clientOrderID, *signal, price, amount)
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

// runStopLossMonitor は Ticker を監視し、損切り条件に達したポジションを即時決済する。
func (p *TradingPipeline) runStopLossMonitor(ctx context.Context) {
	// テスト時に依存が nil の場合は監視ループを回さない
	if p.marketDataSvc == nil || p.riskMgr == nil || p.orderExecutor == nil {
		<-ctx.Done()
		return
	}
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

				clientOrderID := newAgentClientOrderID("stoploss")
				result, err := p.orderExecutor.ClosePosition(ctx, clientOrderID, pos, t.Last)
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

func (p *TradingPipeline) recordTrade(ctx context.Context, symbolID, orderID int64, side, action string, price, amount float64, reason string, isStopLoss bool) {
	if p.tradeHistoryRepo == nil {
		return
	}
	if err := p.tradeHistoryRepo.Save(ctx, repository.TradeRecord{
		SymbolID:   symbolID,
		OrderID:    orderID,
		Side:       side,
		Action:     action,
		Price:      price,
		Amount:     amount,
		Reason:     reason,
		IsStopLoss: isStopLoss,
	}); err != nil {
		slog.Error("pipeline: failed to save trade history", "error", err)
	}
}

func (p *TradingPipeline) persistRiskState(ctx context.Context) {
	if p.riskStateRepo == nil {
		return
	}
	status := p.riskMgr.GetStatus()
	if err := p.riskStateRepo.Save(ctx, repository.RiskState{
		DailyLoss: status.DailyLoss,
		Balance:   status.Balance,
	}); err != nil {
		slog.Error("pipeline: failed to persist risk state", "error", err)
	}
}

// syncState は楽天APIから現在のポジション・残高を取得し、RiskManagerに反映する。
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

// syncStateInitial は起動時にポジション・残高を同期する。
func (p *TradingPipeline) syncStateInitial(ctx context.Context) {
	p.syncState(ctx)
	slog.Info("initial state sync completed")
}

// restoreRiskState は保存されたリスク状態をRiskManagerに復元する。
// 日付が変わっている場合、dailyLoss はリセットする。
func restoreRiskState(ctx context.Context, repo repository.RiskStateRepository, riskMgr *usecase.RiskManager) {
	if repo == nil {
		return
	}
	state, err := repo.Load(ctx)
	if err != nil {
		slog.Error("failed to load risk state", "error", err)
		return
	}
	if state == nil {
		slog.Info("no saved risk state found")
		return
	}

	jst := time.FixedZone("JST", 9*60*60)
	savedDate := time.Unix(state.UpdatedAt, 0).In(jst).Truncate(24 * time.Hour)
	today := time.Now().In(jst).Truncate(24 * time.Hour)

	if savedDate.Before(today) {
		slog.Info("risk state from previous day, resetting daily loss", "savedDate", savedDate.Format("2006-01-02"))
		state.DailyLoss = 0
	}

	riskMgr.UpdateBalance(state.Balance)
	if state.DailyLoss > 0 {
		riskMgr.RecordLoss(state.DailyLoss)
	}
	slog.Info("risk state restored", "balance", state.Balance, "dailyLoss", state.DailyLoss)
}

// startDailyLossReset は毎日0時(JST)に日次損失をリセットするgoroutineを起動する。
func startDailyLossReset(ctx context.Context, riskMgr *usecase.RiskManager) {
	jst := time.FixedZone("JST", 9*60*60)

	for {
		now := time.Now().In(jst)
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, jst)
		untilMidnight := nextMidnight.Sub(now)

		slog.Info("daily loss reset scheduled", "in", untilMidnight.Truncate(time.Second))

		select {
		case <-ctx.Done():
			return
		case <-time.After(untilMidnight):
			riskMgr.ResetDailyLoss()
			slog.Info("daily loss reset completed")
		}
	}
}
