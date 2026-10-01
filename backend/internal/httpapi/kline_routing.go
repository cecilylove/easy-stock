package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
)

type klineRouteFailure struct {
	failures int
	retryAt  time.Time
}
type klineRouteState struct {
	mu       sync.Mutex
	failures map[string]klineRouteFailure
	now      func() time.Time
}

func (r *klineRouteState) timeNow() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func newKLineRouteState() *klineRouteState {
	return &klineRouteState{failures: map[string]klineRouteFailure{}}
}
func (r *klineRouteState) ready(key string) bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.timeNow().Before(r.failures[key].retryAt)
}
func (r *klineRouteState) record(key string, failed bool) {
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
		entry.retryAt = r.timeNow().Add(time.Duration(min(120, 30*(entry.failures-1))) * time.Second)
	}
	r.failures[key] = entry
}

// Completed no-data/validation failures are instrument scoped. Transport
// failures and legacy untyped provider errors retain network-breaker behavior;
// typed statuses are classified by code, never by parsing English error text.
func marketPriceFailure(err error) bool {
	if err == nil || errors.Is(err, foundation.ErrPriceNoData) || errors.Is(err, foundation.ErrInvalidPriceData) || errors.Is(err, context.Canceled) {
		return false
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

func priceFailureCapability(capability, symbol string, err error) string {
	if marketPriceFailure(err) {
		return capability
	}
	return capability + ":symbol:" + symbol
}

func (s *Server) observePriceFailure(ctx context.Context, source, key, capability, symbol string, attemptAt time.Time, err error) {
	if !shouldObserveFailure(ctx) || errors.Is(err, context.Canceled) {
		return
	}
	s.sourceHealth.observe(foundation.SourceObservation{SourceID: source, Capability: priceFailureCapability(capability, symbol, err), AttemptAt: attemptAt, Failed: true})
	if marketPriceFailure(err) {
		s.kLineRoutes.record(key, true)
	}
}

func canonicalKLinePeriod(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "day", "daily", "101", "240":
		return "day"
	case "week", "weekly", "102", "1200":
		return "week"
	case "month", "monthly", "103", "7200":
		return "month"
	case "year":
		return "year"
	case "1", "5", "15", "30", "60", "120":
		return strings.TrimSpace(period)
	default:
		return ""
	}
}

func sourceKLineSupports(provider KLineProvider, symbol, period string) bool {
	if provider == nil {
		return false
	}
	if supported, ok := provider.(interface{ SupportsKLine(string, string) bool }); ok {
		return supported.SupportsKLine(symbol, period)
	}
	if _, ok := provider.(*sina.Client); ok {
		return period != "120"
	}
	return true
}

// Each response is one authoritative supplier snapshot, never cross-source
// patchwork. Field/unit normalization happens before annual aggregation/UI use.
func normalizeKLineContract(lines []foundation.KLine, symbol, period, requested, effective string) ([]foundation.KLine, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: kline source returned no bars", foundation.ErrPriceNoData)
	}
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	result := append([]foundation.KLine(nil), lines...)
	seen := map[int64]bool{}
	source := result[0].Meta.Source
	basis := result[0].Meta.BasisID
	for index := range result {
		bar := &result[index]
		if bar.Symbol != normalized.Canonical || bar.Time.IsZero() || seen[bar.Time.UnixNano()] || bar.Meta.Source != source || bar.Meta.BasisID != basis {
			return nil, fmt.Errorf("%w: kline response mixes symbols, times or price bases", foundation.ErrInvalidPriceData)
		}
		seen[bar.Time.UnixNano()] = true
		for _, value := range []float64{bar.Open, bar.High, bar.Low, bar.Close, bar.Volume, bar.Amount} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("%w: kline response has nonfinite value", foundation.ErrInvalidPriceData)
			}
		}
		if bar.High < max(bar.Open, bar.Close) || bar.Low > min(bar.Open, bar.Close) || bar.High < bar.Low || bar.Volume < 0 || bar.Amount < 0 {
			return nil, fmt.Errorf("%w: kline response has invalid OHLC or quantity", foundation.ErrInvalidPriceData)
		}
		id := sourceID(bar.Meta.Source)
		if bar.Meta.InstrumentID == "" {
			bar.Meta.InstrumentID = normalized.Canonical
		}
		bar.Meta.Period = period
		bar.Meta.RequestedAdjustment = requested
		if id != "" {
			bar.Meta.Provider = id
		}
		if bar.Meta.TimeZone == "" {
			bar.Meta.TimeZone = "Asia/Shanghai"
		}
		if bar.Meta.AmountCurrency == "" {
			bar.Meta.AmountCurrency = "CNY"
		}
		if effective != "" {
			bar.Meta.EffectiveAdjustment = effective
		}
		switch id {
		case "eastmoney":
			if bar.Meta.NativeCode == "" {
				bar.Meta.NativeCode = normalized.EastMoneySecID
			}
			if bar.Meta.EffectiveAdjustment == "" {
				bar.Meta.EffectiveAdjustment = "qfq"
			}
			if bar.Meta.AdjustmentConvention == "" {
				bar.Meta.AdjustmentConvention = "eastmoney:fqt:provider-current"
			}
			if bar.Meta.VolumeUnit == "" {
				bar.Volume *= 100
				bar.Meta.VolumeUnit = "shares"
			}
			if math.IsInf(bar.Volume, 0) {
				return nil, fmt.Errorf("%w: kline volume conversion overflow", foundation.ErrInvalidPriceData)
			}
			if !bar.Meta.FieldsKnown {
				bar.Meta.FieldsKnown = true
				bar.Meta.AvailableFields = []string{"open", "high", "low", "close", "volume", "amount", "change_percent", "turnover_rate"}
			}
		case "sina":
			if bar.Meta.NativeCode == "" {
				bar.Meta.NativeCode = normalized.Sina
			}
			bar.Meta.EffectiveAdjustment = "source"
			if bar.Meta.AdjustmentConvention == "" {
				bar.Meta.AdjustmentConvention = "sina:kline:source-default-unspecified"
			}
			if bar.Meta.VolumeUnit == "" {
				bar.Meta.VolumeUnit = "shares"
			}
			if !bar.Meta.FieldsKnown {
				bar.Meta.FieldsKnown = true
				bar.Meta.AvailableFields = []string{"open", "high", "low", "close", "volume"}
			}
		}
		if indexID, benchmark := tencent.BenchmarkIndexID(normalized.Canonical); benchmark {
			bar.Meta.InstrumentID = indexID
			bar.Meta.VolumeUnit = "provider_index_volume"
		}
		if bar.Meta.EffectiveAdjustment == "none" && bar.Low <= 0 {
			return nil, fmt.Errorf("%w: unadjusted transaction prices must be positive", foundation.ErrInvalidPriceData)
		}
		if bar.Meta.BasisID == "" {
			bar.Meta.BasisID = id + ":" + bar.Meta.AdjustmentConvention + ":" + bar.Meta.EffectiveAdjustment
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Time.Before(result[j].Time) })
	tradeDate := result[len(result)-1].Time.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	for index := range result {
		result[index].Meta.TradeDate = tradeDate
	}
	return result, nil
}

func (s *Server) loadDefaultKLineRoutes(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	routes := []struct {
		provider KLineProvider
		id       string
	}{{s.kLinePrimary, s.kLinePrimarySourceID}, {s.kLineFallback, s.kLineFallbackSourceID}}
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	var failures []string
	for index, route := range routes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !sourceKLineSupports(route.provider, normalized.Canonical, period) {
			continue
		}
		kind := "stock"
		if _, benchmark := tencent.BenchmarkIndexID(normalized.Canonical); benchmark {
			kind = "index"
		}
		key := route.id + ":" + normalized.Market + ":" + kind + ":" + period + ":source"
		if route.id != "" && !s.kLineRoutes.ready(key) {
			failures = append(failures, route.id+" capability cooling down")
			continue
		}
		budget := 6 * time.Second
		if index == len(routes)-1 {
			budget = 10 * time.Second
		}
		if deadline, ok := ctx.Deadline(); ok {
			remainingRoutes := 1
			for _, next := range routes[index+1:] {
				if sourceKLineSupports(next.provider, normalized.Canonical, period) {
					remainingRoutes++
				}
			}
			budget = min(budget, time.Until(deadline)/time.Duration(remainingRoutes))
		}
		attemptAt := time.Now()
		capability := kind + "-kline:" + period + ":source"
		requestCtx, cancel := context.WithTimeout(ctx, budget)
		lines, loadErr := route.provider.KLine(requestCtx, normalized.Canonical, period, limit)
		if loadErr == nil {
			loadErr = requestCtx.Err()
		}
		cancel()
		if loadErr == nil {
			lines, loadErr = normalizeKLineContract(lines, normalized.Canonical, period, "source", "")
		}
		if ctx.Err() != nil {
			if loadErr != nil {
				s.observePriceFailure(ctx, route.id, key, capability, normalized.Canonical, attemptAt, loadErr)
			}
			return nil, ctx.Err()
		}
		if loadErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", route.id, loadErr))
			s.observePriceFailure(ctx, route.id, key, capability, normalized.Canonical, attemptAt, loadErr)
			continue
		}
		if route.id != "" {
			s.kLineRoutes.record(key, false)
		}
		for lineIndex := range lines {
			if len(failures) > 0 {
				lines[lineIndex].Meta.FallbackReason = strings.TrimSpace("默认K来源请求失败或退避，已切换备用；价格口径以实际来源为准；" + lines[lineIndex].Meta.FallbackReason)
			}
			lines[lineIndex].Meta.Capability = capability
		}
		s.sourceHealth.observe(foundation.SourceObservation{SourceID: route.id, Capability: capability, AttemptAt: attemptAt, Meta: lines[len(lines)-1].Meta})
		return normalizeKLinePeriod(lines, period), nil
	}
	if len(failures) == 0 {
		return nil, fmt.Errorf("no source supports requested stock period")
	}
	return nil, fmt.Errorf("all default kline sources unavailable: %s", strings.Join(failures, "; "))
}
