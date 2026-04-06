package main

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// TradingPipeline は自動売買パイプラインを管理する。
// POST /start で Start、POST /stop で Stop を呼ぶ。
type TradingPipeline struct {
	mu     sync.Mutex
	cancel context.CancelFunc

	symbolID    int64
	interval    time.Duration
	tradeAmount float64

	restClient       repository.OrderClient
	marketDataSvc    *usecase.MarketDataService
	indicatorCalc    *usecase.IndicatorCalculator
	strategyEngine   *usecase.StrategyEngine
	orderExecutor    *usecase.OrderExecutor
	riskMgr          *usecase.RiskManager
	tradeHistoryRepo *database.TradeHistoryRepo
	riskStateRepo    *database.RiskStateRepo
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
	tradeHistoryRepo *database.TradeHistoryRepo,
	riskStateRepo *database.RiskStateRepo,
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
func (p *TradingPipeline) Start() {
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
func (p *TradingPipeline) Stop() {
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
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancel != nil
}

// runTradingLoop は一定間隔で指標計算→戦略判定→注文実行を行う。
func (p *TradingPipeline) runTradingLoop(ctx context.Context) {
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
	// 1. 最新ティッカーを取得
	latestTicker, err := p.marketDataSvc.GetLatestTicker(ctx, p.symbolID)
	if err != nil || latestTicker == nil {
		slog.Warn("pipeline: failed to get latest ticker", "error", err)
		return
	}

	// 2. テクニカル指標を計算
	indicators, err := p.indicatorCalc.Calculate(ctx, p.symbolID, "15min")
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
	positions, err := p.restClient.GetPositions(ctx, p.symbolID)
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

	amount := p.tradeAmount / price
	// 楽天の最小注文単位に丸める（BTC_JPY は 0.0001 BTC）
	amount = math.Floor(amount*10000) / 10000
	if amount <= 0 {
		slog.Warn("pipeline: calculated amount is 0, skip", "tradeAmount", p.tradeAmount, "price", price)
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
		p.recordTrade(ctx, p.symbolID, result.OrderID, string(side), "open", price, amount, signal.Reason, false)
		p.syncState(ctx)
		p.persistRiskState(ctx)
	} else {
		slog.Info("pipeline: order not executed", "reason", result.Reason)
	}
}

// runStopLossMonitor は Ticker を監視し、損切り条件に達したポジションを即時決済する。
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
			if t.SymbolID != p.symbolID {
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

func (p *TradingPipeline) recordTrade(ctx context.Context, symbolID, orderID int64, side, action string, price, amount float64, reason string, isStopLoss bool) {
	if p.tradeHistoryRepo == nil {
		return
	}
	if err := p.tradeHistoryRepo.Save(ctx, database.TradeRecord{
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
	if err := p.riskStateRepo.Save(ctx, database.RiskState{
		DailyLoss: status.DailyLoss,
		Balance:   status.Balance,
	}); err != nil {
		slog.Error("pipeline: failed to persist risk state", "error", err)
	}
}

// syncState は楽天APIから現在のポジション・残高を取得し、RiskManagerに反映する。
func (p *TradingPipeline) syncState(ctx context.Context) {
	positions, err := p.restClient.GetPositions(ctx, p.symbolID)
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
func restoreRiskState(ctx context.Context, repo *database.RiskStateRepo, riskMgr *usecase.RiskManager) {
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
