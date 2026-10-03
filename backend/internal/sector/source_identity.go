package sector

import (
	"easy-stock/backend/internal/foundation"
	"sort"
	"strings"
)

func boardStockSources(stocks []foundation.BoardStock, fallback string) string {
	seen := map[string]bool{}
	for _, stock := range stocks {
		if source := strings.TrimSpace(stock.Meta.Source); source != "" {
			seen[source] = true
		}
	}
	list := make([]string, 0, len(seen))
	for source := range seen {
		list = append(list, source)
	}
	sort.Strings(list)
	if len(list) == 0 {
		return fallback
	}
	return strings.Join(list, " + ")
}
