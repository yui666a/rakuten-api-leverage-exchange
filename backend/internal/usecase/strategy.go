package usecase

import (
	"context"
	"math"
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

// EvaluateWithHigherTF はマルチタイムフレーム分析付きでシグナルを生成する。
// higherTFがnon-nilの場合、Trend Followシグナルが上位トレンドに逆行していればHOLDにフィルタする。
// 上位トレンドと一致している場合はconfidenceを10%ブーストする。
// Contrarianシグナルは意図的に逆張りなのでフィルタしない。
// ボラティリティフィルター: BBバンド幅が非常に狭い(squeeze)場合、Trend Followシグナルを抑制する。
func (e *StrategyEngine) EvaluateWithHigherTF(ctx context.Context, indicators entity.IndicatorSet, higherTF *entity.IndicatorSet, lastPrice float64) (*entity.Signal, error) {
	signal, err := e.Evaluate(ctx, indicators, lastPrice)
	if err != nil || signal.Action == entity.SignalActionHold {
		return signal, err
	}

	result := e.stanceResolver.Resolve(ctx, indicators)

	// Volatility filter: squeeze detection for trend-follow signals
	// BBBandwidth < 0.02 (2%) indicates very low volatility / consolidation
	if result.Stance == entity.MarketStanceTrendFollow && indicators.BBBandwidth != nil && *indicators.BBBandwidth < 0.02 {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "volatility filter: Bollinger squeeze, trend signal unreliable",
			Timestamp: signal.Timestamp,
		}, nil
	}

	// BB position can boost/penalize confidence for contrarian
	if result.Stance == entity.MarketStanceContrarian && indicators.BBLower != nil && indicators.BBUpper != nil {
		if signal.Action == entity.SignalActionBuy && lastPrice <= *indicators.BBLower {
			// Price at/below lower band: extra confidence for buy
			signal.Confidence = math.Min(signal.Confidence+0.1, 1.0)
		} else if signal.Action == entity.SignalActionSell && lastPrice >= *indicators.BBUpper {
			// Price at/above upper band: extra confidence for sell
			signal.Confidence = math.Min(signal.Confidence+0.1, 1.0)
		}
	}

	if higherTF == nil || higherTF.SMA20 == nil || higherTF.SMA50 == nil {
		return signal, nil
	}

	// Contrarian signals are intentionally counter-trend; don't filter by higher TF
	if result.Stance == entity.MarketStanceContrarian {
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

	// Signal aligns with higher TF: boost confidence by 10% (capped at 1.0)
	signal.Confidence = math.Min(signal.Confidence+0.1, 1.0)
	return signal, nil
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
			SymbolID:   symbolID,
			Action:     entity.SignalActionBuy,
			Confidence: trendFollowConfidence(sma20, sma50, rsi, histogram, true),
			Reason:     reason,
			Timestamp:  now,
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
			SymbolID:   symbolID,
			Action:     entity.SignalActionSell,
			Confidence: trendFollowConfidence(sma20, sma50, rsi, histogram, false),
			Reason:     reason,
			Timestamp:  now,
		}
	}
	return &entity.Signal{
		SymbolID:  symbolID,
		Action:    entity.SignalActionHold,
		Reason:    "trend follow: no clear signal",
		Timestamp: now,
	}
}

// trendFollowConfidence computes a 0.0–1.0 confidence score for trend-follow signals.
// Factors: SMA divergence strength (40%), RSI headroom (30%), MACD histogram agreement (30%).
func trendFollowConfidence(sma20, sma50, rsi float64, histogram *float64, isBuy bool) float64 {
	// SMA divergence: how strongly the cross is established (capped at 2%)
	smaDivergence := math.Min(math.Abs(sma20-sma50)/sma50*100, 2.0) / 2.0

	// RSI headroom: distance from the overbought/oversold boundary
	var rsiRoom float64
	if isBuy {
		rsiRoom = (70 - rsi) / 40 // 30→1.0, 70→0.0
	} else {
		rsiRoom = (rsi - 30) / 40 // 70→1.0, 30→0.0
	}
	rsiRoom = math.Max(0, math.Min(1, rsiRoom))

	// MACD histogram confirmation
	macdConfirm := 0.5 // neutral when histogram unavailable
	if histogram != nil {
		macdConfirm = math.Min(math.Abs(*histogram)/10, 1.0)
	}

	return smaDivergence*0.4 + rsiRoom*0.3 + macdConfirm*0.3
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
			SymbolID:   symbolID,
			Action:     entity.SignalActionBuy,
			Confidence: contrarianConfidence(rsi, histogram, true),
			Reason:     reason,
			Timestamp:  now,
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
			SymbolID:   symbolID,
			Action:     entity.SignalActionSell,
			Confidence: contrarianConfidence(rsi, histogram, false),
			Reason:     reason,
			Timestamp:  now,
		}
	}
	return &entity.Signal{
		SymbolID:  symbolID,
		Action:    entity.SignalActionHold,
		Reason:    "contrarian: RSI in neutral zone",
		Timestamp: now,
	}
}

// contrarianConfidence computes a 0.0–1.0 confidence score for contrarian signals.
// Factors: RSI extremity (60%), MACD not-against (40%).
func contrarianConfidence(rsi float64, histogram *float64, isBuy bool) float64 {
	// RSI extremity: how deep into oversold/overbought territory
	var rsiExtreme float64
	if isBuy {
		rsiExtreme = (30 - rsi) / 30 // 0→1.0, 30→0.0
	} else {
		rsiExtreme = (rsi - 70) / 30 // 100→1.0, 70→0.0
	}
	rsiExtreme = math.Max(0, math.Min(1, rsiExtreme))

	// MACD not-against: lower opposing momentum = higher confidence
	macdNotAgainst := 0.5 // neutral when histogram unavailable
	if histogram != nil {
		macdNotAgainst = 1.0 - math.Min(math.Abs(*histogram)/20, 1.0)
	}

	return rsiExtreme*0.6 + macdNotAgainst*0.4
}
