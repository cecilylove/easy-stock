package service

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

func CanonicalKLinePeriod(period string) string {
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

func priceSourceID(meta foundation.SourceMeta) string {
	id, _, _ := strings.Cut(strings.TrimSpace(meta.Source), ":")
	if id == "" {
		id = strings.TrimSpace(meta.Provider)
	}
	return id
}

// NormalizeKLineContract validates the supplier-neutral price contract. Real
// adapters normalize their native units and declare their own convention;
// this function never infers those conventions from a provider's Go type.
func NormalizeKLineContract(lines []foundation.KLine, symbol, period, requested, effective string) ([]foundation.KLine, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: kline source returned no bars", foundation.ErrPriceNoData)
	}
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	result := append([]foundation.KLine(nil), lines...)
	seen := make(map[int64]bool, len(result))
	source, basis := result[0].Meta.Source, result[0].Meta.BasisID
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
		id := priceSourceID(bar.Meta)
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
			if bar.Meta.EffectiveAdjustment != "" && bar.Meta.EffectiveAdjustment != effective {
				return nil, fmt.Errorf("%w: kline effective adjustment mismatch", foundation.ErrInvalidPriceData)
			}
			bar.Meta.EffectiveAdjustment = effective
		}
		if indexID, benchmark := foundation.BenchmarkIndexID(normalized.Canonical); benchmark {
			bar.Meta.InstrumentID = indexID
			bar.Meta.VolumeUnit = "provider_index_volume"
		}
		if bar.Meta.EffectiveAdjustment == "none" && bar.Low <= 0 {
			return nil, fmt.Errorf("%w: unadjusted transaction prices must be positive", foundation.ErrInvalidPriceData)
		}
		if bar.Meta.BasisID == "" {
			bar.Meta.BasisID = id + ":" + bar.Meta.AdjustmentConvention + ":" + bar.Meta.EffectiveAdjustment
		}
		bar.Meta.AvailableFields = append([]string(nil), bar.Meta.AvailableFields...)
		if index > 0 {
			first := result[0].Meta
			if bar.Meta.EffectiveAdjustment != first.EffectiveAdjustment || bar.Meta.AdjustmentConvention != first.AdjustmentConvention || bar.Meta.VolumeUnit != first.VolumeUnit || bar.Meta.AmountCurrency != first.AmountCurrency || bar.Meta.TimeZone != first.TimeZone {
				return nil, fmt.Errorf("%w: kline response mixes conventions or quantity units", foundation.ErrInvalidPriceData)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Time.Before(result[j].Time) })
	tradeDate := result[len(result)-1].Time.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	for index := range result {
		result[index].Meta.TradeDate = tradeDate
	}
	return result, nil
}

func ValidateStrictKLineIdentity(bars []foundation.KLine, source, adjustment string) error {
	if len(bars) == 0 {
		return fmt.Errorf("%w: strict kline source returned no bars", foundation.ErrPriceNoData)
	}
	for _, bar := range bars {
		if priceSourceID(bar.Meta) != source || (bar.Meta.Provider != "" && bar.Meta.Provider != source) {
			return fmt.Errorf("%w: strict kline supplier identity mismatch", foundation.ErrInvalidPriceData)
		}
		if bar.Meta.EffectiveAdjustment != "" && bar.Meta.EffectiveAdjustment != adjustment {
			return fmt.Errorf("%w: strict kline effective adjustment mismatch", foundation.ErrInvalidPriceData)
		}
	}
	return nil
}

// NormalizeKLinePeriod keeps the latest session for the one-minute view. Other
// minute periods preserve their existing multi-session sampling behavior.
func NormalizeKLinePeriod(lines []foundation.KLine, period string) []foundation.KLine {
	if strings.TrimSpace(period) != "1" || len(lines) == 0 {
		return lines
	}
	chinaTime := time.FixedZone("Asia/Shanghai", 8*60*60)
	latestTime := time.Time{}
	for _, line := range lines {
		if !line.Time.IsZero() && line.Time.After(latestTime) {
			latestTime = line.Time
		}
	}
	if latestTime.IsZero() {
		return lines
	}
	latestDate := latestTime.In(chinaTime).Format("2006-01-02")
	previousTime, previousClose := time.Time{}, 0.0
	for _, line := range lines {
		if line.Time.IsZero() || line.Time.In(chinaTime).Format("2006-01-02") == latestDate {
			continue
		}
		if line.Close > 0 && line.Time.Before(latestTime) && line.Time.After(previousTime) {
			previousTime, previousClose = line.Time, line.Close
		}
	}
	filtered := make([]foundation.KLine, 0, len(lines))
	for _, line := range lines {
		if line.Time.IsZero() || line.Time.In(chinaTime).Format("2006-01-02") != latestDate {
			continue
		}
		if line.PreviousClose <= 0 && previousClose > 0 {
			line.PreviousClose = previousClose
		}
		filtered = append(filtered, line)
	}
	if len(filtered) == 0 {
		return lines
	}
	return filtered
}
