package domain

import "time"

// Market represents a trading market entity
type Market struct {
	Symbol      string    `json:"symbol"`
	LastPrice   float64   `json:"last_price"`
	Volume      float64   `json:"volume"`
	Change24h   float64   `json:"change_24h"`
	High24h     float64   `json:"high_24h"`
	Low24h      float64   `json:"low_24h"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Order represents a trading order
type Order struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Side        string    `json:"side"` // "buy" or "sell"
	Type        string    `json:"type"` // "market" or "limit"
	Price       float64   `json:"price"`
	Amount      float64   `json:"amount"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Account represents user account information
type Account struct {
	ID          string    `json:"id"`
	Balance     float64   `json:"balance"`
	Currency    string    `json:"currency"`
	LockedFunds float64   `json:"locked_funds"`
	UpdatedAt   time.Time `json:"updated_at"`
}
