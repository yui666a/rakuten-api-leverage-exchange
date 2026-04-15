package usecase

import (
	"context"
	"log/slog"
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
	Resolve(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) StanceResult
	ResolveAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult
}

type stanceOverride struct {
	Stance    entity.MarketStance
	Reasoning string
	SetAt     time.Time
	ExpiresAt time.Time
}

// RuleBasedStanceResolverOptions controls override behavior.
type RuleBasedStanceResolverOptions struct {
	// DisableOverride forces resolver to always use rule-based result.
	DisableOverride bool
	// DisablePersistence prevents loading/saving/deleting override records.
	DisablePersistence bool
}

// RuleBasedStanceResolver はルールベースでマーケットスタンスを解決する。
type RuleBasedStanceResolver struct {
	mu       sync.RWMutex
	override *stanceOverride
	repo     repository.StanceOverrideRepository
	options  RuleBasedStanceResolverOptions
}

// NewRuleBasedStanceResolver はRuleBasedStanceResolverを生成する。
// repoがnon-nilの場合、起動時にDBからオーバーライドを復元する。
func NewRuleBasedStanceResolver(repo repository.StanceOverrideRepository) *RuleBasedStanceResolver {
	return NewRuleBasedStanceResolverWithOptions(repo, RuleBasedStanceResolverOptions{})
}

// NewRuleBasedStanceResolverWithOptions creates resolver with explicit options.
func NewRuleBasedStanceResolverWithOptions(repo repository.StanceOverrideRepository, options RuleBasedStanceResolverOptions) *RuleBasedStanceResolver {
	if options.DisablePersistence {
		repo = nil
	}

	r := &RuleBasedStanceResolver{
		repo:    repo,
		options: options,
	}
	if repo != nil && !options.DisableOverride {
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
				if err := repo.Delete(context.Background()); err != nil {
					slog.Warn("failed to delete expired stance override on startup", "error", err)
				}
			}
		}
	}
	return r
}

// Resolve はインジケータに基づいてマーケットスタンスを解決する。
// オーバーライドが有効な場合はそれを優先する。
func (r *RuleBasedStanceResolver) Resolve(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) StanceResult {
	return r.ResolveAt(ctx, indicators, lastPrice, time.Now())
}

// ResolveAt resolves stance at caller-supplied time (for deterministic backtests).
func (r *RuleBasedStanceResolver) ResolveAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult {
	if now.IsZero() {
		now = time.Now()
	}

	if r.options.DisableOverride {
		return r.applyRules(indicators, lastPrice, now)
	}

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
	return r.applyRules(indicators, lastPrice, now)
}

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

const (
	minOverrideTTL = 1 * time.Minute
	maxOverrideTTL = 1440 * time.Minute // 24h
)

// SetOverride はスタンスオーバーライドを設定する。
// TTLは1分〜1440分(24時間)の範囲にクランプされる。
func (r *RuleBasedStanceResolver) SetOverride(stance entity.MarketStance, reasoning string, ttl time.Duration) {
	if r.options.DisableOverride {
		return
	}

	if ttl < minOverrideTTL {
		ttl = minOverrideTTL
	}
	if ttl > maxOverrideTTL {
		ttl = maxOverrideTTL
	}

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
		if err := r.repo.Save(context.Background(), repository.StanceOverrideRecord{
			Stance:    string(stance),
			Reasoning: reasoning,
			SetAt:     now.Unix(),
			TTLSec:    int64(ttl.Seconds()),
		}); err != nil {
			slog.Warn("failed to save stance override", "error", err)
		}
	}
}

// ClearOverride はスタンスオーバーライドをクリアする。
func (r *RuleBasedStanceResolver) ClearOverride() {
	if r.options.DisableOverride {
		return
	}
	r.clearOverrideInternal()
}

func (r *RuleBasedStanceResolver) clearOverrideInternal() {
	r.mu.Lock()
	r.override = nil
	r.mu.Unlock()

	if r.repo != nil {
		if err := r.repo.Delete(context.Background()); err != nil {
			slog.Warn("failed to delete stance override", "error", err)
		}
	}
}

// GetOverride は現在有効なオーバーライドを返す。期限切れの場合はnilを返す。
func (r *RuleBasedStanceResolver) GetOverride() *StanceResult {
	if r.options.DisableOverride {
		return nil
	}

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
