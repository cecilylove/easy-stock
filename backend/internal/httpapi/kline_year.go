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
	var source, basis, symbol, unit string
	identitySet := false
	for _, bar := range ordered {
		valid := !bar.Time.IsZero() && bar.High >= max(bar.Open, bar.Close) && bar.Low <= min(bar.Open, bar.Close) && bar.Volume >= 0 && bar.Amount >= 0
		for _, value := range []float64{bar.Open, bar.High, bar.Low, bar.Close, bar.Volume, bar.Amount} {
			valid = valid && !math.IsNaN(value) && !math.IsInf(value, 0)
		}
		if !valid {
			continue
		}
		if !identitySet {
			source, basis, symbol, unit = bar.Meta.Source, bar.Meta.BasisID, bar.Symbol, bar.Meta.VolumeUnit
			identitySet = true
		} else if bar.Meta.Source != source || bar.Meta.BasisID != basis || bar.Symbol != symbol || bar.Meta.VolumeUnit != unit {
			return nil // Never aggregate across supplier/price-basis/unit boundaries.
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
			bar.ChangePercent, bar.PreviousClose = 0, 0 // A month's baseline is not the prior year close.
			bar.TurnoverRate = 0                        // Monthly turnover cannot describe unique annual turnover.
			bar.Meta.Period = "year"
			bar.Meta.FieldsKnown = bar.Meta.FieldsKnown || len(bar.Meta.AvailableFields) > 0
			bar.Meta.AvailableFields = annualAvailableFields(bar.Meta)
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
		fields := annualAvailableFields(bar.Meta)
		if bar.Meta.FieldsKnown || len(bar.Meta.AvailableFields) > 0 {
			if annual.Meta.FieldsKnown {
				intersection := []string{}
				for _, field := range annual.Meta.AvailableFields {
					for _, available := range fields {
						if available == field {
							intersection = append(intersection, field)
							break
						}
					}
				}
				annual.Meta.AvailableFields = intersection
			} else {
				annual.Meta.AvailableFields = fields
			}
			annual.Meta.FieldsKnown = true
		}
		annual.Meta.Partial = annual.Meta.Partial || bar.Meta.Partial
		annual.Meta.Stale = annual.Meta.Stale || bar.Meta.Stale
		if bar.Meta.FetchedAt.After(annual.Meta.FetchedAt) {
			annual.Meta.FetchedAt = bar.Meta.FetchedAt
		}
	}
	for index := range result {
		if index > 0 && result[index-1].Time.In(chinaTime).Month() == time.December && result[index].Time.In(chinaTime).Year() == result[index-1].Time.In(chinaTime).Year()+1 {
			result[index].PreviousClose = result[index-1].Close
			if result[index].Meta.FieldsKnown {
				result[index].Meta.AvailableFields = append(result[index].Meta.AvailableFields, "previous_close")
			}
			if result[index].PreviousClose > 0 {
				result[index].ChangePercent = (result[index].Close/result[index].PreviousClose - 1) * 100
				if result[index].Meta.FieldsKnown {
					result[index].Meta.AvailableFields = append(result[index].Meta.AvailableFields, "change_percent")
				}
			}
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result
}

func annualAvailableFields(meta foundation.SourceMeta) []string {
	fields := []string{}
	for _, field := range meta.AvailableFields {
		if field != "previous_close" && field != "change_percent" && field != "turnover_rate" {
			fields = append(fields, field)
		}
	}
	return fields
}
