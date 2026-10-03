package httpapi

import (
	"easy-stock/backend/internal/foundation"
	"sort"
	"strings"
)

// Describes the collected inputs, never the supplier historically used by a
// model. Empty pools have no row provenance and are explicitly unknown here.
func marketEmotionInputSources(events []foundation.LimitUpEvent, pools marketEmotionSidePools, lines map[string]foundation.KLine, catalog []foundation.StockCatalogEntry) string {
	sources := map[string]bool{}
	add := func(kind string, meta foundation.SourceMeta) {
		if source := strings.TrimSpace(meta.Source); source != "" {
			sources[kind+":"+source] = true
		}
	}
	for _, event := range events {
		add("涨停", event.Meta)
		for field, source := range event.Meta.FieldSources {
			if source != "" {
				sources["涨停补字段("+field+"):"+source] = true
			}
		}
	}
	for _, event := range pools.broken {
		add("炸板", event.Meta)
	}
	for _, event := range pools.down {
		add("跌停", event.Meta)
	}
	for _, line := range lines {
		add("日K", line.Meta)
	}
	for _, item := range catalog {
		add("分类目录", item.Meta)
	}
	result := make([]string, 0, len(sources))
	for value := range sources {
		result = append(result, value)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return "本地情绪模型（输入来源未记录）"
	}
	return "本地情绪模型；实际采集输入：" + strings.Join(result, " + ")
}
