package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/exitplan"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

// exitPlanRepo は exitplan.Repository を SQLite で実装する。DB は
// RunMigrations 済みであること。
type exitPlanRepo struct {
	db *sql.DB
}

// NewExitPlanRepository は SQLite 実装を返す。
func NewExitPlanRepository(db *sql.DB) exitplan.Repository {
	return &exitPlanRepo{db: db}
}

func (r *exitPlanRepo) Create(ctx context.Context, plan *exitplan.ExitPlan) error {
	if plan == nil {
		return errors.New("exitPlanRepo.Create: nil plan")
	}
	const q = `
		INSERT INTO exit_plans (
			position_id, symbol_id, side, entry_price,
			sl_percent, sl_atr_multiplier,
			tp_percent,
			trailing_mode, trailing_atr_multiplier,
			trailing_activated, trailing_hwm,
			created_at, updated_at, closed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	res, err := r.db.ExecContext(ctx, q,
		plan.PositionID, plan.SymbolID, string(plan.Side), plan.EntryPrice,
		plan.Policy.StopLoss.Percent, plan.Policy.StopLoss.ATRMultiplier,
		plan.Policy.TakeProfit.Percent,
		int(plan.Policy.Trailing.Mode), plan.Policy.Trailing.ATRMultiplier,
		boolToInt(plan.TrailingActivated), nullableFloat(plan.TrailingHWM),
		plan.CreatedAt, plan.UpdatedAt, nullableInt64(plan.ClosedAt),
	)
	if err != nil {
		return fmt.Errorf("insert exit_plans: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("LastInsertId exit_plans: %w", err)
	}
	plan.ID = id
	return nil
}

func (r *exitPlanRepo) FindByPositionID(ctx context.Context, positionID int64) (*exitplan.ExitPlan, error) {
	const q = `
		SELECT id, position_id, symbol_id, side, entry_price,
		       sl_percent, sl_atr_multiplier,
		       tp_percent,
		       trailing_mode, trailing_atr_multiplier,
		       trailing_activated, trailing_hwm,
		       created_at, updated_at, closed_at
		FROM exit_plans
		WHERE position_id = ?
	`
	row := r.db.QueryRowContext(ctx, q, positionID)
	plan, err := scanExitPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("FindByPositionID: %w", err)
	}
	return plan, nil
}

func (r *exitPlanRepo) ListOpen(ctx context.Context, symbolID int64) ([]*exitplan.ExitPlan, error) {
	const q = `
		SELECT id, position_id, symbol_id, side, entry_price,
		       sl_percent, sl_atr_multiplier,
		       tp_percent,
		       trailing_mode, trailing_atr_multiplier,
		       trailing_activated, trailing_hwm,
		       created_at, updated_at, closed_at
		FROM exit_plans
		WHERE symbol_id = ? AND closed_at IS NULL
		ORDER BY id ASC
	`
	rows, err := r.db.QueryContext(ctx, q, symbolID)
	if err != nil {
		return nil, fmt.Errorf("ListOpen query: %w", err)
	}
	defer rows.Close()
	var out []*exitplan.ExitPlan
	for rows.Next() {
		plan, err := scanExitPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("ListOpen scan: %w", err)
		}
		out = append(out, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListOpen iter: %w", err)
	}
	return out, nil
}

func (r *exitPlanRepo) UpdateTrailing(ctx context.Context, planID int64, hwm float64, activated bool, updatedAt int64) error {
	const q = `
		UPDATE exit_plans
		SET trailing_activated = ?, trailing_hwm = ?, updated_at = ?
		WHERE id = ? AND closed_at IS NULL
	`
	res, err := r.db.ExecContext(ctx, q, boolToInt(activated), hwm, updatedAt, planID)
	if err != nil {
		return fmt.Errorf("UpdateTrailing: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("UpdateTrailing: plan id=%d not found or already closed", planID)
	}
	return nil
}

func (r *exitPlanRepo) Close(ctx context.Context, planID int64, closedAt int64) error {
	const q = `
		UPDATE exit_plans
		SET closed_at = ?, updated_at = ?
		WHERE id = ? AND closed_at IS NULL
	`
	res, err := r.db.ExecContext(ctx, q, closedAt, closedAt, planID)
	if err != nil {
		return fmt.Errorf("Close: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("Close: plan id=%d not found or already closed", planID)
	}
	return nil
}

// rowScanner は *sql.Row と *sql.Rows の Scan を共通化する最小インタフェース。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanExitPlan(s rowScanner) (*exitplan.ExitPlan, error) {
	var (
		id              int64
		positionID      int64
		symbolID        int64
		side            string
		entryPrice      float64
		slPercent       float64
		slATRMult       float64
		tpPercent       float64
		trailingMode    int
		trailingATRMult float64
		trailingAct     int
		trailingHWM     sql.NullFloat64
		createdAt       int64
		updatedAt       int64
		closedAt        sql.NullInt64
	)
	if err := s.Scan(
		&id, &positionID, &symbolID, &side, &entryPrice,
		&slPercent, &slATRMult,
		&tpPercent,
		&trailingMode, &trailingATRMult,
		&trailingAct, &trailingHWM,
		&createdAt, &updatedAt, &closedAt,
	); err != nil {
		return nil, err
	}
	plan := &exitplan.ExitPlan{
		ID:         id,
		PositionID: positionID,
		SymbolID:   symbolID,
		Side:       entity.OrderSide(side),
		EntryPrice: entryPrice,
		Policy: risk.RiskPolicy{
			StopLoss:   risk.StopLossSpec{Percent: slPercent, ATRMultiplier: slATRMult},
			TakeProfit: risk.TakeProfitSpec{Percent: tpPercent},
			Trailing:   risk.TrailingSpec{Mode: risk.TrailingMode(trailingMode), ATRMultiplier: trailingATRMult},
		},
		TrailingActivated: trailingAct == 1,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}
	if trailingHWM.Valid {
		v := trailingHWM.Float64
		plan.TrailingHWM = &v
	}
	if closedAt.Valid {
		v := closedAt.Int64
		plan.ClosedAt = &v
	}
	return plan, nil
}

func nullableFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
