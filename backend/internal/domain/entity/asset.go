package entity

type Asset struct {
	Currency     string  `json:"currency"`
	OnhandAmount float64 `json:"onhandAmount"`
}
