package entity

// ExecutionQualityReport summarises live trading execution quality over a
// rolling time window (typically 24 h). Returned by GET /api/v1/execution-quality.
type ExecutionQualityReport struct {
	WindowSec       int64                                  `json:"windowSec"`
	From            int64                                  `json:"fromTimestamp"`
	To              int64                                  `json:"toTimestamp"`
	Trades          ExecutionQualityTrades                 `json:"trades"`
	CircuitBreaker  ExecutionQualityCircuitBreaker         `json:"circuitBreaker"`
}

// ExecutionQualityTrades aggregates the maker/taker mix and JPY costs.
//
// AvgSlippageBps is the trade-count-weighted mean of the per-trade slippage
// vs the contemporaneous mid price (positive means filled worse than mid for
// taker, negative means better — typical maker scenarios). nil when slippage
// could not be computed (no ticker around the trade timestamp).
type ExecutionQualityTrades struct {
	Count           int                                       `json:"count"`
	MakerCount      int                                       `json:"makerCount"`
	TakerCount      int                                       `json:"takerCount"`
	UnknownCount    int                                       `json:"unknownCount"`
	MakerRatio      float64                                   `json:"makerRatio"`
	TotalFeeJPY     float64                                   `json:"totalFeeJpy"`
	AvgSlippageBps  *float64                                  `json:"avgSlippageBps,omitempty"`
	ByOrderBehavior map[string]ExecutionQualityBehaviorBucket `json:"byOrderBehavior,omitempty"`
}

// ExecutionQualityBehaviorBucket is a per-(OPEN/CLOSE) breakdown.
type ExecutionQualityBehaviorBucket struct {
	Count      int     `json:"count"`
	MakerCount int     `json:"makerCount"`
	MakerRatio float64 `json:"makerRatio"`
	FeeJPY     float64 `json:"feeJpy"`
}

// ExecutionQualityCircuitBreaker mirrors the live-mode RiskStatus so the
// report can be consumed independently of /status by clients that only
// care about the execution view.
type ExecutionQualityCircuitBreaker struct {
	Halted     bool   `json:"halted"`
	HaltReason string `json:"haltReason,omitempty"`
}
