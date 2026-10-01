package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

type adjustedKLineProvider = AdjustedKLineProvider

// Explicit adjustments use Tencent's supplier convention. Retired EastMoney
// provider requests are rejected, never silently relabelled.
func (s *Server) loadAdjustedKLine(ctx context.Context, symbol, period string, limit int, adjustment string) ([]foundation.KLine, error) {
	return s.loadProviderAdjustedKLine(ctx, symbol, period, limit, adjustment, "tencent")
}

func (s *Server) strictKLineProvider(provider string) AdjustedKLineProvider {
	if provider == "tencent" {
		return s.kLineStrictTencent
	}
	return nil
}

// Explicitly injected providers must return the selected supplier identity.
// Never relabel a Tencent response as EastMoney merely because it implements
// the same Go interface or uses the same qfq/hfq parameter names.
func validateStrictKLineIdentity(bars []foundation.KLine, source, adjustment string) error {
	if len(bars) == 0 {
		return fmt.Errorf("%w: strict kline source returned no bars", foundation.ErrPriceNoData)
	}
	for _, bar := range bars {
		if sourceID(bar.Meta.Source) != source || (bar.Meta.Provider != "" && bar.Meta.Provider != source) {
			return fmt.Errorf("%w: strict kline supplier identity mismatch", foundation.ErrInvalidPriceData)
		}
		if bar.Meta.EffectiveAdjustment != "" && bar.Meta.EffectiveAdjustment != adjustment {
			return fmt.Errorf("%w: strict kline effective adjustment mismatch", foundation.ErrInvalidPriceData)
		}
	}
	return nil
}

func (s *Server) loadProviderAdjustedKLine(ctx context.Context, symbol, period string, limit int, adjustment, source string) ([]foundation.KLine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	period = canonicalKLinePeriod(period)
	if adjustment != "none" && adjustment != "qfq" && adjustment != "hfq" {
		return nil, fmt.Errorf("unsupported adjustment")
	}
	if source != "tencent" {
		return nil, fmt.Errorf("unsupported strict provider")
	}
	if period == "year" {
		limit = min(max(limit, 1), 50)
		months, err := s.loadProviderAdjustedKLine(ctx, symbol, "month", (limit+1)*12, adjustment, source)
		if err != nil {
			return nil, err
		}
		years := aggregateYearKLines(months, limit)
		if len(years) == 0 {
			return nil, fmt.Errorf("adjusted monthly source returned no usable annual bars")
		}
		for index := range years {
			years[index].Meta.Period = "year"
		}
		return years, nil
	}
	if period != "day" && period != "week" && period != "month" {
		return nil, fmt.Errorf("specified stock price adjustment supports only day/week/month/year")
	}
	provider := s.strictKLineProvider(source)
	if provider == nil {
		return nil, fmt.Errorf("specified source does not support explicit price adjustment")
	}
	if capability, ok := provider.(interface {
		SupportsAdjustedKLine(string, string, string) bool
	}); ok && !capability.SupportsAdjustedKLine(symbol, period, adjustment) {
		return nil, fmt.Errorf("specified source does not support this stock market or period")
	}
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	key := source + ":" + normalized.Market + ":" + period + ":" + adjustment
	if !s.kLineRoutes.ready(key) {
		return nil, fmt.Errorf("指定复权来源正在失败退避；未切换其他价格口径，请稍后重试")
	}
	attemptAt := time.Now()
	capability := "stock-kline:" + period + ":" + adjustment
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	bars, err := provider.KLineAdjusted(requestCtx, normalized.Canonical, period, limit, adjustment)
	if err == nil {
		err = requestCtx.Err()
	}
	if err == nil {
		err = validateStrictKLineIdentity(bars, source, adjustment)
	}
	if err == nil {
		bars, err = normalizeKLineContract(bars, normalized.Canonical, period, adjustment, adjustment)
	}
	if err != nil {
		s.observePriceFailure(ctx, source, key, capability, normalized.Canonical, attemptAt, err)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("指定复权来源暂不可用，不切换到不同复权口径；可稍后重试或选择来源默认")
	}
	s.kLineRoutes.record(key, false)
	label := map[string]string{"none": "不复权", "qfq": "前复权", "hfq": "后复权"}[adjustment]
	for index := range bars {
		bars[index].Meta.Capability = capability
		bars[index].Meta.FallbackReason = strings.TrimSpace(label + "（" + source + "来源指定口径，不代表其他来源同名口径等价）；" + bars[index].Meta.FallbackReason)
	}
	s.sourceHealth.observe(foundation.SourceObservation{SourceID: source, Capability: capability, AttemptAt: attemptAt, Meta: bars[len(bars)-1].Meta})
	return bars, nil
}
