package backtest

import (
	"bytes"
	"testing"
	"time"
)

func TestNewULIDAt_LengthAndOrder(t *testing.T) {
	entropyA := bytes.NewReader(bytes.Repeat([]byte{0x00}, 10))
	entropyB := bytes.NewReader(bytes.Repeat([]byte{0x00}, 10))

	a, err := NewULIDAt(time.UnixMilli(1_770_000_000_000), entropyA)
	if err != nil {
		t.Fatalf("new ulid a: %v", err)
	}
	b, err := NewULIDAt(time.UnixMilli(1_770_000_000_001), entropyB)
	if err != nil {
		t.Fatalf("new ulid b: %v", err)
	}

	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("ulid length must be 26: a=%d b=%d", len(a), len(b))
	}
	if !(a < b) {
		t.Fatalf("expected lexical order by time: a=%s b=%s", a, b)
	}
}
