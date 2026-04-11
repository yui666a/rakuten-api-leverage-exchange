package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// CreateOrderOutcome は CreateOrderRaw の結果。
//
// RawResponse は楽天 API のレスポンス本文 (パース失敗時でもセットされる)。
// Orders はパースに成功した場合のみ非 nil。
// HTTPStatus は HTTP ステータスコード。0 のとき送信前 / コネクション失敗で取得不可。
// ParseError はレスポンス本文のパースに失敗した場合のエラー。
// TransportError は HTTP コネクション系の失敗 (DNS, TCP reset, タイムアウト等)。
// HTTPError は 4xx/5xx を受け取った場合のエラー (本文がパースできた場合は nil で、Orders と HTTPStatus を見て判断する)。
type CreateOrderOutcome struct {
	RawResponse    []byte
	HTTPStatus     int
	Orders         []entity.Order
	ParseError     error
	TransportError error
	HTTPError      error
}

// OrderClient は注文操作のインターフェース。
type OrderClient interface {
	CreateOrder(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error)
	// CreateOrderRaw は CreateOrder の詳細版。送信から応答パースまでの各段階を
	// 構造化して返すため、呼び出し側で submitted/failed の判定が可能。
	CreateOrderRaw(ctx context.Context, req entity.OrderRequest) (CreateOrderOutcome, error)
	CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error)
	GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error)
	GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error)
	GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error)
	GetAssets(ctx context.Context) ([]entity.Asset, error)
}
