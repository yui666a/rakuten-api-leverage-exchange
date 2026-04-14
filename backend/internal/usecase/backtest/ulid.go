package backtest

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"time"
)

var ulidEncoding = []byte("0123456789ABCDEFGHJKMNPQRSTVWXYZ")

// NewULID generates a ULID string with current time.
func NewULID() (string, error) {
	return NewULIDAt(time.Now(), rand.Reader)
}

// NewULIDAt generates a ULID string using provided time and entropy source.
func NewULIDAt(now time.Time, entropy io.Reader) (string, error) {
	var raw [16]byte
	ts := now.UnixMilli()
	if ts < 0 {
		return "", fmt.Errorf("negative timestamp is not supported")
	}
	// 48-bit millisecond timestamp (big-endian)
	raw[0] = byte(ts >> 40)
	raw[1] = byte(ts >> 32)
	raw[2] = byte(ts >> 24)
	raw[3] = byte(ts >> 16)
	raw[4] = byte(ts >> 8)
	raw[5] = byte(ts)

	if _, err := io.ReadFull(entropy, raw[6:]); err != nil {
		return "", fmt.Errorf("read entropy: %w", err)
	}
	return encodeULID(raw), nil
}

func encodeULID(raw [16]byte) string {
	base := big.NewInt(32)
	n := new(big.Int).SetBytes(raw[:])
	out := make([]byte, 26)
	mod := new(big.Int)

	for i := 25; i >= 0; i-- {
		n.QuoRem(n, base, mod)
		out[i] = ulidEncoding[mod.Int64()]
	}
	return string(out)
}
