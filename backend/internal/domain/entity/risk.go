package entity

// RiskConfig はリスク管理のパラメータ。
type RiskConfig struct {
	MaxPositionAmount float64 `json:"maxPositionAmount"` // 同時ポジション上限（円）
	MaxDailyLoss      float64 `json:"maxDailyLoss"`      // 日次損失上限（円）
	StopLossPercent   float64 `json:"stopLossPercent"`    // 損切りライン（%）
	TakeProfitPercent float64 `json:"takeProfitPercent"`  // 利確ライン（%）
	InitialCapital    float64 `json:"initialCapital"`     // 軍資金（円）
}

// OrderProposal はRisk Managerに承認を求める注文提案。
type OrderProposal struct {
	SymbolID   int64
	Side       OrderSide
	OrderType  OrderType
	Amount     float64  // 数量
	Price      float64  // 概算価格（成行の場合はBestAsk/BestBid）
	IsClose    bool     // 決済注文かどうか
	PositionID *int64   // 決済対象ポジションID
}

// RiskCheckResult はRisk Managerの判定結果。
type RiskCheckResult struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}
