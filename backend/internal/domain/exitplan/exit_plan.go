// Package exitplan は建玉と 1:1 で対応する出口管理エンティティ ExitPlan
// を定義する。entity / risk のどちらに置いても import cycle が発生する
// ため、独立した domain パッケージとして切り出している。
package exitplan

import (
	"errors"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

// ExitPlan は建玉と 1:1 で対応する出口管理エンティティ。SL/TP のルールは
// risk.RiskPolicy として保存し、現在価格は read 時に動的計算する（ATR
// レジーム変化への追従を許容）。Trailing の HWM だけが動的状態として
// 永続化される。
//
// Phase 1 ではシャドウ運用のみ。Phase 2 で SL/TP/Trailing 発火判定と
// CurrentSLPrice / CurrentTPPrice / CurrentTrailingTriggerPrice を追加する。
type ExitPlan struct {
	ID         int64
	PositionID int64
	SymbolID   int64
	Side       entity.OrderSide
	EntryPrice float64
	Policy     risk.RiskPolicy

	TrailingActivated bool
	TrailingHWM       *float64

	CreatedAt int64
	UpdatedAt int64
	ClosedAt  *int64
}

// NewInput は New の入力。コンストラクタ専用の record 型。
type NewInput struct {
	PositionID int64
	SymbolID   int64
	Side       entity.OrderSide
	EntryPrice float64
	Policy     risk.RiskPolicy
	CreatedAt  int64
}

// New は不変条件を検証して新しい ExitPlan を返す。Repository の Create で
// 永続化する前に必ずこれを通すことで「無防備な建玉」を防ぐ。
func New(in NewInput) (*ExitPlan, error) {
	if in.PositionID <= 0 {
		return nil, errors.New("ExitPlan: PositionID must be > 0")
	}
	if in.SymbolID <= 0 {
		return nil, errors.New("ExitPlan: SymbolID must be > 0")
	}
	if in.Side != entity.OrderSideBuy && in.Side != entity.OrderSideSell {
		return nil, fmt.Errorf("ExitPlan: Side must be BUY or SELL (got %q)", in.Side)
	}
	if in.EntryPrice <= 0 {
		return nil, fmt.Errorf("ExitPlan: EntryPrice must be > 0 (got %v)", in.EntryPrice)
	}
	if err := in.Policy.Validate(); err != nil {
		return nil, fmt.Errorf("ExitPlan: invalid policy: %w", err)
	}
	if in.CreatedAt <= 0 {
		return nil, errors.New("ExitPlan: CreatedAt must be > 0")
	}
	return &ExitPlan{
		PositionID: in.PositionID,
		SymbolID:   in.SymbolID,
		Side:       in.Side,
		EntryPrice: in.EntryPrice,
		Policy:     in.Policy,
		CreatedAt:  in.CreatedAt,
		UpdatedAt:  in.CreatedAt,
	}, nil
}

// IsClosed は close 済みか判定する。
func (e *ExitPlan) IsClosed() bool {
	return e.ClosedAt != nil
}

// RaiseTrailingHWM は新しい tick 価格で Trailing の最良値を更新する。
// 含み益超え（ロング: price > EntryPrice、ショート: price < EntryPrice）で
// 初めて呼ばれた瞬間に Activated を true にし HWM を price で初期化する。
// その後はロングなら新高値、ショートなら新安値のときだけ更新する。
//
// 戻り値は HWM が変化したか（=永続化が必要か）。closed plan に対して
// 呼ばれた場合や no-op の場合は false。
func (e *ExitPlan) RaiseTrailingHWM(price float64, now int64) bool {
	if e.IsClosed() {
		return false
	}
	if !e.TrailingActivated {
		switch e.Side {
		case entity.OrderSideBuy:
			if price <= e.EntryPrice {
				return false
			}
		case entity.OrderSideSell:
			if price >= e.EntryPrice {
				return false
			}
		}
		e.TrailingActivated = true
		hwm := price
		e.TrailingHWM = &hwm
		e.UpdatedAt = now
		return true
	}
	if e.TrailingHWM == nil {
		hwm := price
		e.TrailingHWM = &hwm
		e.UpdatedAt = now
		return true
	}
	switch e.Side {
	case entity.OrderSideBuy:
		if price > *e.TrailingHWM {
			*e.TrailingHWM = price
			e.UpdatedAt = now
			return true
		}
	case entity.OrderSideSell:
		if price < *e.TrailingHWM {
			*e.TrailingHWM = price
			e.UpdatedAt = now
			return true
		}
	}
	return false
}

// Close は ExitPlan を closed 状態に遷移させる。二重 close はエラー。
func (e *ExitPlan) Close(now int64) error {
	if e.IsClosed() {
		return errors.New("ExitPlan: already closed")
	}
	e.ClosedAt = &now
	e.UpdatedAt = now
	return nil
}
