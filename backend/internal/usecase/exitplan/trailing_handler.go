package exitplan

import (
	"context"
	"log/slog"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	domainexitplan "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/exitplan"
)

// TrailingPersistenceConfig は TrailingPersistenceHandler のコンストラクタ引数。
type TrailingPersistenceConfig struct {
	Repo   domainexitplan.Repository
	Logger *slog.Logger
}

// TrailingPersistenceHandler は TickEvent を listen して、open ExitPlan の
// HWM を更新する。永続化失敗は warn ログで握り潰す（Phase 2a はまだ
// 既存 TickRiskHandler の発火経路には影響しない）。
//
// Phase 2b で TickRiskHandler が ExitPlan ベースに置き換わったら、本
// handler の HWM 更新が発火判定の唯一のソースになる。
type TrailingPersistenceHandler struct {
	repo   domainexitplan.Repository
	logger *slog.Logger
}

// NewTrailingPersistenceHandler は handler を返す。Repo nil で panic。
func NewTrailingPersistenceHandler(cfg TrailingPersistenceConfig) *TrailingPersistenceHandler {
	if cfg.Repo == nil {
		panic("exitplan.NewTrailingPersistenceHandler: Repo must not be nil")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &TrailingPersistenceHandler{
		repo:   cfg.Repo,
		logger: logger.With("component", "exitplan_trailing"),
	}
}

// Handle implements eventengine.EventHandler.
func (h *TrailingPersistenceHandler) Handle(ctx context.Context, ev entity.Event) ([]entity.Event, error) {
	te, ok := ev.(entity.TickEvent)
	if !ok {
		return nil, nil
	}
	plans, err := h.repo.ListOpen(ctx, te.SymbolID)
	if err != nil {
		h.logger.Warn("ListOpen failed", "err", err, "symbolID", te.SymbolID)
		return nil, nil
	}
	for _, plan := range plans {
		changed := plan.RaiseTrailingHWM(te.Price, te.Timestamp)
		if !changed {
			continue
		}
		hwm := *plan.TrailingHWM
		if err := h.repo.UpdateTrailing(ctx, plan.ID, hwm, plan.TrailingActivated, te.Timestamp); err != nil {
			h.logger.Warn("UpdateTrailing failed",
				"err", err,
				"planID", plan.ID,
				"positionID", plan.PositionID,
				"hwm", hwm,
			)
			continue
		}
		h.logger.Debug("trailing HWM persisted",
			"planID", plan.ID,
			"positionID", plan.PositionID,
			"hwm", hwm,
			"activated", plan.TrailingActivated,
		)
	}
	return nil, nil
}
