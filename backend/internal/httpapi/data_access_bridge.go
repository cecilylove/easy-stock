package httpapi

import (
	"context"
	"easy-stock/backend/internal/foundation"
)

// Background consumers reuse routed prices without a page cache or extra fallback.
type kLineAccess func(context.Context, string, string, int) ([]foundation.KLine, error)

func (f kLineAccess) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	return f(ctx, symbol, period, limit)
}
