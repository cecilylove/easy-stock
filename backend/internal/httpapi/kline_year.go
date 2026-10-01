package httpapi

import (
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// Year bars retain the source's adjustment convention. Volume and amount are
// summed once per observed calendar month; an unfinished year stays unfinished.
func aggregateYearKLines(months []foundation.KLine, limit int) []foundation.KLine {
	chinaTime := time.FixedZone("Asia/Shanghai", 8*60*60)
	ordered := append([]foundation.KLine(nil), months...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Time.Before(ordered[j].Time) })
	byMonth := make(map[string]foundation.KLine)
	for _, bar := range ordered {
		valid := !bar.Time.IsZero() && bar.High >= max(bar.Open, bar.Close) && bar.Low <= min(bar.Open, bar.Close) && bar.Volume >= 0 && bar.Amount >= 0
		for _, value := range []float64{bar.Open, bar.High, bar.Low, bar.Close, bar.Volume, bar.Amount} {
			valid = valid && !math.IsNaN(value) && !math.IsInf(value, 0)
		}
		if !valid {
			continue
		}
		byMonth[bar.Time.In(chinaTime).Format("2006-01")] = bar
	}
	keys := make([]string, 0, len(byMonth))
	for key := range byMonth {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result []foundation.KLine
	lastYear := ""
	for _, key := range keys {
		bar := byMonth[key]
		year := key[:4]
		if year != lastYear {
			bar.ChangePercent = 0
			bar.TurnoverRate = 0 // Monthly turnover cannot describe unique annual turnover.
			bar.Meta.FallbackReason = strings.Trim(strings.TrimSpace(bar.Meta.FallbackReason+"；年K由来源月K按自然年聚合，首尾年度可能未完整"), "；")
			result = append(result, bar)
			lastYear = year
			continue
		}
		annual := &result[len(result)-1]
		annual.Time, annual.Close = bar.Time, bar.Close
		annual.High, annual.Low = max(annual.High, bar.High), min(annual.Low, bar.Low)
		annual.Volume += bar.Volume
		annual.Amount += bar.Amount
		annual.Meta.Stale = annual.Meta.Stale || bar.Meta.Stale
		if bar.Meta.FetchedAt.After(annual.Meta.FetchedAt) {
			annual.Meta.FetchedAt = bar.Meta.FetchedAt
		}
	}
	for index := range result {
		if index > 0 {
			result[index].PreviousClose = result[index-1].Close
		}
		if result[index].PreviousClose > 0 {
			result[index].ChangePercent = (result[index].Close/result[index].PreviousClose - 1) * 100
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result
}
