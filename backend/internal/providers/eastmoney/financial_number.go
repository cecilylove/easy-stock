package eastmoney

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// financialNumber preserves presence and validity without changing the shared
// flexibleFloat decoder used by non-financial endpoints. The zero value is
// missing; a reported numeric zero is valid.
type financialNumber struct {
	value float64
	valid bool
}

func (number *financialNumber) UnmarshalJSON(data []byte) error {
	*number = financialNumber{}
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		return nil
	}
	text := string(data)
	if data[0] == '"' {
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
	} else if data[0] != '-' && (data[0] < '0' || data[0] > '9') {
		return nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	number.value, number.valid = value, true
	return nil
}
