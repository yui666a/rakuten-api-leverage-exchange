package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ExecutionQualityRepo persists execution-quality snapshots so the API can
// serve cached values instead of hitting the venue on every request.
type ExecutionQualityRepo struct {
	db *sql.DB
}

func NewExecutionQualityRepo(db *sql.DB) *ExecutionQualityRepo {
	return &ExecutionQualityRepo{db: db}
}

func (r *ExecutionQualityRepo) Save(ctx context.Context, symbolID int64, capturedAt int64, report entity.ExecutionQualityReport) error {
	bucketJSON := []byte("{}")
	if len(report.Trades.ByOrderBehavior) > 0 {
		raw, err := json.Marshal(report.Trades.ByOrderBehavior)
		if err != nil {
			return fmt.Errorf("marshal byOrderBehavior: %w", err)
		}
		bucketJSON = raw
	}
	var avgSlip sql.NullFloat64
	if report.Trades.AvgSlippageBps != nil {
		avgSlip = sql.NullFloat64{Valid: true, Float64: *report.Trades.AvgSlippageBps}
	}
	halted := 0
	if report.CircuitBreaker.Halted {
		halted = 1
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO execution_quality_snapshots (
			symbol_id, captured_at, window_sec, from_ts, to_ts,
			trades_count, maker_count, taker_count, unknown_count,
			maker_ratio, total_fee_jpy, avg_slippage_bps,
			by_order_behavior_json, halted, halt_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		symbolID, capturedAt, report.WindowSec, report.From, report.To,
		report.Trades.Count, report.Trades.MakerCount, report.Trades.TakerCount,
		report.Trades.UnknownCount, report.Trades.MakerRatio, report.Trades.TotalFeeJPY,
		avgSlip, string(bucketJSON), halted, report.CircuitBreaker.HaltReason,
	)
	if err != nil {
		return fmt.Errorf("insert execution_quality_snapshot: %w", err)
	}
	return nil
}

func (r *ExecutionQualityRepo) Latest(ctx context.Context, symbolID int64) (*entity.ExecutionQualityReport, error) {
	var (
		windowSec, fromTs, toTs                              int64
		count, makerCount, takerCount, unknownCount, halted  int
		makerRatio, totalFeeJpy                              float64
		avgSlip                                              sql.NullFloat64
		bucketJSON, haltReason                               string
	)
	err := r.db.QueryRowContext(ctx,
		`SELECT window_sec, from_ts, to_ts,
		        trades_count, maker_count, taker_count, unknown_count,
		        maker_ratio, total_fee_jpy, avg_slippage_bps,
		        by_order_behavior_json, halted, halt_reason
		 FROM execution_quality_snapshots
		 WHERE symbol_id = ?
		 ORDER BY captured_at DESC LIMIT 1`,
		symbolID,
	).Scan(
		&windowSec, &fromTs, &toTs,
		&count, &makerCount, &takerCount, &unknownCount,
		&makerRatio, &totalFeeJpy, &avgSlip,
		&bucketJSON, &halted, &haltReason,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest execution_quality_snapshot: %w", err)
	}

	rep := entity.ExecutionQualityReport{
		WindowSec: windowSec,
		From:      fromTs,
		To:        toTs,
		Trades: entity.ExecutionQualityTrades{
			Count:        count,
			MakerCount:   makerCount,
			TakerCount:   takerCount,
			UnknownCount: unknownCount,
			MakerRatio:   makerRatio,
			TotalFeeJPY:  totalFeeJpy,
		},
		CircuitBreaker: entity.ExecutionQualityCircuitBreaker{
			Halted:     halted == 1,
			HaltReason: haltReason,
		},
	}
	if avgSlip.Valid {
		v := avgSlip.Float64
		rep.Trades.AvgSlippageBps = &v
	}
	var buckets map[string]entity.ExecutionQualityBehaviorBucket
	if err := json.Unmarshal([]byte(bucketJSON), &buckets); err == nil && len(buckets) > 0 {
		rep.Trades.ByOrderBehavior = buckets
	}
	return &rep, nil
}

func (r *ExecutionQualityRepo) PurgeOlderThan(ctx context.Context, cutoffMillis int64) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM execution_quality_snapshots WHERE captured_at < ?`,
		cutoffMillis,
	)
	if err != nil {
		return 0, fmt.Errorf("purge execution_quality_snapshots: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return n, nil
}
