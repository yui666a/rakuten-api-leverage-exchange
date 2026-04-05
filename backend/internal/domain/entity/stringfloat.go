package entity

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// StringFloat64 unmarshals a JSON value that may be either a number or a
// string-encoded number (e.g. "12345.67") into a float64.
type StringFloat64 float64

func (sf *StringFloat64) UnmarshalJSON(data []byte) error {
	// Try number first.
	var f float64
	if err := json.Unmarshal(data, &f); err == nil {
		*sf = StringFloat64(f)
		return nil
	}

	// Try quoted string.
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("StringFloat64: cannot unmarshal %s", string(data))
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("StringFloat64: cannot parse %q: %w", s, err)
	}
	*sf = StringFloat64(f)
	return nil
}

func (sf StringFloat64) MarshalJSON() ([]byte, error) {
	return json.Marshal(float64(sf))
}

func (sf StringFloat64) Float64() float64 {
	return float64(sf)
}
