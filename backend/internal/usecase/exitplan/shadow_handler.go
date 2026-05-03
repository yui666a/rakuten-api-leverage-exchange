// Package exitplan は ExitPlan を駆動するイベントハンドラを提供する。
//
// Phase 1 (シャドウ運用) では ShadowHandler が OrderEvent を listen して
// ExitPlan の作成・close だけを行う。SL/TP/Trailing の発火判定や HWM 更新は
// 既存 RiskManager / TickRiskHandler に任せたまま。観察ログを取って
// Phase 2 で発火経路を移管する。
package exitplan

import (
	"context"
	"log/slog"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	domainexitplan "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/exitplan"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

// ShadowHandlerConfig は ShadowHandler のコンストラクタ引数。
type ShadowHandlerConfig struct {
	Repo   domainexitplan.Repository
	Policy risk.RiskPolicy
	// Logger は省略可。nil の場合 slog.Default() を使う。
	Logger *slog.Logger
}

// ShadowHandler は OrderEvent をシャドウで listen し、ExitPlan を作成・close する。
// emit はせず、failure はログだけで握り潰す（既存の発注パスに影響を与えない）。
type ShadowHandler struct {
	repo   domainexitplan.Repository
	policy risk.RiskPolicy
	logger *slog.Logger
}

// NewShadowHandler は設定済みの ShadowHandler を返す。Repo nil は panic。
func NewShadowHandler(cfg ShadowHandlerConfig) *ShadowHandler {
	if cfg.Repo == nil {
		panic("exitplan.NewShadowHandler: Repo must not be nil")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ShadowHandler{
		repo:   cfg.Repo,
		policy: cfg.Policy,
		logger: logger.With("component", "exitplan_shadow"),
	}
}

// Handle implements eventengine.EventHandler. OrderEvent 以外は素通り。
func (h *ShadowHandler) Handle(ctx context.Context, ev entity.Event) ([]entity.Event, error) {
	oe, ok := ev.(entity.OrderEvent)
	if !ok {
		return nil, nil
	}
	// reversal トレード等で 1 OrderEvent に open/close 両方乗ることがあるので
	// 両分岐を独立に走らせる。
	if oe.ClosedPositionID != 0 {
		h.handleClose(ctx, oe)
	}
	if oe.OpenedPositionID != 0 {
		h.handleOpen(ctx, oe)
	}
	return nil, nil
}

func (h *ShadowHandler) handleOpen(ctx context.Context, oe entity.OrderEvent) {
	side := entity.OrderSide(oe.Side)
	if side != entity.OrderSideBuy && side != entity.OrderSideSell {
		h.logger.Warn("unknown order side, skipping shadow create",
			"side", oe.Side, "positionID", oe.OpenedPositionID,
		)
		return
	}
	plan, err := domainexitplan.New(domainexitplan.NewInput{
		PositionID: oe.OpenedPositionID,
		SymbolID:   oe.SymbolID,
		Side:       side,
		EntryPrice: oe.Price,
		Policy:     h.policy,
		CreatedAt:  oe.Timestamp,
	})
	if err != nil {
		h.logger.Warn("shadow ExitPlan construction failed",
			"err", err, "positionID", oe.OpenedPositionID,
		)
		return
	}
	if err := h.repo.Create(ctx, plan); err != nil {
		h.logger.Warn("shadow ExitPlan persist failed",
			"err", err, "positionID", oe.OpenedPositionID,
		)
		return
	}
	h.logger.Info("shadow ExitPlan created",
		"positionID", oe.OpenedPositionID,
		"symbolID", oe.SymbolID,
		"side", oe.Side,
		"entryPrice", oe.Price,
		"planID", plan.ID,
	)
}

func (h *ShadowHandler) handleClose(ctx context.Context, oe entity.OrderEvent) {
	plan, err := h.repo.FindByPositionID(ctx, oe.ClosedPositionID)
	if err != nil {
		h.logger.Warn("shadow ExitPlan find failed on close",
			"err", err, "positionID", oe.ClosedPositionID,
		)
		return
	}
	if plan == nil {
		// シャドウ運用初期は楽天 API 既存建玉に対して plan が無いケースあり
		h.logger.Info("shadow ExitPlan not found on close (orphan close)",
			"positionID", oe.ClosedPositionID,
		)
		return
	}
	if plan.IsClosed() {
		h.logger.Warn("shadow ExitPlan already closed",
			"positionID", oe.ClosedPositionID, "planID", plan.ID,
		)
		return
	}
	if err := h.repo.Close(ctx, plan.ID, oe.Timestamp); err != nil {
		h.logger.Warn("shadow ExitPlan close persist failed",
			"err", err, "planID", plan.ID,
		)
		return
	}
	h.logger.Info("shadow ExitPlan closed",
		"positionID", oe.ClosedPositionID, "planID", plan.ID,
		"closePrice", oe.Price,
	)
}
