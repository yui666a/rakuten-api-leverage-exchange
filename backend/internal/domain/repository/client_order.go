package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ClientOrderRecord はクライアント注文 1 件のライフサイクルを表すレコード。
//
// status は注文の現在状態。pending/submitted は楽天側の真実が不明な状態を表し、
// reconcile によって reconciled-* に確定する。
//
// SymbolID/Intent/Side/Amount/PositionID は reconcile 時の照合キー。RawResponse は
// 楽天応答の生 JSON (パース失敗時の手動解析用)。
type ClientOrderRecord struct {
	ClientOrderID string
	Status        entity.ClientOrderStatus
	SymbolID      int64
	Intent        entity.ClientOrderIntent
	Side          entity.OrderSide
	Amount        float64
	PositionID    int64 // 0 のとき未指定 (intent != close)
	OrderID       int64 // 0 のとき未取得
	RawResponse   string
	ErrorMessage  string
	CreatedAt     int64
	UpdatedAt     int64

	// Executed は後方互換のために残す。Status が confirmed/completed/reconciled-confirmed のとき true。
	Executed bool
}

// ClientOrderUpdate は UpdateStatus でまとめて差し込めるフィールド束。
// nil ポインタは「更新しない」を表す。
type ClientOrderUpdate struct {
	OrderID      *int64
	RawResponse  *string
	ErrorMessage *string
}

type ClientOrderRepository interface {
	// Find は client_order_id で検索し、存在しなければ nil を返す。
	// 後方互換のために残す (Step 2 で deprecated 化予定)。
	Find(ctx context.Context, clientOrderID string) (*ClientOrderRecord, error)

	// Save はレコードを保存する。後方互換のために残す (Step 2 で deprecated 化予定)。
	Save(ctx context.Context, record ClientOrderRecord) error

	// InsertOrGet は INSERT OR IGNORE で record を挿入し、既存行があれば既存行を返す。
	// inserted=true なら新規挿入、false なら既存行 (existing) を読み直したもの。
	InsertOrGet(ctx context.Context, record ClientOrderRecord) (existing *ClientOrderRecord, inserted bool, err error)

	// UpdateStatus は status と任意フィールドを更新する。updated_at は呼び出し側時刻 (now) で上書きする。
	UpdateStatus(ctx context.Context, clientOrderID string, status entity.ClientOrderStatus, now int64, update ClientOrderUpdate) error

	// ListByStatus は指定 status のレコードを updated_at 昇順で最大 limit 件返す。
	ListByStatus(ctx context.Context, statuses []entity.ClientOrderStatus, limit int) ([]ClientOrderRecord, error)

	// DeleteExpired は created_at が beforeUnix より古いレコードを削除する。
	DeleteExpired(ctx context.Context, beforeUnix int64) error
}
