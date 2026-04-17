package repository

import "errors"

// ErrParentResultSelfReference is returned by BacktestResultRepository.Save
// when the result's ParentResultID references its own ID.
//
// Use errors.Is to detect this error even after wrapping with fmt.Errorf("...: %w", err).
var ErrParentResultSelfReference = errors.New("backtest_result: parent_result_id cannot equal id")

// ErrParentResultNotFound is returned by BacktestResultRepository.Save
// when the result's ParentResultID points to a row that does not exist.
//
// Use errors.Is to detect this error even after wrapping with fmt.Errorf("...: %w", err).
var ErrParentResultNotFound = errors.New("backtest_result: parent_result_id does not reference an existing row")
