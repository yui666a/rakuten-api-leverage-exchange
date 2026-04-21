package entity

// RiskConfig はリスク管理のパラメータ。
type RiskConfig struct {
	MaxPositionAmount     float64 `json:"maxPositionAmount"`     // 同時ポジション上限（円）
	MaxDailyLoss          float64 `json:"maxDailyLoss"`          // 日次損失上限（円）
	StopLossPercent       float64 `json:"stopLossPercent"`       // 損切りライン（%）— ATR未使用時のフォールバック
	StopLossATRMultiplier float64 `json:"stopLossAtrMultiplier"` // ATR基準の損切り倍率（0=固定%を使用）
	// TrailingATRMultiplier: >0 ならトレイリングストップのリバーサル距離を
	// ATR × この倍率 で算出する (ATR ベース)。0 なら StopLossPercent を使う
	// 従来挙動 (エントリー価格 × %)。両方設定時は「より遠い方＝保守的」を
	// 採用し、ノイズによる早期決済を抑える。
	TrailingATRMultiplier float64 `json:"trailingAtrMultiplier"`
	TakeProfitPercent     float64 `json:"takeProfitPercent"`    // 利確ライン（%）
	InitialCapital        float64 `json:"initialCapital"`       // 軍資金（円）
	MaxConsecutiveLosses  int     `json:"maxConsecutiveLosses"` // 連敗上限（0=無効）
	CooldownMinutes       int     `json:"cooldownMinutes"`      // 冷却期間（分）
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
