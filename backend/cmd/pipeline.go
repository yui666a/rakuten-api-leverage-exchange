package main

import (
	"context"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
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

	restClient      repository.OrderClient
	marketDataSvc   *usecase.MarketDataService
	indicatorCalc   *usecase.IndicatorCalculator
	strategyEngine  *usecase.StrategyEngine
	orderExecutor   *usecase.OrderExecutor
	riskMgr         *usecase.RiskManager
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
) *TradingPipeline {
	return &TradingPipeline{
		symbolID:       cfg.SymbolID,
		interval:       cfg.Interval,
		tradeAmount:    cfg.TradeAmount,
		restClient:     restClient,
		marketDataSvc:  marketDataSvc,
		indicatorCalc:  indicatorCalc,
		strategyEngine: strategyEngine,
		orderExecutor:  orderExecutor,
		riskMgr:        riskMgr,
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

	log.Println("trading pipeline started")
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

	log.Println("trading pipeline stopped")
}

// Running はパイプラインが実行中かどうかを返す��
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
		log.Printf("pipeline: failed to get latest ticker: %v", err)
		return
	}

	// 2. テクニカル指標を計算
	indicators, err := p.indicatorCalc.Calculate(ctx, p.symbolID, "15min")
	if err != nil {
		log.Printf("pipeline: failed to calculate indicators: %v", err)
		return
	}

	// 3. 戦略判定
	signal, err := p.strategyEngine.Evaluate(ctx, *indicators, latestTicker.Last)
	if err != nil {
		log.Printf("pipeline: failed to evaluate strategy: %v", err)
		return
	}

	log.Printf("pipeline: signal=%s reason=%q price=%.2f", signal.Action, signal.Reason, latestTicker.Last)

	if signal.Action == entity.SignalActionHold {
		return
	}

	// 4. 同一方向のポジションを保持中ならスキップ
	positions, err := p.restClient.GetPositions(ctx, p.symbolID)
	if err != nil {
		log.Printf("pipeline: failed to get positions: %v", err)
		return
	}

	side := entity.OrderSideBuy
	if signal.Action == entity.SignalActionSell {
		side = entity.OrderSideSell
	}

	for _, pos := range positions {
		if pos.OrderSide == side && pos.RemainingAmount > 0 {
			log.Printf("pipeline: skip %s, already holding %s position (id=%d)", signal.Action, side, pos.ID)
			return
		}
	}

	// 5. 注文数量を計算
	price := latestTicker.BestAsk
	if signal.Action == entity.SignalActionSell {
		price = latestTicker.BestBid
	}
	if price <= 0 {
		log.Printf("pipeline: invalid price %.2f, skip", price)
		return
	}

	amount := p.tradeAmount / price
	// 楽天の最小注文単位に丸める（BTC_JPY は 0.0001 BTC）
	amount = math.Floor(amount*10000) / 10000
	if amount <= 0 {
		log.Printf("pipeline: calculated amount is 0, skip (tradeAmount=%.0f price=%.2f)", p.tradeAmount, price)
		return
	}

	// 6. 注文実行
	result, err := p.orderExecutor.ExecuteSignal(ctx, *signal, price, amount)
	if err != nil {
		log.Printf("pipeline: order execution failed: %v", err)
		return
	}

	if result.Executed {
		log.Printf("pipeline: order executed: orderId=%d side=%s amount=%.4f price=%.2f", result.OrderID, side, amount, price)
		// ポジション・残高を同期
		p.syncState(ctx)
	} else {
		log.Printf("pipeline: order not executed: %s", result.Reason)
	}
}

// runStopLossMonitor は Ticker を監視し、損切り条件に達したポジションを即時決済す��。
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
				log.Printf("pipeline: stop-loss triggered for position %d (side=%s price=%.2f current=%.2f)",
					pos.ID, pos.OrderSide, pos.Price, t.Last)

				result, err := p.orderExecutor.ClosePosition(ctx, pos, t.Last)
				if err != nil {
					log.Printf("pipeline: stop-loss close failed: %v", err)
					continue
				}
				if result.Executed {
					log.Printf("pipeline: stop-loss closed: orderId=%d", result.OrderID)
					// 損失を記録
					loss := math.Abs(pos.FloatingProfit)
					p.riskMgr.RecordLoss(loss)
				}
			}
		}
	}
}

// syncState は楽天APIから現在のポジション・残高を取得し、RiskManagerに反映する。
func (p *TradingPipeline) syncState(ctx context.Context) {
	positions, err := p.restClient.GetPositions(ctx, p.symbolID)
	if err != nil {
		log.Printf("pipeline: failed to sync positions: %v", err)
	} else {
		p.riskMgr.UpdatePositions(positions)
	}

	assets, err := p.restClient.(interface {
		GetAssets(ctx context.Context) ([]entity.Asset, error)
	}).GetAssets(ctx)
	if err != nil {
		log.Printf("pipeline: failed to sync assets: %v", err)
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
// main.go から呼ばれる。
func (p *TradingPipeline) syncStateInitial(ctx context.Context) {
	p.syncState(ctx)
	log.Println("initial state sync completed")
}

// startDailyLossReset は毎日0時(JST)に日次損失をリセットするgoroutineを起動する。
func startDailyLossReset(ctx context.Context, riskMgr *usecase.RiskManager) {
	jst := time.FixedZone("JST", 9*60*60)

	for {
		now := time.Now().In(jst)
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, jst)
		untilMidnight := nextMidnight.Sub(now)

		log.Printf("daily loss reset scheduled in %s", untilMidnight.Truncate(time.Second))

		select {
		case <-ctx.Done():
			return
		case <-time.After(untilMidnight):
			riskMgr.ResetDailyLoss()
			log.Println("daily loss reset completed")
		}
	}
}
