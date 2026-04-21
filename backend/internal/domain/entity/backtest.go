package entity

// BacktestConfig defines execution parameters for a single backtest run.
type BacktestConfig struct {
	Symbol           string  `json:"symbol"`
	SymbolID         int64   `json:"symbolId"`
	PrimaryInterval  string  `json:"primaryInterval"`
	HigherTFInterval string  `json:"higherTfInterval"`
	FromTimestamp    int64   `json:"fromTimestamp"`
	ToTimestamp      int64   `json:"toTimestamp"`
	InitialBalance   float64 `json:"initialBalance"`
	SpreadPercent    float64 `json:"spreadPercent"`
	DailyCarryCost   float64 `json:"dailyCarryCost"`
	SlippagePercent  float64 `json:"slippagePercent"`
}

// BacktestSummary stores fixed output metrics for a run.
type BacktestSummary struct {
	PeriodFrom         int64   `json:"periodFrom"`
	PeriodTo           int64   `json:"periodTo"`
	InitialBalance     float64 `json:"initialBalance"`
	FinalBalance       float64 `json:"finalBalance"`
	TotalReturn        float64 `json:"totalReturn"`
	TotalTrades        int     `json:"totalTrades"`
	WinTrades          int     `json:"winTrades"`
	LossTrades         int     `json:"lossTrades"`
	WinRate            float64 `json:"winRate"`
	ProfitFactor       float64 `json:"profitFactor"`
	MaxDrawdown        float64 `json:"maxDrawdown"`
	MaxDrawdownBalance float64 `json:"maxDrawdownBalance"`
	SharpeRatio        float64 `json:"sharpeRatio"`
	AvgHoldSeconds     int64   `json:"avgHoldSeconds"`
	TotalCarryingCost  float64 `json:"totalCarryingCost"`
	TotalSpreadCost    float64 `json:"totalSpreadCost"`
	// BiweeklyWinRate is the mean of 14-day sliding-window win rates across the
	// backtest period, expressed on a 0-100 scale (matches WinRate scale).
	// Windows with fewer than 3 closed trades are penalized to 0 (not skipped);
	// if the coverage ratio (windows with >=3 trades / total windows) is below 50%,
	// the overall value is reported as 0 to signal low reliability.
	BiweeklyWinRate float64 `json:"biweeklyWinRate"`

	// ByExitReason buckets trades by their exit reason (e.g. "reverse_signal",
	// "stop_loss", "take_profit", "end_of_test"). Empty map for legacy rows
	// persisted before PR-1.
	ByExitReason map[string]SummaryBreakdown `json:"byExitReason,omitempty"`

	// BySignalSource buckets trades by their originating signal source
	// ("trend_follow" / "contrarian" / "breakout" / "unknown"). Empty map for
	// legacy rows persisted before PR-1.
	BySignalSource map[string]SummaryBreakdown `json:"bySignalSource,omitempty"`
}

// SummaryBreakdown holds aggregated metrics for a subset of trades grouped by
// some key (exit reason, signal source, regime, etc.). Values are scoped to
// the subset only — TotalPnL/AvgPnL/ProfitFactor are computed within the
// subset, not relative to the overall run.
type SummaryBreakdown struct {
	Trades       int     `json:"trades"`
	WinTrades    int     `json:"winTrades"`
	LossTrades   int     `json:"lossTrades"`
	WinRate      float64 `json:"winRate"`      // 0-100 scale, matches BacktestSummary.WinRate
	TotalPnL     float64 `json:"totalPnL"`     // JPY
	AvgPnL       float64 `json:"avgPnL"`       // JPY per trade; 0 when Trades == 0
	ProfitFactor float64 `json:"profitFactor"` // sum(wins) / |sum(losses)|; 0 if no losses
}

// BacktestTradeRecord is a closed trade record produced by the simulator.
type BacktestTradeRecord struct {
	TradeID      int64   `json:"tradeId"`
	SymbolID     int64   `json:"symbolId"`
	EntryTime    int64   `json:"entryTime"`
	ExitTime     int64   `json:"exitTime"`
	Side         string  `json:"side"`
	EntryPrice   float64 `json:"entryPrice"`
	ExitPrice    float64 `json:"exitPrice"`
	Amount       float64 `json:"amount"`
	PnL          float64 `json:"pnl"`
	PnLPercent   float64 `json:"pnlPercent"`
	CarryingCost float64 `json:"carryingCost"`
	SpreadCost   float64 `json:"spreadCost"`
	ReasonEntry  string  `json:"reasonEntry"`
	ReasonExit   string  `json:"reasonExit"`
}

// BacktestResult is the persisted aggregate output of one run.
type BacktestResult struct {
	ID        string                `json:"id"`
	CreatedAt int64                 `json:"createdAt"`
	Config    BacktestConfig        `json:"config"`
	Summary   BacktestSummary       `json:"summary"`
	Trades    []BacktestTradeRecord `json:"trades,omitempty"`

	// PDCA metadata. Introduced by the PDCA strategy optimizer (see design doc §5).
	// ProfileName identifies the StrategyProfile that produced this run (empty for legacy rows).
	ProfileName string `json:"profileName"`
	// PDCACycleID links this run to a PDCA cycle document/ID (empty when unassigned).
	PDCACycleID string `json:"pdcaCycleId,omitempty"`
	// Hypothesis records the experimenter's hypothesis for this run.
	Hypothesis string `json:"hypothesis,omitempty"`
	// ParentResultID points to the previous run in a comparison chain.
	// nil means "root node" (no parent).
	ParentResultID *string `json:"parentResultId,omitempty"`
}

// PeriodSpec describes a single labelled time window for a multi-period
// backtest. From/To are "YYYY-MM-DD" strings on the handler boundary; the
// runner parses them into millisecond timestamps.
type PeriodSpec struct {
	Label string `json:"label"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// LabeledBacktestResult pairs a BacktestResult with the PeriodSpec.Label that
// produced it, so the caller can correlate rows back to the user's request.
type LabeledBacktestResult struct {
	Label  string         `json:"label"`
	Result BacktestResult `json:"result"`
}

// MultiPeriodAggregate summarises the N per-period results as one scalar set.
// The RobustnessScore = GeomMeanReturn - ReturnStdDev is the simple one-shot
// promotion heuristic documented in docs/design/plans/
// 2026-04-21-pr2-multi-period-backtest.md.
//
// Ruin handling: when any period returns <= -1.0 (total bankruptcy), the
// geometric mean is not well-defined. We deliberately set
// GeomMeanReturn = NaN so downstream consumers cannot accidentally use it
// as a score, and clamp AllPositive=false to signal the ruin path.
type MultiPeriodAggregate struct {
	GeomMeanReturn  float64 `json:"geomMeanReturn"`
	ReturnStdDev    float64 `json:"returnStdDev"`
	WorstReturn     float64 `json:"worstReturn"`
	BestReturn      float64 `json:"bestReturn"`
	WorstDrawdown   float64 `json:"worstDrawdown"`
	AllPositive     bool    `json:"allPositive"`
	RobustnessScore float64 `json:"robustnessScore"`
}

// MultiPeriodResult is the persisted output of a single multi-period run: N
// labelled child results plus one aggregate. The child BacktestResults are
// saved individually into backtest_results; this envelope lives in a separate
// multi_period_results table keyed by the ID below.
type MultiPeriodResult struct {
	ID          string                  `json:"id"`
	CreatedAt   int64                   `json:"createdAt"`
	ProfileName string                  `json:"profileName"`
	Periods     []LabeledBacktestResult `json:"periods"`
	Aggregate   MultiPeriodAggregate    `json:"aggregate"`

	PDCACycleID    string  `json:"pdcaCycleId,omitempty"`
	Hypothesis     string  `json:"hypothesis,omitempty"`
	ParentResultID *string `json:"parentResultId,omitempty"`
}
