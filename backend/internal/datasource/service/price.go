package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
)

// PriceRoute is an explicitly enabled route; implementing an interface alone
// never puts a provider in the default chain.
type PriceRoute struct {
	SourceID string
	Provider contracts.KLineProvider
}

type PriceServiceConfig struct {
	Routes    []PriceRoute
	Strict    map[string]contracts.AdjustedKLineProvider
	State     *PriceRouteState
	Observe   func(foundation.SourceObservation)
	Normalize func([]foundation.KLine, string, string, string, string) ([]foundation.KLine, error)
	// Only pre-registry injected HTTP fixtures may omit or mismatch source IDs.
	AllowLegacySourceIdentity bool
}

// PriceService owns source selection, budgets, price-basis validation and
// backoff. It does not cache source-default K lines or join different suppliers'
// OHLC. The HTTP boundary only binds public parameters and the existing DTO.
type PriceService struct {
	routes                    []PriceRoute
	strict                    map[string]contracts.AdjustedKLineProvider
	state                     *PriceRouteState
	observe                   func(foundation.SourceObservation)
	normalize                 func([]foundation.KLine, string, string, string, string) ([]foundation.KLine, error)
	allowLegacySourceIdentity bool
}

func NewPriceService(config PriceServiceConfig) *PriceService {
	normalize := config.Normalize
	if normalize == nil {
		normalize = NormalizeKLineContract
	}
	strict := make(map[string]contracts.AdjustedKLineProvider, len(config.Strict))
	for id, provider := range config.Strict {
		strict[id] = provider
	}
	state := config.State
	if state == nil {
		state = NewPriceRouteState(nil, nil)
	}
	return &PriceService{routes: append([]PriceRoute(nil), config.Routes...), strict: strict, state: state, observe: config.Observe, normalize: normalize, allowLegacySourceIdentity: config.AllowLegacySourceIdentity}
}

func (s *PriceService) StrictProvider(source string) contracts.AdjustedKLineProvider {
	return s.strict[source]
}

// SupportsAdjusted reports support without requesting or observing a supplier.
// Legacy mocks without a support method retain their injected capabilities.
func (s *PriceService) SupportsAdjusted(source, symbol, period, adjustment string) bool {
	provider := s.StrictProvider(source)
	period = CanonicalKLinePeriod(period)
	if period == "year" {
		period = "month"
	}
	if provider == nil || (period != "day" && period != "week" && period != "month") || (adjustment != "none" && adjustment != "qfq" && adjustment != "hfq") {
		return false
	}
	if supported, ok := provider.(interface {
		SupportsAdjustedKLine(string, string, string) bool
	}); ok {
		return supported.SupportsAdjustedKLine(symbol, period, adjustment)
	}
	_, err := foundation.NormalizeSymbol(symbol)
	return err == nil
}

func SourceKLineSupports(provider contracts.KLineProvider, symbol, period string) bool {
	if provider == nil {
		return false
	}
	if supported, ok := provider.(interface{ SupportsKLine(string, string) bool }); ok {
		return supported.SupportsKLine(symbol, period)
	}
	return true
}

func (s *PriceService) recordFailure(ctx context.Context, source, key, capability, symbol string, attemptAt time.Time, err error) {
	if errors.Is(ctx.Err(), context.Canceled) || contracts.Kind(err) == contracts.Canceled || contracts.Kind(err) == contracts.Unsupported {
		return
	}
	if s.observe != nil {
		s.observe(foundation.SourceObservation{SourceID: source, Capability: PriceFailureCapability(capability, symbol, err), AttemptAt: attemptAt, Failed: true})
	}
	if MarketPriceFailure(err) {
		s.state.Record(key, true)
	}
}

func (s *PriceService) recordSuccess(source, key, capability string, attemptAt time.Time, meta foundation.SourceMeta) {
	if source != "" {
		s.state.Record(key, false)
	}
	if s.observe != nil {
		s.observe(foundation.SourceObservation{SourceID: source, Capability: capability, AttemptAt: attemptAt, Meta: meta})
	}
}

func (s *PriceService) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	period = CanonicalKLinePeriod(period)
	if period == "" {
		return nil, fmt.Errorf("unsupported kline period")
	}
	if period == "year" {
		limit = min(max(limit, 1), 50)
		months, err := s.KLine(ctx, symbol, "month", (limit+1)*12)
		if err != nil {
			return nil, err
		}
		years := AggregateYearKLines(months, limit)
		if len(years) == 0 {
			return nil, fmt.Errorf("monthly source returned no usable bars for year aggregation")
		}
		return years, nil
	}
	return s.LoadDefaultKLineRoutes(ctx, symbol, period, limit)
}

func (s *PriceService) LoadDefaultKLineRoutes(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	kind := "stock"
	if _, benchmark := foundation.BenchmarkIndexID(normalized.Canonical); benchmark {
		kind = "index"
	}
	var failures []string
	for index, route := range s.routes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !SourceKLineSupports(route.Provider, normalized.Canonical, period) {
			continue
		}
		key := route.SourceID + ":" + normalized.Market + ":" + kind + ":" + period + ":source"
		if route.SourceID != "" && !s.state.Ready(key) {
			failures = append(failures, route.SourceID+" capability cooling down")
			continue
		}
		budget := 6 * time.Second
		if index == len(s.routes)-1 {
			budget = 10 * time.Second
		}
		if deadline, ok := ctx.Deadline(); ok {
			remainingRoutes := 1
			for _, next := range s.routes[index+1:] {
				if SourceKLineSupports(next.Provider, normalized.Canonical, period) {
					remainingRoutes++
				}
			}
			budget = min(budget, time.Until(deadline)/time.Duration(remainingRoutes))
		}
		attemptAt := time.Now()
		capability := kind + "-kline:" + period + ":source"
		requestCtx, cancel := context.WithTimeout(ctx, budget)
		lines, loadErr := route.Provider.KLine(requestCtx, normalized.Canonical, period, limit)
		if loadErr == nil {
			loadErr = requestCtx.Err()
		}
		cancel()
		if loadErr == nil {
			lines, loadErr = s.normalize(lines, normalized.Canonical, period, "source", "")
		}
		if loadErr == nil && !s.allowLegacySourceIdentity && route.SourceID != "" && priceSourceID(lines[0].Meta) != route.SourceID {
			loadErr = fmt.Errorf("%w: default kline supplier identity mismatch", foundation.ErrInvalidPriceData)
		}
		if ctx.Err() != nil {
			if loadErr != nil {
				s.recordFailure(ctx, route.SourceID, key, capability, normalized.Canonical, attemptAt, loadErr)
			}
			return nil, ctx.Err()
		}
		if loadErr != nil {
			if errors.Is(loadErr, context.Canceled) {
				return nil, loadErr
			}
			failures = append(failures, fmt.Sprintf("%s: %v", route.SourceID, loadErr))
			s.recordFailure(ctx, route.SourceID, key, capability, normalized.Canonical, attemptAt, loadErr)
			continue
		}
		for lineIndex := range lines {
			if len(failures) > 0 {
				lines[lineIndex].Meta.FallbackReason = strings.TrimSpace("默认K来源请求失败或退避，已切换备用；价格口径以实际来源为准；" + lines[lineIndex].Meta.FallbackReason)
			}
			lines[lineIndex].Meta.Capability = capability
		}
		s.recordSuccess(route.SourceID, key, capability, attemptAt, lines[len(lines)-1].Meta)
		return NormalizeKLinePeriod(lines, period), nil
	}
	if len(failures) == 0 {
		return nil, fmt.Errorf("no source supports requested stock period")
	}
	return nil, fmt.Errorf("all default kline sources unavailable: %s", strings.Join(failures, "; "))
}

// KLineAdjusted uses exactly the selected registered convention. The default
// chain is never consulted after an explicit source or adjustment request.
func (s *PriceService) KLineAdjusted(ctx context.Context, symbol, period string, limit int, adjustment, source string) ([]foundation.KLine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	period = CanonicalKLinePeriod(period)
	if adjustment != "none" && adjustment != "qfq" && adjustment != "hfq" {
		return nil, fmt.Errorf("unsupported adjustment")
	}
	if s.StrictProvider(source) == nil {
		return nil, fmt.Errorf("unsupported strict provider")
	}
	if period == "year" {
		limit = min(max(limit, 1), 50)
		months, err := s.KLineAdjusted(ctx, symbol, "month", (limit+1)*12, adjustment, source)
		if err != nil {
			return nil, err
		}
		years := AggregateYearKLines(months, limit)
		if len(years) == 0 {
			return nil, fmt.Errorf("adjusted monthly source returned no usable annual bars")
		}
		return years, nil
	}
	if period != "day" && period != "week" && period != "month" {
		return nil, fmt.Errorf("specified stock price adjustment supports only day/week/month/year")
	}
	if !s.SupportsAdjusted(source, symbol, period, adjustment) {
		return nil, fmt.Errorf("specified source does not support this stock market or period")
	}
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	key := source + ":" + normalized.Market + ":" + period + ":" + adjustment
	if !s.state.Ready(key) {
		return nil, fmt.Errorf("指定复权来源正在失败退避；未切换其他价格口径，请稍后重试")
	}
	attemptAt := time.Now()
	capability := "stock-kline:" + period + ":" + adjustment
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	bars, err := s.StrictProvider(source).KLineAdjusted(requestCtx, normalized.Canonical, period, limit, adjustment)
	if err == nil {
		err = requestCtx.Err()
	}
	if err == nil {
		err = ValidateStrictKLineIdentity(bars, source, adjustment)
	}
	if err == nil {
		bars, err = s.normalize(bars, normalized.Canonical, period, adjustment, adjustment)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		s.recordFailure(ctx, source, key, capability, normalized.Canonical, attemptAt, err)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("指定复权来源暂不可用，不切换到不同复权口径；可稍后重试或选择来源默认")
	}
	label := map[string]string{"none": "不复权", "qfq": "前复权", "hfq": "后复权"}[adjustment]
	for index := range bars {
		bars[index].Meta.Capability = capability
		bars[index].Meta.FallbackReason = strings.TrimSpace(label + "（" + source + "来源指定口径，不代表其他来源同名口径等价）；" + bars[index].Meta.FallbackReason)
	}
	s.recordSuccess(source, key, capability, attemptAt, bars[len(bars)-1].Meta)
	return bars, nil
}
