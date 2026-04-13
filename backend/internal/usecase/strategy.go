package usecase

import (
	"context"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// StrategyEngine はテクニカル指標とスタンスリゾルバの戦略方針を統合して売買シグナルを生成する。
type StrategyEngine struct {
	stanceResolver StanceResolver
}

func NewStrategyEngine(stanceResolver StanceResolver) *StrategyEngine {
	return &StrategyEngine{
		stanceResolver: stanceResolver,
	}
}

// Evaluate はテクニカル指標と現在価格から売買シグナルを生成する。
// 指標データが不足している場合はHOLDを返す。
func (e *StrategyEngine) Evaluate(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) (*entity.Signal, error) {
	// 指標チェックを先に行い、不要な処理を防ぐ
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
		return e.evaluateTrendFollow(indicators.SymbolID, sma20, sma50, rsi, indicators.Histogram), nil
	case entity.MarketStanceContrarian:
		return e.evaluateContrarian(indicators.SymbolID, rsi, indicators.Histogram), nil
	default:
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "stance is HOLD",
			Timestamp: time.Now().Unix(),
		}, nil
	}
}

func (e *StrategyEngine) evaluateTrendFollow(symbolID int64, sma20, sma50, rsi float64, histogram *float64) *entity.Signal {
	now := time.Now().Unix()

	if sma20 > sma50 && rsi < 70 {
		// MACD histogram confirmation: skip buy if momentum is negative
		if histogram != nil && *histogram < 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "trend follow: MACD histogram negative, skipping buy",
				Timestamp: now,
			}
		}
		reason := "trend follow: SMA20 > SMA50, RSI not overbought"
		if histogram != nil {
			reason += ", MACD confirmed"
		}
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionBuy,
			Reason:    reason,
			Timestamp: now,
		}
	}
	if sma20 < sma50 && rsi > 30 {
		// MACD histogram confirmation: skip sell if momentum is positive
		if histogram != nil && *histogram > 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "trend follow: MACD histogram positive, skipping sell",
				Timestamp: now,
			}
		}
		reason := "trend follow: SMA20 < SMA50, RSI not oversold"
		if histogram != nil {
			reason += ", MACD confirmed"
		}
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionSell,
			Reason:    reason,
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

func (e *StrategyEngine) evaluateContrarian(symbolID int64, rsi float64, histogram *float64) *entity.Signal {
	now := time.Now().Unix()

	if rsi < 30 {
		// Skip contrarian buy if MACD momentum is still strongly negative
		if histogram != nil && *histogram < -10 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "contrarian: RSI oversold but MACD momentum still strongly negative",
				Timestamp: now,
			}
		}
		reason := "contrarian: RSI oversold, expecting bounce"
		if histogram != nil {
			reason += ", MACD not strongly against"
		}
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionBuy,
			Reason:    reason,
			Timestamp: now,
		}
	}
	if rsi > 70 {
		// Skip contrarian sell if MACD momentum is still strongly positive
		if histogram != nil && *histogram > 10 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "contrarian: RSI overbought but MACD momentum still strongly positive",
				Timestamp: now,
			}
		}
		reason := "contrarian: RSI overbought, expecting pullback"
		if histogram != nil {
			reason += ", MACD not strongly against"
		}
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionSell,
			Reason:    reason,
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
