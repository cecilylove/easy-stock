package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

type adjustedKLineProvider interface {
	KLineAdjusted(context.Context, string, string, int, string) ([]foundation.KLine, error)
}

// An explicit adjustment is a strict contract, not permission to silently return
// a different provider's adjustment convention when the primary is unavailable.
func (s *Server) loadAdjustedKLine(ctx context.Context, symbol, period string, limit int, adjustment string) ([]foundation.KLine, error) {
	period = strings.TrimSpace(period)
	if adjustment != "none" && adjustment != "qfq" && adjustment != "hfq" {
		return nil, fmt.Errorf("unsupported adjustment")
	}
	if period == "year" {
		limit = min(max(limit, 1), 50)
		months, err := s.loadAdjustedKLine(ctx, symbol, "month", (limit+1)*12, adjustment)
		if err != nil {
			return nil, err
		}
		years := aggregateYearKLines(months, limit)
		if len(years) == 0 {
			return nil, fmt.Errorf("adjusted monthly source returned no usable annual bars")
		}
		return years, nil
	}
	provider, ok := s.kLinePrimary.(adjustedKLineProvider)
	if !ok {
		return nil, fmt.Errorf("current primary source does not support explicit price adjustment")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	bars, err := provider.KLineAdjusted(requestCtx, symbol, period, limit, adjustment)
	if err == nil {
		err = requestCtx.Err()
	}
	if err == nil && len(bars) == 0 {
		err = fmt.Errorf("adjusted source returned no bars")
	}
	if err != nil {
		if shouldObserveFailure(ctx) {
			s.sourceHealth.failure(s.kLinePrimarySourceID, err)
		}
		return nil, fmt.Errorf("指定复权来源暂不可用，不切换到不同复权口径；可稍后重试或选择来源默认")
	}
	label := map[string]string{"none": "不复权", "qfq": "前复权", "hfq": "后复权"}[adjustment]
	for index := range bars {
		bars[index].Meta.FallbackReason = label + "（来源明确指定）"
		s.sourceHealth.success(bars[index].Meta)
	}
	return normalizeKLinePeriod(bars, period), nil
}
