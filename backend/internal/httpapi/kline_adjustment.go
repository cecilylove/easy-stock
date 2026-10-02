package httpapi

import (
	"context"
	"easy-stock/backend/internal/datasource/service"
	"easy-stock/backend/internal/foundation"
)

type adjustedKLineProvider = AdjustedKLineProvider

func (s *Server) loadAdjustedKLine(ctx context.Context, symbol, period string, limit int, adjustment string) ([]foundation.KLine, error) {
	return s.loadProviderAdjustedKLine(ctx, symbol, period, limit, adjustment, "tencent")
}
func (s *Server) strictKLineProvider(provider string) AdjustedKLineProvider {
	return s.priceDataService().StrictProvider(provider)
}
func validateStrictKLineIdentity(bars []foundation.KLine, source, adjustment string) error {
	return service.ValidateStrictKLineIdentity(bars, source, adjustment)
}
func (s *Server) loadProviderAdjustedKLine(ctx context.Context, symbol, period string, limit int, adjustment, source string) ([]foundation.KLine, error) {
	return s.priceDataService().KLineAdjusted(ctx, symbol, period, limit, adjustment, source)
}
