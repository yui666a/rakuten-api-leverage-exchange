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
