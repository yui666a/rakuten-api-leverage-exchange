package entity

import (
	"encoding/json"
	"fmt"
	"strconv"
)

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

// Rakuten API may return numeric fields as JSON strings (e.g. "8698.2") for some symbols.
// Accept both string and number forms.
func (p *Position) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID              int64          `json:"id"`
		SymbolID        int64          `json:"symbolId"`
		PositionStatus  PositionStatus `json:"positionStatus"`
		OrderSide       OrderSide      `json:"orderSide"`
		Price           flexFloat      `json:"price"`
		Amount          flexFloat      `json:"amount"`
		RemainingAmount flexFloat      `json:"remainingAmount"`
		Leverage        flexFloat      `json:"leverage"`
		FloatingProfit  flexFloat      `json:"floatingProfit"`
		Profit          flexFloat      `json:"profit"`
		BestPrice       flexFloat      `json:"bestPrice"`
		OrderID         int64          `json:"orderId"`
		CreatedAt       int64          `json:"createdAt"`
	}
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*p = Position{
		ID:              r.ID,
		SymbolID:        r.SymbolID,
		PositionStatus:  r.PositionStatus,
		OrderSide:       r.OrderSide,
		Price:           float64(r.Price),
		Amount:          float64(r.Amount),
		RemainingAmount: float64(r.RemainingAmount),
		Leverage:        float64(r.Leverage),
		FloatingProfit:  float64(r.FloatingProfit),
		Profit:          float64(r.Profit),
		BestPrice:       float64(r.BestPrice),
		OrderID:         r.OrderID,
		CreatedAt:       r.CreatedAt,
	}
	return nil
}

// flexFloat decodes a JSON number or numeric string into a float64.
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if s == "" {
			return nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("flexFloat parse %q: %w", s, err)
		}
		*f = flexFloat(v)
		return nil
	}
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = flexFloat(v)
	return nil
}
