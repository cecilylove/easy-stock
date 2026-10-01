package foundation

import (
	"errors"
	"fmt"
)

// ErrPriceNoData is a completed price response without the requested instrument
// or price series. It is not evidence of a market-wide transport outage.
// Wrappers must preserve this identity with %w; missing qfq/hfq never permits
// substituting a different key or convention.
var ErrPriceNoData = errors.New("requested price instrument or series has no data")

// ErrInvalidPriceData distinguishes completed but unusable instrument data
// from transport failures; it must not trip a whole-market price breaker.
var ErrInvalidPriceData = errors.New("requested price data failed validation")

// PriceHTTPStatusError permits classification by actual status, not by parsing
// an upstream body or an English error message. 404 is not a transport outage;
// 429 and 5xx are upstream service failures.
type PriceHTTPStatusError struct {
	Provider   string
	StatusCode int
}

func (e *PriceHTTPStatusError) Error() string {
	return fmt.Sprintf("%s price http status %d", e.Provider, e.StatusCode)
}
