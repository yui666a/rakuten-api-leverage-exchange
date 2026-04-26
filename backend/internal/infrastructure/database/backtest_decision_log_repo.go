package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

const backtestDecisionLogDefaultLimit = 500

type backtestDecisionLogRepo struct {
	db *sql.DB
}

// NewBacktestDecisionLogRepository returns a repository.BacktestDecisionLogRepository
// backed by the given *sql.DB. Each row is scoped to a backtest run id; a
// 3-day retention sweep deletes old rows automatically (see usecase/decisionlog).
func NewBacktestDecisionLogRepository(db *sql.DB) repository.BacktestDecisionLogRepository {
	return &backtestDecisionLogRepo{db: db}
}

func (r *backtestDecisionLogRepo) Insert(ctx context.Context, rec entity.DecisionRecord, runID string) error {
	const q = `
		INSERT INTO backtest_decision_log (
			backtest_run_id,
			bar_close_at, sequence_in_bar, trigger_kind,
			symbol_id, currency_pair, primary_interval,
			stance, last_price,
			signal_action, signal_confidence, signal_reason,
			risk_outcome, risk_reason,
			book_gate_outcome, book_gate_reason,
			order_outcome, order_id, executed_amount, executed_price, order_error,
			closed_position_id, opened_position_id,
			indicators_json, higher_tf_indicators_json,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	if _, err := r.db.ExecContext(ctx, q,
		runID,
		rec.BarCloseAt, rec.SequenceInBar, rec.TriggerKind,
		rec.SymbolID, rec.CurrencyPair, rec.PrimaryInterval,
		rec.Stance, rec.LastPrice,
		rec.SignalAction, rec.SignalConfidence, rec.SignalReason,
		rec.RiskOutcome, rec.RiskReason,
		rec.BookGateOutcome, rec.BookGateReason,
		rec.OrderOutcome, rec.OrderID, rec.ExecutedAmount, rec.ExecutedPrice, rec.OrderError,
		rec.ClosedPositionID, rec.OpenedPositionID,
		rec.IndicatorsJSON, rec.HigherTFIndicatorsJSON,
		rec.CreatedAt,
	); err != nil {
		return fmt.Errorf("backtest_decision_log insert: %w", err)
	}
	return nil
}

func (r *backtestDecisionLogRepo) ListByRun(ctx context.Context, runID string, limit int, cursor int64) ([]entity.DecisionRecord, int64, error) {
	if limit <= 0 {
		limit = backtestDecisionLogDefaultLimit
	}
	args := []any{runID}
	where := "backtest_run_id = ?"
	if cursor > 0 {
		where += " AND id < ?"
		args = append(args, cursor)
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
		       created_at
		FROM backtest_decision_log
		WHERE %s
		ORDER BY id DESC
		LIMIT ?
	`, where)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("backtest_decision_log list: %w", err)
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
			&rec.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("backtest_decision_log scan: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("backtest_decision_log rows: %w", err)
	}

	var next int64
	if len(out) == limit {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

func (r *backtestDecisionLogRepo) DeleteByRun(ctx context.Context, runID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM backtest_decision_log WHERE backtest_run_id = ?`, runID)
	if err != nil {
		return 0, fmt.Errorf("backtest_decision_log delete by run: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("backtest_decision_log rows affected: %w", err)
	}
	return n, nil
}

func (r *backtestDecisionLogRepo) DeleteOlderThan(ctx context.Context, cutoff int64) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM backtest_decision_log WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("backtest_decision_log delete older: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("backtest_decision_log rows affected: %w", err)
	}
	return n, nil
}
