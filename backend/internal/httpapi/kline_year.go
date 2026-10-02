package httpapi

import (
	"easy-stock/backend/internal/datasource/service"
	"easy-stock/backend/internal/foundation"
)

func aggregateYearKLines(months []foundation.KLine, limit int) []foundation.KLine {
	return service.AggregateYearKLines(months, limit)
}
func annualAvailableFields(meta foundation.SourceMeta) []string {
	return service.AnnualAvailableFields(meta)
}
