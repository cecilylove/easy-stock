package httpapi

import (
	"context"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/service"
	"easy-stock/backend/internal/foundation"
)

// Keep the private fields used by pre-migration HTTP fixtures. The map is owned
// and synchronized by PriceRouteState; backoff logic lives in the data service.
type klineRouteFailure = service.PriceRouteFailure

type klineRouteState struct {
	failures map[string]klineRouteFailure
	now      func() time.Time
	state    *service.PriceRouteState
}

func newKLineRouteState() *klineRouteState {
	r := &klineRouteState{failures: make(map[string]klineRouteFailure)}
	r.state = service.NewPriceRouteState(r.timeNow, r.failures)
	return r
}
func (r *klineRouteState) timeNow() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}
func (r *klineRouteState) ready(key string) bool {
	if r == nil {
		return true
	}
	return r.state.Ready(key)
}
func (r *klineRouteState) record(key string, failed bool) {
	if r != nil {
		r.state.Record(key, failed)
	}
}
func (r *klineRouteState) dataState() *service.PriceRouteState {
	if r == nil {
		return nil
	}
	return r.state
}

// priceDataService binds legacy injectable providers to the new service. The
// service accepts explicit candidate/strict registries; this bridge preserves
// the public API's existing default pair and strict Tencent convention.
func (s *Server) priceDataService() *service.PriceService {
	routes := []service.PriceRoute{
		{SourceID: s.kLinePrimarySourceID, Provider: s.kLinePrimary},
		{SourceID: s.kLineFallbackSourceID, Provider: s.kLineFallback},
	}
	strict := map[string]contracts.AdjustedKLineProvider{"tencent": s.kLineStrictTencent}
	normalize := service.NormalizeLegacyKLineContract
	legacy := s.priceRoutes == nil && s.strictPriceSources == nil
	if s.priceRoutes != nil {
		routes = s.priceRoutes
		normalize = service.NormalizeKLineContract
	}
	if s.strictPriceSources != nil {
		strict = s.strictPriceSources
		normalize = service.NormalizeKLineContract
	}
	return service.NewPriceService(service.PriceServiceConfig{
		Routes: routes,
		Strict: strict,
		State:  s.kLineRoutes.dataState(),
		Observe: func(observation foundation.SourceObservation) {
			if s.sourceHealth != nil {
				s.sourceHealth.observe(observation)
			}
		},
		Normalize:                 normalize,
		AllowLegacySourceIdentity: legacy,
	})
}

func marketPriceFailure(err error) bool { return service.MarketPriceFailure(err) }
func priceFailureCapability(capability, symbol string, err error) string {
	return service.PriceFailureCapability(capability, symbol, err)
}
func canonicalKLinePeriod(period string) string { return service.CanonicalKLinePeriod(period) }
func sourceKLineSupports(provider KLineProvider, symbol, period string) bool {
	return service.SourceKLineSupports(provider, symbol, period)
}
func normalizeKLineContract(lines []foundation.KLine, symbol, period, requested, effective string) ([]foundation.KLine, error) {
	return service.NormalizeLegacyKLineContract(lines, symbol, period, requested, effective)
}
func (s *Server) loadDefaultKLineRoutes(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	return s.priceDataService().LoadDefaultKLineRoutes(ctx, symbol, period, limit)
}
