package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// FailureKind は注文実行が失敗した際の分類。
//
// pre-flight 記録 (Step 2) で submitted/failed の判定に使う。
type FailureKind string

const (
	// FailureKindNone は失敗していない。
	FailureKindNone FailureKind = ""
	// FailureKindPreSend は送信前 (バリデーション・risk reject) で失敗。楽天は呼ばれていない。
	FailureKindPreSend FailureKind = "pre_send"
	// FailureKindRejected は楽天が明示的に拒否 (4xx + 構造化エラーボディ)。失敗確定。
	FailureKindRejected FailureKind = "rejected"
	// FailureKindAmbiguous は楽天側の真実が不明 (パース失敗・タイムアウト・5xx)。
	// reconcile による確定が必要。
	FailureKindAmbiguous FailureKind = "ambiguous"
)

// ExecutionResult は注文実行の結果。
type ExecutionResult struct {
	Executed    bool        `json:"executed"`
	OrderID     int64       `json:"orderId,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	RawResponse []byte      `json:"-"`
	FailureKind FailureKind `json:"-"`
}

// OrderExecutor はシグナルに基づいて注文を実行する。
type OrderExecutor struct {
	client      repository.OrderClient
	riskMgr     *RiskManager
	realtimeHub *RealtimeHub // optional. 約定成立時にトレードイベントを push する。
}

func NewOrderExecutor(client repository.OrderClient, riskMgr *RiskManager) *OrderExecutor {
	return &OrderExecutor{
		client:  client,
		riskMgr: riskMgr,
	}
}

// SetRealtimeHub wires a RealtimeHub so that successful executions publish a
// "trade_event" payload to all subscribers (front-end will turn this into a
// browser notification). nil clears the hub. The executor remains fully
// functional without a hub — pushes are best-effort.
func (e *OrderExecutor) SetRealtimeHub(hub *RealtimeHub) {
	e.realtimeHub = hub
}

// TradeEventKind labels the lifecycle phase a trade_event represents so the
// UI can pick a per-kind icon / sound without re-deriving from raw fields.
type TradeEventKind string

const (
	// TradeEventOpen は新規エントリーが約定したことを示す。
	TradeEventOpen TradeEventKind = "open"
	// TradeEventClose はポジションのクローズが約定したことを示す。
	TradeEventClose TradeEventKind = "close"
)

// TradeEventPayload is the JSON shape pushed on the "trade_event" channel.
// Kept narrow so the front-end can reason about it without depending on the
// internal Order entity.
type TradeEventPayload struct {
	Kind          TradeEventKind `json:"kind"`
	SymbolID      int64          `json:"symbolId"`
	Side          string         `json:"side"`
	Amount        float64        `json:"amount"`
	Price         float64        `json:"price"`
	OrderID       int64          `json:"orderId"`
	ClientOrderID string         `json:"clientOrderId,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	PositionID    int64          `json:"positionId,omitempty"`
	Timestamp     int64          `json:"timestamp"`
}

// publishTradeEvent best-effort sends a trade_event to the realtime hub. Any
// publish failure is logged at debug level — losing a notification must never
// fail the trade itself.
func (e *OrderExecutor) publishTradeEvent(payload TradeEventPayload) {
	if e.realtimeHub == nil {
		return
	}
	if err := e.realtimeHub.PublishData("trade_event", payload.SymbolID, payload); err != nil {
		slog.Debug("publish trade_event failed", "err", err)
	}
}

// ExecuteSignal はシグナルを受け取り、Risk Manager で検証後に注文を送信する。
//
// clientOrderID は注文ライフサイクル追跡用のキー。Step 1.5 ではログ出力に留め、
// Step 2 で pre-flight 記録に使われる。
func (e *OrderExecutor) ExecuteSignal(ctx context.Context, clientOrderID string, signal entity.Signal, price float64, amount float64) (*ExecutionResult, error) {
	if signal.Action == entity.SignalActionHold {
		return &ExecutionResult{
			Executed:    false,
			Reason:      "signal is HOLD, no action",
			FailureKind: FailureKindPreSend,
		}, nil
	}

	side := entity.OrderSideBuy
	if signal.Action == entity.SignalActionSell {
		side = entity.OrderSideSell
	}

	proposal := entity.OrderProposal{
		SymbolID:  signal.SymbolID,
		Side:      side,
		OrderType: entity.OrderTypeMarket,
		Amount:    amount,
		Price:     price,
		IsClose:   false,
	}

	check := e.riskMgr.CheckOrder(ctx, proposal)
	if !check.Approved {
		slog.Info("order rejected by risk manager", "reason", check.Reason, "clientOrderID", clientOrderID)
		return &ExecutionResult{
			Executed:    false,
			Reason:      fmt.Sprintf("risk rejected: %s", check.Reason),
			FailureKind: FailureKindPreSend,
		}, nil
	}

	req := entity.OrderRequest{
		SymbolID:     signal.SymbolID,
		OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{
			OrderBehavior: entity.OrderBehaviorOpen,
			OrderSide:     side,
			OrderType:     entity.OrderTypeMarket,
			Amount:        amount,
		},
	}

	orders, err := e.client.CreateOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("API returned no orders")
	}

	slog.Info("order created",
		"orderID", orders[0].ID,
		"symbolID", signal.SymbolID,
		"side", side,
		"amount", amount,
		"clientOrderID", clientOrderID,
	)

	e.publishTradeEvent(TradeEventPayload{
		Kind:          TradeEventOpen,
		SymbolID:      signal.SymbolID,
		Side:          string(side),
		Amount:        amount,
		Price:         price,
		OrderID:       orders[0].ID,
		ClientOrderID: clientOrderID,
		Reason:        signal.Reason,
		Timestamp:     time.Now().UnixMilli(),
	})

	return &ExecutionResult{
		Executed: true,
		OrderID:  orders[0].ID,
	}, nil
}

// ClosePosition は指定ポジションを成行決済する。
//
// clientOrderID は注文ライフサイクル追跡用のキー。Step 1.5 ではログ出力に留め、
// Step 2 で pre-flight 記録に使われる。
func (e *OrderExecutor) ClosePosition(ctx context.Context, clientOrderID string, pos entity.Position, currentPrice float64) (*ExecutionResult, error) {
	closeSide := entity.OrderSideSell
	if pos.OrderSide == entity.OrderSideSell {
		closeSide = entity.OrderSideBuy
	}

	proposal := entity.OrderProposal{
		SymbolID:   pos.SymbolID,
		Side:       closeSide,
		OrderType:  entity.OrderTypeMarket,
		Amount:     pos.RemainingAmount,
		Price:      currentPrice,
		IsClose:    true,
		PositionID: &pos.ID,
	}

	check := e.riskMgr.CheckOrder(ctx, proposal)
	if !check.Approved {
		return &ExecutionResult{
			Executed:    false,
			Reason:      fmt.Sprintf("risk rejected close: %s", check.Reason),
			FailureKind: FailureKindPreSend,
		}, nil
	}

	posID := pos.ID
	req := entity.OrderRequest{
		SymbolID:     pos.SymbolID,
		OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{
			OrderBehavior: entity.OrderBehaviorClose,
			PositionID:    &posID,
			OrderSide:     closeSide,
			OrderType:     entity.OrderTypeMarket,
			Amount:        pos.RemainingAmount,
		},
	}

	orders, err := e.client.CreateOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to close position: %w", err)
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("API returned no orders for close")
	}

	slog.Info("position closed",
		"positionID", pos.ID,
		"orderID", orders[0].ID,
		"side", closeSide,
		"clientOrderID", clientOrderID,
	)

	e.publishTradeEvent(TradeEventPayload{
		Kind:          TradeEventClose,
		SymbolID:      pos.SymbolID,
		Side:          string(closeSide),
		Amount:        pos.RemainingAmount,
		Price:         currentPrice,
		OrderID:       orders[0].ID,
		ClientOrderID: clientOrderID,
		PositionID:    pos.ID,
		Timestamp:     time.Now().UnixMilli(),
	})

	return &ExecutionResult{
		Executed: true,
		OrderID:  orders[0].ID,
	}, nil
}
