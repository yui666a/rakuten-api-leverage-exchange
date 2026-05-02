package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

const decisionLogDefaultLimit = 200

// decisionLogRepo persists DecisionRecord rows into the live `decision_log`
// table. Read-side methods order newest-first by id (autoincrement matches
// insertion order, which matches creation time for a single-writer pipeline).
type decisionLogRepo struct {
	db *sql.DB
}

// NewDecisionLogRepository returns a repository.DecisionLogRepository backed
// by the given *sql.DB. The DB must already have the `decision_log` table
// (run RunMigrations first).
func NewDecisionLogRepository(db *sql.DB) repository.DecisionLogRepository {
	return &decisionLogRepo{db: db}
}

func (r *decisionLogRepo) Insert(ctx context.Context, rec entity.DecisionRecord) error {
	_, err := r.InsertAndID(ctx, rec)
	return err
}

func (r *decisionLogRepo) InsertAndID(ctx context.Context, rec entity.DecisionRecord) (int64, error) {
	const q = `
		INSERT INTO decision_log (
			bar_close_at, sequence_in_bar, trigger_kind,
			symbol_id, currency_pair, primary_interval,
			stance, last_price,
			signal_action, signal_confidence, signal_reason,
			risk_outcome, risk_reason,
			book_gate_outcome, book_gate_reason,
			order_outcome, order_id, executed_amount, executed_price, order_error,
			closed_position_id, opened_position_id,
			indicators_json, higher_tf_indicators_json,
			signal_direction, signal_strength,
			decision_intent, decision_side, decision_reason,
			exit_policy_outcome,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	res, err := r.db.ExecContext(ctx, q,
		rec.BarCloseAt, rec.SequenceInBar, rec.TriggerKind,
		rec.SymbolID, rec.CurrencyPair, rec.PrimaryInterval,
		rec.Stance, rec.LastPrice,
		rec.SignalAction, rec.SignalConfidence, rec.SignalReason,
		rec.RiskOutcome, rec.RiskReason,
		rec.BookGateOutcome, rec.BookGateReason,
		rec.OrderOutcome, rec.OrderID, rec.ExecutedAmount, rec.ExecutedPrice, rec.OrderError,
		rec.ClosedPositionID, rec.OpenedPositionID,
		rec.IndicatorsJSON, rec.HigherTFIndicatorsJSON,
		rec.SignalDirection, rec.SignalStrength,
		rec.DecisionIntent, rec.DecisionSide, rec.DecisionReason,
		rec.ExitPolicyOutcome,
		rec.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("decision_log insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("decision_log insert last id: %w", err)
	}
	return id, nil
}

func (r *decisionLogRepo) Update(ctx context.Context, rec entity.DecisionRecord) error {
	const q = `
		UPDATE decision_log SET
			bar_close_at = ?, sequence_in_bar = ?, trigger_kind = ?,
			symbol_id = ?, currency_pair = ?, primary_interval = ?,
			stance = ?, last_price = ?,
			signal_action = ?, signal_confidence = ?, signal_reason = ?,
			risk_outcome = ?, risk_reason = ?,
			book_gate_outcome = ?, book_gate_reason = ?,
			order_outcome = ?, order_id = ?, executed_amount = ?, executed_price = ?, order_error = ?,
			closed_position_id = ?, opened_position_id = ?,
			indicators_json = ?, higher_tf_indicators_json = ?,
			signal_direction = ?, signal_strength = ?,
			decision_intent = ?, decision_side = ?, decision_reason = ?,
			exit_policy_outcome = ?,
			created_at = ?
		WHERE id = ?
	`
	res, err := r.db.ExecContext(ctx, q,
		rec.BarCloseAt, rec.SequenceInBar, rec.TriggerKind,
		rec.SymbolID, rec.CurrencyPair, rec.PrimaryInterval,
		rec.Stance, rec.LastPrice,
		rec.SignalAction, rec.SignalConfidence, rec.SignalReason,
		rec.RiskOutcome, rec.RiskReason,
		rec.BookGateOutcome, rec.BookGateReason,
		rec.OrderOutcome, rec.OrderID, rec.ExecutedAmount, rec.ExecutedPrice, rec.OrderError,
		rec.ClosedPositionID, rec.OpenedPositionID,
		rec.IndicatorsJSON, rec.HigherTFIndicatorsJSON,
		rec.SignalDirection, rec.SignalStrength,
		rec.DecisionIntent, rec.DecisionSide, rec.DecisionReason,
		rec.ExitPolicyOutcome,
		rec.CreatedAt,
		rec.ID,
	)
	if err != nil {
		return fmt.Errorf("decision_log update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("decision_log update rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("decision_log update: id %d not found", rec.ID)
	}
	return nil
}

func (r *decisionLogRepo) List(ctx context.Context, f repository.DecisionLogFilter) ([]entity.DecisionRecord, int64, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = decisionLogDefaultLimit
	}

	args := make([]any, 0, 5)
	where := "1=1"
	if f.SymbolID > 0 {
		where += " AND symbol_id = ?"
		args = append(args, f.SymbolID)
	}
	if f.From > 0 {
		where += " AND bar_close_at >= ?"
		args = append(args, f.From)
	}
	if f.To > 0 {
		where += " AND bar_close_at <= ?"
		args = append(args, f.To)
	}
	if f.Cursor > 0 {
		where += " AND id < ?"
		args = append(args, f.Cursor)
	}
	args = append(args, limit)

	q := fmt.Sprintf(`
		SELECT id, bar_close_at, sequence_in_bar, trigger_kind,
		       symbol_id, currency_pair, primary_interval,
		       stance, last_price,
		       signal_action, signal_confidence, signal_reason,
		       risk_outcome, risk_reason,
		       book_gate_outcome, book_gate_reason,
		       order_outcome, order_id, executed_amount, executed_price, order_error,
		       closed_position_id, opened_position_id,
		       indicators_json, higher_tf_indicators_json,
		       signal_direction, signal_strength,
		       decision_intent, decision_side, decision_reason,
		       exit_policy_outcome,
		       created_at
		FROM decision_log
		WHERE %s
		ORDER BY id DESC
		LIMIT ?
	`, where)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("decision_log list: %w", err)
	}
	defer rows.Close()

	out := make([]entity.DecisionRecord, 0, limit)
	for rows.Next() {
		var rec entity.DecisionRecord
		if err := rows.Scan(
			&rec.ID, &rec.BarCloseAt, &rec.SequenceInBar, &rec.TriggerKind,
			&rec.SymbolID, &rec.CurrencyPair, &rec.PrimaryInterval,
			&rec.Stance, &rec.LastPrice,
			&rec.SignalAction, &rec.SignalConfidence, &rec.SignalReason,
			&rec.RiskOutcome, &rec.RiskReason,
			&rec.BookGateOutcome, &rec.BookGateReason,
			&rec.OrderOutcome, &rec.OrderID, &rec.ExecutedAmount, &rec.ExecutedPrice, &rec.OrderError,
			&rec.ClosedPositionID, &rec.OpenedPositionID,
			&rec.IndicatorsJSON, &rec.HigherTFIndicatorsJSON,
			&rec.SignalDirection, &rec.SignalStrength,
			&rec.DecisionIntent, &rec.DecisionSide, &rec.DecisionReason,
			&rec.ExitPolicyOutcome,
			&rec.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("decision_log scan: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("decision_log rows: %w", err)
	}

	var next int64
	if len(out) == limit {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}
