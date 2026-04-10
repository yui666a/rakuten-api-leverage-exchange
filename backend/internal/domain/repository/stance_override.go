package repository

import "context"

// StanceOverrideRecord はスタンスオーバーライドの永続化レコード。
type StanceOverrideRecord struct {
	Stance    string
	Reasoning string
	SetAt     int64
	TTLSec    int64
}

// StanceOverrideRepository はスタンスオーバーライドの永続化インターフェース。
type StanceOverrideRepository interface {
	Save(ctx context.Context, record StanceOverrideRecord) error
	Load(ctx context.Context) (*StanceOverrideRecord, error)
	Delete(ctx context.Context) error
}
