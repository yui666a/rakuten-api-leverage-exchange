package usecase

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// StanceResult はスタンス解決の結果。
type StanceResult struct {
	Stance    entity.MarketStance `json:"stance"`
	Reasoning string              `json:"reasoning"`
	Source    string              `json:"source"` // "override" or "rule-based"
	ExpiresAt *time.Time          `json:"expiresAt,omitempty"`
	UpdatedAt int64               `json:"updatedAt"`
}

// StanceResolver はマーケットスタンスを解決するインターフェース。
type StanceResolver interface {
	Resolve(ctx context.Context, indicators entity.IndicatorSet) StanceResult
}

type stanceOverride struct {
	Stance    entity.MarketStance
	Reasoning string
	SetAt     time.Time
	ExpiresAt time.Time
}

// RuleBasedStanceResolver はルールベースでマーケットスタンスを解決する。
type RuleBasedStanceResolver struct {
	mu       sync.RWMutex
	override *stanceOverride
	repo     repository.StanceOverrideRepository
}

// NewRuleBasedStanceResolver はRuleBasedStanceResolverを生成する。
// repoがnon-nilの場合、起動時にDBからオーバーライドを復元する。
func NewRuleBasedStanceResolver(repo repository.StanceOverrideRepository) *RuleBasedStanceResolver {
	r := &RuleBasedStanceResolver{
		repo: repo,
	}
	if repo != nil {
		record, err := repo.Load(context.Background())
		if err == nil && record != nil {
			setAt := time.Unix(record.SetAt, 0)
			expiresAt := setAt.Add(time.Duration(record.TTLSec) * time.Second)
			if time.Now().Before(expiresAt) {
				r.override = &stanceOverride{
					Stance:    entity.MarketStance(record.Stance),
					Reasoning: record.Reasoning,
					SetAt:     setAt,
					ExpiresAt: expiresAt,
				}
			} else {
				// 期限切れのオーバーライドを削除
				_ = repo.Delete(context.Background())
			}
		}
	}
	return r
}

// Resolve はインジケータに基づいてマーケットスタンスを解決する。
// オーバーライドが有効な場合はそれを優先する。
func (r *RuleBasedStanceResolver) Resolve(ctx context.Context, indicators entity.IndicatorSet) StanceResult {
	now := time.Now()

	// オーバーライドチェック
	r.mu.RLock()
	override := r.override
	r.mu.RUnlock()

	if override != nil {
		if now.Before(override.ExpiresAt) {
			expiresAt := override.ExpiresAt
			return StanceResult{
				Stance:    override.Stance,
				Reasoning: override.Reasoning,
				Source:    "override",
				ExpiresAt: &expiresAt,
				UpdatedAt: override.SetAt.Unix(),
			}
		}
		// 期限切れ: 自動クリア
		r.clearOverrideInternal()
	}

	// ルールベース判定
	return r.applyRules(indicators, now)
}

func (r *RuleBasedStanceResolver) applyRules(indicators entity.IndicatorSet, now time.Time) StanceResult {
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

	// 2. RSI極端値 → CONTRARIAN
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

	// 3. SMA収束 → HOLD
	divergence := math.Abs(sma20-sma50) / sma50
	if divergence < 0.001 { // 0.1%
		return StanceResult{
			Stance:    entity.MarketStanceHold,
			Reasoning: "SMA converged",
			Source:    "rule-based",
			UpdatedAt: now.Unix(),
		}
	}

	// 4. それ以外 → TREND_FOLLOW
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

// SetOverride はスタンスオーバーライドを設定する。
func (r *RuleBasedStanceResolver) SetOverride(stance entity.MarketStance, reasoning string, ttl time.Duration) {
	now := time.Now()
	expiresAt := now.Add(ttl)

	r.mu.Lock()
	r.override = &stanceOverride{
		Stance:    stance,
		Reasoning: reasoning,
		SetAt:     now,
		ExpiresAt: expiresAt,
	}
	r.mu.Unlock()

	if r.repo != nil {
		_ = r.repo.Save(context.Background(), repository.StanceOverrideRecord{
			Stance:    string(stance),
			Reasoning: reasoning,
			SetAt:     now.Unix(),
			TTLSec:    int64(ttl.Seconds()),
		})
	}
}

// ClearOverride はスタンスオーバーライドをクリアする。
func (r *RuleBasedStanceResolver) ClearOverride() {
	r.clearOverrideInternal()
}

func (r *RuleBasedStanceResolver) clearOverrideInternal() {
	r.mu.Lock()
	r.override = nil
	r.mu.Unlock()

	if r.repo != nil {
		_ = r.repo.Delete(context.Background())
	}
}

// GetOverride は現在有効なオーバーライドを返す。期限切れの場合はnilを返す。
func (r *RuleBasedStanceResolver) GetOverride() *StanceResult {
	r.mu.RLock()
	override := r.override
	r.mu.RUnlock()

	if override == nil {
		return nil
	}

	now := time.Now()
	if now.After(override.ExpiresAt) || now.Equal(override.ExpiresAt) {
		r.clearOverrideInternal()
		return nil
	}

	expiresAt := override.ExpiresAt
	return &StanceResult{
		Stance:    override.Stance,
		Reasoning: override.Reasoning,
		Source:    "override",
		ExpiresAt: &expiresAt,
		UpdatedAt: override.SetAt.Unix(),
	}
}
