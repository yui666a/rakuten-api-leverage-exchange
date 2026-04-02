package entity

type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

type OrderType string

const (
	OrderTypeMarket OrderType = "MARKET"
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeStop   OrderType = "STOP"
)

type OrderPattern string

const (
	OrderPatternNormal OrderPattern = "NORMAL"
	OrderPatternOCO    OrderPattern = "OCO"
	OrderPatternIFD    OrderPattern = "IFD"
	OrderPatternIFDOCO OrderPattern = "IFD_OCO"
)

type OrderBehavior string

const (
	OrderBehaviorOpen  OrderBehavior = "OPEN"
	OrderBehaviorClose OrderBehavior = "CLOSE"
)

type OrderStatus string

const (
	OrderStatusWorkingOrder OrderStatus = "WORKING_ORDER"
	OrderStatusPartialFill  OrderStatus = "PARTIAL_FILL"
)

type OrderRequest struct {
	SymbolID     int64        `json:"symbolId"`
	OrderPattern OrderPattern `json:"orderPattern"`
	OrderData    OrderData    `json:"orderData"`
}

type OrderData struct {
	OrderBehavior      OrderBehavior `json:"orderBehavior"`
	PositionID         *int64        `json:"positionId,omitempty"`
	OrderSide          OrderSide     `json:"orderSide"`
	OrderType          OrderType     `json:"orderType"`
	Price              *float64      `json:"price,omitempty"`
	Amount             float64       `json:"amount"`
	OrderExpire        *int64        `json:"orderExpire,omitempty"`
	Leverage           *float64      `json:"leverage,omitempty"`
	CloseBehavior      *string       `json:"closeBehavior,omitempty"`
	PostOnly           *bool         `json:"postOnly,omitempty"`
	IFDCloseLimitPrice *float64      `json:"ifdCloseLimitPrice,omitempty"`
	IFDCloseStopPrice  *float64      `json:"ifdCloseStopPrice,omitempty"`
}

type Order struct {
	ID              int64         `json:"id"`
	SymbolID        int64         `json:"symbolId"`
	OrderBehavior   OrderBehavior `json:"orderBehavior"`
	OrderSide       OrderSide     `json:"orderSide"`
	OrderPattern    OrderPattern  `json:"orderPattern"`
	OrderType       OrderType     `json:"orderType"`
	Price           float64       `json:"price"`
	Amount          float64       `json:"amount"`
	RemainingAmount float64       `json:"remainingAmount"`
	OrderStatus     OrderStatus   `json:"orderStatus"`
	Leverage        float64       `json:"leverage"`
	OrderCreatedAt  int64         `json:"orderCreatedAt"`
}
