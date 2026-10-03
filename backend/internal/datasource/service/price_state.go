package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
)

// PriceRouteFailure tracks only consecutive network/capability failures. No
// data for one instrument must never suppress other instruments on that route.
type PriceRouteFailure struct {
	failures int
	retryAt  time.Time
}

type PriceRouteState struct {
	mu       sync.Mutex
	failures map[string]PriceRouteFailure
	now      func() time.Time
}

// NewPriceRouteState owns failures after construction. A caller retaining the
// map may inspect it only while no requests are running; the optional map keeps
// legacy HTTP test fixtures readable during the migration.
func NewPriceRouteState(now func() time.Time, failures map[string]PriceRouteFailure) *PriceRouteState {
	if failures == nil {
		failures = make(map[string]PriceRouteFailure)
	}
	if now == nil {
		now = time.Now
	}
	return &PriceRouteState{failures: failures, now: now}
}

func (r *PriceRouteState) Ready(key string) bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.now().Before(r.failures[key].retryAt)
}

func (r *PriceRouteState) Record(key string, failed bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !failed {
		delete(r.failures, key)
		return
	}
	entry := r.failures[key]
	entry.failures++
	if entry.failures >= 2 {
		entry.retryAt = r.now().Add(time.Duration(min(120, 30*(entry.failures-1))) * time.Second)
	}
	r.failures[key] = entry
}

// MarketPriceFailure classifies typed failures, never their English wording.
// Legacy injected providers may still return untyped transport errors.
func MarketPriceFailure(err error) bool {
	if err == nil || errors.Is(err, foundation.ErrPriceNoData) || errors.Is(err, foundation.ErrInvalidPriceData) || errors.Is(err, context.Canceled) {
		return false
	}
	var capability *contracts.Error
	if errors.As(err, &capability) {
		switch capability.Kind {
		case contracts.Unsupported, contracts.NoData, contracts.InvalidResponse, contracts.Unauthorized, contracts.Canceled:
			return false
		case contracts.RateLimited, contracts.UpstreamFailure, contracts.TimedOut:
			return true
		}
	}
	var status *foundation.PriceHTTPStatusError
	if errors.As(err, &status) {
		return status.StatusCode == 429 || status.StatusCode >= 500
	}
	var syntax *json.SyntaxError
	var shape *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &shape) {
		return false
	}
	return true
}

func PriceFailureCapability(capability, symbol string, err error) string {
	if MarketPriceFailure(err) {
		return capability
	}
	return capability + ":symbol:" + symbol
}
