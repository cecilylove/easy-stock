// Package marketoverview retains source compatibility for older composition
// roots. All cross-supplier routing lives in datasource/service.
package marketoverview

import "easy-stock/backend/internal/datasource/service"

type Primary = service.Primary
type IndustryMomentumProvider = service.IndustryMomentumProvider
type FundFlowProvider = service.FundFlowProvider
type USSectorMomentumProvider = service.USSectorMomentumProvider
type IndexProvider = service.IndexProvider
type Provider = service.Market

var ErrUnsupportedIndexSeries = service.ErrUnsupportedIndexSeries

func New(primary Primary, index IndexProvider, industry IndustryMomentumProvider, funds FundFlowProvider) *Provider {
	return service.NewLegacyMarket(primary, index, industry, funds)
}
func CanonicalIndexID(id string) string          { return service.CanonicalIndexID(id) }
func CanonicalIndexPeriod(period string) string  { return service.CanonicalIndexPeriod(period) }
func SupportsIndexSeries(id, period string) bool { return service.SupportsIndexSeries(id, period) }
