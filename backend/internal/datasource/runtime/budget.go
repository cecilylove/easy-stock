// Package runtime contains capability execution rules shared by access services.
// Cache TTLs and retry ownership remain explicit per consumer and adapter.
package runtime

import (
	"context"
	"time"
)

// Budget never extends a caller's deadline. It is intentionally not a retry
// loop: existing protocol adapters own their bounded transport behavior.
func Budget(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	if duration <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, duration)
}
func Call[T any](ctx context.Context, budget time.Duration, fetch func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	child, cancel := Budget(ctx, budget)
	defer cancel()
	return fetch(child)
}
