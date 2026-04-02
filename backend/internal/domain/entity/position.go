package entity

type PositionStatus string

const (
	PositionStatusOpen            PositionStatus = "OPEN"
	PositionStatusPartiallyClosed PositionStatus = "PARTIALLY_CLOSED"
)

type Position struct {
	ID              int64          `json:"id"`
	SymbolID        int64          `json:"symbolId"`
	PositionStatus  PositionStatus `json:"positionStatus"`
	OrderSide       OrderSide      `json:"orderSide"`
	Price           float64        `json:"price"`
	Amount          float64        `json:"amount"`
	RemainingAmount float64        `json:"remainingAmount"`
	Leverage        float64        `json:"leverage"`
	FloatingProfit  float64        `json:"floatingProfit"`
	Profit          float64        `json:"profit"`
	BestPrice       float64        `json:"bestPrice"`
	OrderID         int64          `json:"orderId"`
	CreatedAt       int64          `json:"createdAt"`
}
