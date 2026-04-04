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
