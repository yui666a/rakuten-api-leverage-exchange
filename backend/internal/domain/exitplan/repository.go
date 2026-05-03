package exitplan

import "context"

// Repository は建玉ごとの ExitPlan を永続化する。SL/TP のルールは
// 不変なので Create 時に書ききり、変化点（Trailing 活性化・HWM 更新・close）
// だけを書き込む I/O 設計。
type Repository interface {
	// Create は新規 ExitPlan を永続化する。PositionID は unique 制約で
	// 同じ建玉に対して二重 ExitPlan が作られない。
	Create(ctx context.Context, plan *ExitPlan) error

	// FindByPositionID は建玉 ID で ExitPlan を引く。closed 含む全件。
	// 見つからない場合は (nil, nil)。
	FindByPositionID(ctx context.Context, positionID int64) (*ExitPlan, error)

	// ListOpen は ClosedAt IS NULL の ExitPlan を symbol_id で絞って返す。
	// Phase 2 の tick handler が毎 tick これを呼ぶので、走査効率を意識する。
	ListOpen(ctx context.Context, symbolID int64) ([]*ExitPlan, error)

	// UpdateTrailing は HWM と Activated フラグだけを更新する。SL/TP の
	// ルール部分は変更しない。closed な plan に対してはエラー。
	UpdateTrailing(ctx context.Context, planID int64, hwm float64, activated bool, updatedAt int64) error

	// Close は ClosedAt を立てる。二重 close はエラー。
	Close(ctx context.Context, planID int64, closedAt int64) error
}
