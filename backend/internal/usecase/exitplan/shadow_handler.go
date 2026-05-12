// Package exitplan は ExitPlan を駆動するイベントハンドラを提供する。
//
// Phase 1 (シャドウ運用) では ShadowHandler が venue 確定済みの
// PositionConfirmedEvent を listen して ExitPlan を作成し、OrderEvent
// (ClosedPositionID > 0) で close する。SL/TP/Trailing の発火判定や
// HWM 更新は既存 RiskManager / TickRiskHandler に任せたまま。観察
// ログを取って Phase 2 で発火経路を移管する。
//
// 2026-05-12 の本番事故 (OrderEvent.Price が signalPrice fallback で
// 汚染され、EntryPrice ¥9,168.4 と記録 → TP/SL が誤発火) を踏まえ、
// PR ADR #260 で ExitPlan 作成経路は PositionConfirmedEvent (venue 真値
// 由来) に切り替えた。close 経路は OrderEvent のままで問題ない:
// closeLocked の ClosedPositionID は venue から返ってきた real position
// id で、shadow が close する対象は plan の PositionID と一致する。
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

// ShadowHandler はシャドウ運用で ExitPlan を作成・close する。
// emit はせず、failure はログだけで握り潰す（既存の発注パスに影響を与えない）。
//
// 作成経路 (handleOpen) は PositionConfirmedEvent を入力にして venue 真値の
// EntryPrice を使う。close 経路 (handleClose) は OrderEvent.ClosedPositionID
// を入力にする (close 時の価格は ExitPlan の SL/TP 判定に使われないので
// signalPrice fallback の影響を受けない)。
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

// Handle implements eventengine.EventHandler.
//
//   - PositionConfirmedEvent → ExitPlan を新規作成 (venue 真値の EntryPrice
//     で TP/SL ラインを引く)
//   - OrderEvent with ClosedPositionID > 0 → ExitPlan を close
//
// reversal トレード等で 1 OrderEvent の OpenedPositionID にも値が乗る
// ケースは引き続き存在するが、shadow は **open 側を OrderEvent から作らない**:
// 真値の EntryPrice を持たないため誤った plan を入れるくらいなら、次の
// SyncPositions で発火する PositionConfirmedEvent を待つ。
func (h *ShadowHandler) Handle(ctx context.Context, ev entity.Event) ([]entity.Event, error) {
	switch e := ev.(type) {
	case entity.PositionConfirmedEvent:
		h.handleConfirmed(ctx, e)
	case entity.OrderEvent:
		if e.ClosedPositionID != 0 {
			h.handleClose(ctx, e)
		}
	}
	return nil, nil
}

func (h *ShadowHandler) handleConfirmed(ctx context.Context, ev entity.PositionConfirmedEvent) {
	if ev.Side != entity.OrderSideBuy && ev.Side != entity.OrderSideSell {
		h.logger.Warn("unknown order side, skipping shadow create",
			"side", ev.Side, "positionID", ev.PositionID,
		)
		return
	}
	if ev.EntryPrice <= 0 {
		// PositionConfirmedEvent invariant violation. SyncPositions
		// already filters Price<=0 rows out, so reaching here means
		// the emit path was bypassed somehow — log loudly and skip
		// rather than persist a plan anchored to a non-positive price.
		h.logger.Warn("shadow ExitPlan skipped: confirmed event had non-positive entry price",
			"positionID", ev.PositionID, "entryPrice", ev.EntryPrice,
		)
		return
	}
	// Idempotency guard: SyncPositions emits a confirmed event the first
	// time a PositionID becomes visible. If the same event reaches us
	// twice (e.g. test fixtures, retry paths), skip the duplicate
	// instead of failing the repo's unique constraint.
	if existing, err := h.repo.FindByPositionID(ctx, ev.PositionID); err == nil && existing != nil {
		return
	}
	plan, err := domainexitplan.New(domainexitplan.NewInput{
		PositionID: ev.PositionID,
		SymbolID:   ev.SymbolID,
		Side:       ev.Side,
		EntryPrice: ev.EntryPrice,
		Policy:     h.policy,
		CreatedAt:  ev.EntryTimestamp,
	})
	if err != nil {
		h.logger.Warn("shadow ExitPlan construction failed",
			"err", err, "positionID", ev.PositionID,
		)
		return
	}
	if err := h.repo.Create(ctx, plan); err != nil {
		h.logger.Warn("shadow ExitPlan persist failed",
			"err", err, "positionID", ev.PositionID,
		)
		return
	}
	h.logger.Info("shadow ExitPlan created",
		"positionID", ev.PositionID,
		"orderID", ev.OrderID,
		"symbolID", ev.SymbolID,
		"side", ev.Side,
		"entryPrice", ev.EntryPrice,
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
