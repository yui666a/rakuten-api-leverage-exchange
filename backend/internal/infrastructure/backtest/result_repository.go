package backtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// resultColumns は backtest_results テーブルの全カラムを列挙した共通定義。
// INSERT/SELECT の双方でこの定数を参照することで列リストの重複を排除する。
//
// 不変条件: このカラム順序は scanResultRow の Scan 引数順序および Save の
// INSERT バインド引数順序と完全に一致していなければならない。変更時は両方を
// 必ず同時に更新すること。
const resultColumns = `id, created_at, symbol, symbol_id, primary_interval, higher_tf_interval,
	from_ts, to_ts, initial_balance, final_balance, total_return, total_trades,
	win_trades, loss_trades, win_rate, profit_factor, max_drawdown, max_drawdown_balance,
	sharpe_ratio, avg_hold_seconds, total_carrying_cost, total_spread_cost,
	profile_name, pdca_cycle_id, hypothesis, parent_result_id, biweekly_win_rate,
	breakdown_json,
	drawdown_periods_json, drawdown_threshold, time_in_market_ratio, longest_flat_streak_bars,
	expectancy_per_trade, avg_win_jpy, avg_loss_jpy`

// resultColumnPlaceholders は resultColumns と同じ個数 (35) の INSERT プレースホルダ。
// resultColumns のカラム数を変更した場合はここも必ず同期させること。
const resultColumnPlaceholders = `?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?`

type ResultRepository struct {
	db *sql.DB
}

func NewResultRepository(db *sql.DB) *ResultRepository {
	return &ResultRepository{db: db}
}

func (r *ResultRepository) Save(ctx context.Context, result entity.BacktestResult) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Application-layer integrity checks on parent_result_id (design doc §5.1).
	// Self-reference check happens first so we short-circuit without hitting the DB.
	if result.ParentResultID != nil {
		if *result.ParentResultID == result.ID {
			return fmt.Errorf("save backtest result: %w", repository.ErrParentResultSelfReference)
		}
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM backtest_results WHERE id = ?`, *result.ParentResultID).Scan(&exists)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("save backtest result: %w", repository.ErrParentResultNotFound)
			}
			return fmt.Errorf("check parent existence: %w", err)
		}
	}

	parentID := sql.NullString{}
	if result.ParentResultID != nil {
		parentID = sql.NullString{String: *result.ParentResultID, Valid: true}
	}

	// breakdown_json は両マップが空なら NULL (レガシー行と同形式)。
	// 片方でも非空ならまとめて 1 本の JSON オブジェクトにシリアライズ。
	breakdownBlob := sql.NullString{}
	if len(result.Summary.ByExitReason) > 0 || len(result.Summary.BySignalSource) > 0 {
		payload := breakdownPayload{
			ByExitReason:   result.Summary.ByExitReason,
			BySignalSource: result.Summary.BySignalSource,
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal breakdown: %w", err)
		}
		breakdownBlob = sql.NullString{String: string(b), Valid: true}
	}

	// PR-3: DrawdownPeriods + UnrecoveredDrawdown を 1 本の JSON にまとめる。
	// レガシー行 (両方ゼロ/nil) は NULL 保存で後方互換。
	ddBlob := sql.NullString{}
	if len(result.Summary.DrawdownPeriods) > 0 || result.Summary.UnrecoveredDrawdown != nil {
		payload := drawdownPayload{
			Periods:     result.Summary.DrawdownPeriods,
			Unrecovered: result.Summary.UnrecoveredDrawdown,
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal drawdowns: %w", err)
		}
		ddBlob = sql.NullString{String: string(b), Valid: true}
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO backtest_results (`+resultColumns+`) VALUES (`+resultColumnPlaceholders+`)`,
		result.ID,
		result.CreatedAt,
		result.Config.Symbol,
		result.Config.SymbolID,
		result.Config.PrimaryInterval,
		result.Config.HigherTFInterval,
		result.Config.FromTimestamp,
		result.Config.ToTimestamp,
		result.Summary.InitialBalance,
		result.Summary.FinalBalance,
		result.Summary.TotalReturn,
		result.Summary.TotalTrades,
		result.Summary.WinTrades,
		result.Summary.LossTrades,
		result.Summary.WinRate,
		result.Summary.ProfitFactor,
		result.Summary.MaxDrawdown,
		result.Summary.MaxDrawdownBalance,
		result.Summary.SharpeRatio,
		result.Summary.AvgHoldSeconds,
		result.Summary.TotalCarryingCost,
		result.Summary.TotalSpreadCost,
		result.ProfileName,
		result.PDCACycleID,
		result.Hypothesis,
		parentID,
		result.Summary.BiweeklyWinRate,
		breakdownBlob,
		ddBlob,
		result.Summary.DrawdownThreshold,
		result.Summary.TimeInMarketRatio,
		result.Summary.LongestFlatStreakBars,
		result.Summary.ExpectancyPerTrade,
		result.Summary.AvgWinJPY,
		result.Summary.AvgLossJPY,
	)
	if err != nil {
		return fmt.Errorf("insert backtest result: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO backtest_trades (
			result_id, trade_id, symbol_id, entry_time, exit_time, side,
			entry_price, exit_price, amount, pnl, pnl_percent, carrying_cost, spread_cost, reason_entry, reason_exit
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare trade insert: %w", err)
	}
	defer stmt.Close()

	for _, tr := range result.Trades {
		if _, err := stmt.ExecContext(
			ctx,
			result.ID,
			tr.TradeID,
			tr.SymbolID,
			tr.EntryTime,
			tr.ExitTime,
			tr.Side,
			tr.EntryPrice,
			tr.ExitPrice,
			tr.Amount,
			tr.PnL,
			tr.PnLPercent,
			tr.CarryingCost,
			tr.SpreadCost,
			tr.ReasonEntry,
			tr.ReasonExit,
		); err != nil {
			return fmt.Errorf("insert trade: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (r *ResultRepository) List(ctx context.Context, filter repository.BacktestResultFilter) ([]entity.BacktestResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	// Dynamic WHERE construction. Only parameterised placeholders are used;
	// the static column names are not derived from caller input.
	var clauses []string
	var args []any

	if filter.ProfileName != "" {
		clauses = append(clauses, "profile_name = ?")
		args = append(args, filter.ProfileName)
	}
	if filter.PDCACycleID != "" {
		clauses = append(clauses, "pdca_cycle_id = ?")
		args = append(args, filter.PDCACycleID)
	}
	// ParentResultID takes precedence over HasParent (design doc §5.3).
	switch {
	case filter.ParentResultID != nil:
		clauses = append(clauses, "parent_result_id = ?")
		args = append(args, *filter.ParentResultID)
	case filter.HasParent != nil && *filter.HasParent:
		clauses = append(clauses, "parent_result_id IS NOT NULL")
	case filter.HasParent != nil && !*filter.HasParent:
		clauses = append(clauses, "parent_result_id IS NULL")
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	args = append(args, limit, offset)

	query := `SELECT ` + resultColumns + ` FROM backtest_results` + where + `
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list backtest results: %w", err)
	}
	defer rows.Close()

	var results []entity.BacktestResult
	for rows.Next() {
		var result entity.BacktestResult
		if err := scanResultRow(rows, &result); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate list rows: %w", err)
	}
	return results, nil
}

func (r *ResultRepository) FindByID(ctx context.Context, id string) (*entity.BacktestResult, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+resultColumns+` FROM backtest_results WHERE id = ?`, id)

	var result entity.BacktestResult
	if err := scanResultRow(row, &result); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	trades, err := r.loadTrades(ctx, id)
	if err != nil {
		return nil, err
	}
	result.Trades = trades
	return &result, nil
}

func (r *ResultRepository) DeleteOlderThan(ctx context.Context, timestamp int64) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM backtest_results WHERE created_at < ?`, timestamp)
	if err != nil {
		return 0, fmt.Errorf("delete old results: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

func (r *ResultRepository) loadTrades(ctx context.Context, resultID string) ([]entity.BacktestTradeRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			trade_id, symbol_id, entry_time, exit_time, side,
			entry_price, exit_price, amount, pnl, pnl_percent, carrying_cost, spread_cost, reason_entry, reason_exit
		FROM backtest_trades
		WHERE result_id = ?
		ORDER BY trade_id ASC
	`, resultID)
	if err != nil {
		return nil, fmt.Errorf("query trades: %w", err)
	}
	defer rows.Close()

	trades := make([]entity.BacktestTradeRecord, 0)
	for rows.Next() {
		var tr entity.BacktestTradeRecord
		if err := rows.Scan(
			&tr.TradeID,
			&tr.SymbolID,
			&tr.EntryTime,
			&tr.ExitTime,
			&tr.Side,
			&tr.EntryPrice,
			&tr.ExitPrice,
			&tr.Amount,
			&tr.PnL,
			&tr.PnLPercent,
			&tr.CarryingCost,
			&tr.SpreadCost,
			&tr.ReasonEntry,
			&tr.ReasonExit,
		); err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		trades = append(trades, tr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trades: %w", err)
	}
	return trades, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

// breakdownPayload は breakdown_json カラムの中身に対応する serialization
// envelope。将来 regime 別・期間別など新たな breakdown を追加しても、
// 既存キーを無視して後方互換に読める形にしておく。
type breakdownPayload struct {
	ByExitReason   map[string]entity.SummaryBreakdown `json:"byExitReason,omitempty"`
	BySignalSource map[string]entity.SummaryBreakdown `json:"bySignalSource,omitempty"`
}

// drawdownPayload は drawdown_periods_json カラムの中身。回復済み episodes と
// 期間末まで未回復の 1 件を 1 本の JSON に詰める。
type drawdownPayload struct {
	Periods     []entity.DrawdownPeriod `json:"periods,omitempty"`
	Unrecovered *entity.DrawdownPeriod  `json:"unrecovered,omitempty"`
}

func scanResultRow(scanner rowScanner, result *entity.BacktestResult) error {
	var parentID sql.NullString
	var breakdownBlob sql.NullString
	var ddBlob sql.NullString
	err := scanner.Scan(
		&result.ID,
		&result.CreatedAt,
		&result.Config.Symbol,
		&result.Config.SymbolID,
		&result.Config.PrimaryInterval,
		&result.Config.HigherTFInterval,
		&result.Config.FromTimestamp,
		&result.Config.ToTimestamp,
		&result.Summary.InitialBalance,
		&result.Summary.FinalBalance,
		&result.Summary.TotalReturn,
		&result.Summary.TotalTrades,
		&result.Summary.WinTrades,
		&result.Summary.LossTrades,
		&result.Summary.WinRate,
		&result.Summary.ProfitFactor,
		&result.Summary.MaxDrawdown,
		&result.Summary.MaxDrawdownBalance,
		&result.Summary.SharpeRatio,
		&result.Summary.AvgHoldSeconds,
		&result.Summary.TotalCarryingCost,
		&result.Summary.TotalSpreadCost,
		&result.ProfileName,
		&result.PDCACycleID,
		&result.Hypothesis,
		&parentID,
		&result.Summary.BiweeklyWinRate,
		&breakdownBlob,
		&ddBlob,
		&result.Summary.DrawdownThreshold,
		&result.Summary.TimeInMarketRatio,
		&result.Summary.LongestFlatStreakBars,
		&result.Summary.ExpectancyPerTrade,
		&result.Summary.AvgWinJPY,
		&result.Summary.AvgLossJPY,
	)
	if err != nil {
		return err
	}
	if parentID.Valid {
		v := parentID.String
		result.ParentResultID = &v
	} else {
		result.ParentResultID = nil
	}
	if breakdownBlob.Valid && breakdownBlob.String != "" {
		var payload breakdownPayload
		// 不正な JSON はレガシー互換のため静かに無視 (breakdown フィールドを空のまま残す)。
		if err := json.Unmarshal([]byte(breakdownBlob.String), &payload); err == nil {
			result.Summary.ByExitReason = payload.ByExitReason
			result.Summary.BySignalSource = payload.BySignalSource
		}
	}
	if ddBlob.Valid && ddBlob.String != "" {
		var payload drawdownPayload
		if err := json.Unmarshal([]byte(ddBlob.String), &payload); err == nil {
			result.Summary.DrawdownPeriods = payload.Periods
			result.Summary.UnrecoveredDrawdown = payload.Unrecovered
		}
	}
	result.Summary.PeriodFrom = result.Config.FromTimestamp
	result.Summary.PeriodTo = result.Config.ToTimestamp
	return nil
}
