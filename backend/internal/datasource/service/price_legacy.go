package service

import "easy-stock/backend/internal/foundation"

// NormalizeLegacyKLineContract is only a compatibility bridge for injected
// pre-migration providers with incomplete metadata. New routes use the neutral
// validator and adapters must return normalized quantities themselves. This
// does not enable retired sources or construct any supplier client.
func NormalizeLegacyKLineContract(lines []foundation.KLine, symbol, period, requested, effective string) ([]foundation.KLine, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	result := append([]foundation.KLine(nil), lines...)
	for index := range result {
		bar := &result[index]
		switch priceSourceID(bar.Meta) {
		case "eastmoney":
			if bar.Meta.NativeCode == "" {
				bar.Meta.NativeCode = normalized.EastMoneySecID
			}
			if bar.Meta.EffectiveAdjustment == "" {
				if effective != "" {
					bar.Meta.EffectiveAdjustment = effective
				} else {
					bar.Meta.EffectiveAdjustment = "qfq"
				}
			}
			if bar.Meta.AdjustmentConvention == "" {
				bar.Meta.AdjustmentConvention = "eastmoney:fqt:provider-current"
			}
			if bar.Meta.VolumeUnit == "" {
				bar.Volume *= 100
				bar.Meta.VolumeUnit = "shares"
			}
			if !bar.Meta.FieldsKnown {
				bar.Meta.FieldsKnown = true
				bar.Meta.AvailableFields = []string{"open", "high", "low", "close", "volume", "amount", "change_percent", "turnover_rate"}
			}
		case "sina":
			if bar.Meta.NativeCode == "" {
				bar.Meta.NativeCode = normalized.Sina
			}
			if bar.Meta.EffectiveAdjustment == "" {
				bar.Meta.EffectiveAdjustment = "source"
			}
			if bar.Meta.AdjustmentConvention == "" {
				bar.Meta.AdjustmentConvention = "sina:kline:source-default-unspecified"
			}
			if bar.Meta.VolumeUnit == "" {
				bar.Meta.VolumeUnit = "shares"
			}
			if !bar.Meta.FieldsKnown {
				bar.Meta.FieldsKnown = true
				bar.Meta.AvailableFields = []string{"open", "high", "low", "close", "volume"}
			}
		}
	}
	return NormalizeKLineContract(result, symbol, period, requested, effective)
}
