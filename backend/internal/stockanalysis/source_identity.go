package stockanalysis

import (
	"easy-stock/backend/internal/foundation"
	"sort"
	"strings"
)

func researchReportSources(items []foundation.MarketResearchItem) string {
	seen := map[string]bool{}
	for _, item := range items {
		if source := strings.TrimSpace(item.Meta.Source); source != "" {
			seen[source] = true
		}
	}
	sources := make([]string, 0, len(seen))
	for source := range seen {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	if len(sources) == 0 {
		return "研报来源未记录"
	}
	return strings.Join(sources, " + ")
}
