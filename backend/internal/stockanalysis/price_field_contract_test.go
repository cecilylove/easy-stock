package stockanalysis

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func TestHistoricalAmountHonorsKnownFieldsAndLegacyPositive(t *testing.T) {
	masked := foundation.SourceMeta{FieldsKnown: true, AvailableFields: []string{"close", "volume"}}
	known := foundation.SourceMeta{FieldsKnown: true, AvailableFields: []string{"amount"}}
	lines := []foundation.KLine{
		{Amount: 9_000_000_000, Meta: masked},
		{Amount: 8_000_000_000, Meta: foundation.SourceMeta{FieldsKnown: true}},
		{Amount: 200_000_000}, // Legacy positive fields remain usable.
		{Amount: 400_000_000, Meta: known},
		{Amount: 0, Meta: known},
		{Amount: -1, Meta: known},
		{Amount: math.Inf(1), Meta: known},
		{Amount: math.NaN(), Meta: known},
	}
	if got := averageKLineAmount(lines, len(lines)); got != 300_000_000 {
		t.Fatalf("average must use only valid positive available amounts: %v", got)
	}
}

func TestHistoricalFieldQualityReportsPartialCoverage(t *testing.T) {
	lines := syntheticTrendLines("600000.SH", 20, 10, .1, 300_000_000)
	for i := range lines[1:] {
		lines[i+1].Meta.FieldsKnown = true
		lines[i+1].Meta.AvailableFields = []string{"close", "volume"}
	}
	for _, quality := range historicalFieldQuality(lines, 20) {
		if quality.Status != "limited" || !strings.Contains(quality.Message, "20个交易日有1个有效") {
			t.Fatalf("partial historical coverage must stay explicit: %+v", quality)
		}
	}
	if got := averageKLineAmount(lines, 20); got != 300_000_000 {
		t.Fatalf("missing samples must not dilute real positive amount to zero: %v", got)
	}
	short := analyzeShortTerm("600000.SH", lines, []foundation.LimitUpEvent{{Symbol: "600000.SH", TurnoverRate: 5}})
	if short.LatestTurnover != 5 {
		t.Fatalf("separately available event turnover must remain usable: %+v", short)
	}
}

func TestAnalyzeMissingHistoricalFieldsDoesNotConfirmCapacity(t *testing.T) {
	for _, count := range []int{1, 19, 80} {
		t.Run(fmt.Sprintf("%d_days", count), func(t *testing.T) {
			lines := syntheticTrendLines("600000.SH", count, 10, .1, 2_500_000_000)
			for i := range lines {
				// Populated placeholders must not override the supplier's mask.
				lines[i].Meta.FieldsKnown = true
				lines[i].Meta.AvailableFields = []string{"open", "close", "high", "low", "volume"}
			}
			input := Input{Symbol: "600000.SH", KLines: lines}
			analysis, err := Analyze(input)
			if err != nil {
				t.Fatal(err)
			}
			if analysis.ShortTerm.AverageAmount20 != 0 || analysis.ShortTerm.LatestTurnover != 0 || analysis.ShortTerm.Tradability != "流动性未确认" {
				t.Fatalf("missing fields were interpreted as capacity: %+v", analysis.ShortTerm)
			}
			for _, key := range []string{"historical_amount", "historical_turnover"} {
				found := false
				for _, quality := range analysis.DataQuality {
					if quality.Key == key && quality.Status == "limited" && strings.Contains(quality.Message, "0个有效") {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing field quality %s: %+v", key, analysis.DataQuality)
				}
			}
			plan := buildShortTermQuantitativePlan(input, analysis.ShortTerm, analysis.Theme, analysis.Market)
			if plan.Stock.AuctionAmountMin != 0 || plan.Stock.OpeningAmountMin != 0 || !strings.Contains(strings.Join(plan.Missing, ";"), "成交额阈值不可用") {
				t.Fatalf("missing amount must preserve unavailable thresholds: %+v", plan)
			}
			if count < 20 {
				for _, signal := range analysis.Signals {
					if signal.Key == "liquidity" || signal.Key == "turnover" {
						t.Fatalf("unknown new-listing fields must not be scored: %+v", signal)
					}
				}
				for _, item := range analysis.Evidence {
					if item.Category == "流动性" && (!strings.Contains(item.Title, "成交额未知") || !strings.Contains(item.Title, "换手率未知")) {
						t.Fatalf("unknown new-listing liquidity evidence: %+v", item)
					}
				}
			}
			for _, bar := range analysis.dailyBars {
				if bar.Amount != 0 || bar.TurnoverRate != 0 {
					t.Fatalf("AI bars leaked masked values: %+v", bar)
				}
			}
		})
	}
}

func TestHistoricalFieldsKeepLegacyPositiveSignals(t *testing.T) {
	lines := syntheticTrendLines("600000.SH", 3, 10, .1, 2_500_000_000)
	analysis, err := Analyze(Input{Symbol: "600000.SH", KLines: lines})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ShortTerm.Tradability != "容量充足" || analysis.Trend.AverageAmount != 2_500_000_000 || analysis.Trend.AverageTurnover != 3.5 {
		t.Fatalf("legacy positive values must retain existing behavior: %+v", analysis)
	}
	for _, key := range []string{"liquidity", "turnover"} {
		found := false
		for _, signal := range analysis.Signals {
			found = found || signal.Key == key
		}
		if !found {
			t.Fatalf("legacy signal %s missing", key)
		}
	}
}

func TestResearchSnapshotPreservesEffectiveBasisAndUnits(t *testing.T) {
	cases := []struct {
		name  string
		meta  foundation.SourceMeta
		basis string
		unit  string
	}{
		{"url_is_not_effective", foundation.SourceMeta{SourceURL: "https://example.test/kline?fqt=1"}, "未明确标注有效复权", "unknown"},
		{"requested_is_not_effective", foundation.SourceMeta{RequestedAdjustment: "qfq"}, "未明确标注有效复权", "unknown"},
		{"none_overrides_url", foundation.SourceMeta{SourceURL: "https://example.test/kline?fqt=1", RequestedAdjustment: "qfq", EffectiveAdjustment: "none", VolumeUnit: "provider_index_volume"}, "明确标注不复权", "provider_index_volume"},
		{"qfq_convention", foundation.SourceMeta{EffectiveAdjustment: "qfq", AdjustmentConvention: "tencent:fqkline:provider-current", BasisID: "tencent:qfq", VolumeUnit: "shares"}, "明确标注前复权", "shares"},
		{"hfq", foundation.SourceMeta{EffectiveAdjustment: "hfq", VolumeUnit: "shares"}, "明确标注后复权", "shares"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := syntheticTrendLines("600000.SH", 25, 10, .1, 9_000_000_000)
			for i := range lines {
				lines[i].Meta = tc.meta
				lines[i].Meta.FieldsKnown = true
				lines[i].Meta.AvailableFields = []string{"open", "close", "high", "low", "volume"}
			}
			input := Input{Symbol: "600000.SH", KLines: lines, BenchmarkKLines: lines, BenchmarkSymbol: "000300.SH"}
			analysis, err := Analyze(input)
			if err != nil {
				t.Fatal(err)
			}
			analysis.Relative.Available = true
			// Reverse input order to ensure latest metadata is selected by date.
			input.KLines = append([]foundation.KLine{}, lines[1:]...)
			input.KLines = append(input.KLines, lines[0])
			input.KLines[len(input.KLines)-1].Meta.EffectiveAdjustment = "wrong"
			input.KLines[len(input.KLines)-1].Meta.VolumeUnit = "wrong"
			snapshot := BuildResearchSnapshot(input, analysis, lines[len(lines)-1].Time)
			for _, id := range []string{"m-price", "m-relative"} {
				var content map[string]json.RawMessage
				for _, source := range snapshot.Sources {
					if source.ID == id {
						if err := json.Unmarshal([]byte(source.Content), &content); err != nil {
							t.Fatal(err)
						}
					}
				}
				var basis, unit string
				if err := json.Unmarshal(content["price_basis"], &basis); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(content["volume_unit"], &unit); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(basis, tc.basis) || unit != tc.unit {
					t.Fatalf("%s source contract lost: basis=%q unit=%q", id, basis, unit)
				}
				if tc.meta.AdjustmentConvention != "" && !strings.Contains(basis, tc.meta.AdjustmentConvention) {
					t.Fatalf("adjustment convention lost: %q", basis)
				}
				if tc.meta.BasisID != "" && !strings.Contains(basis, tc.meta.BasisID) {
					t.Fatalf("basis identity lost: %q", basis)
				}
			}
			if !strings.Contains(strings.Join(snapshot.Limitations, ";"), "不能按0成交额") || !strings.Contains(strings.Join(snapshot.Limitations, ";"), "不能按0换手") {
				t.Fatalf("historical field limitations missing: %+v", snapshot.Limitations)
			}
			for _, bar := range snapshot.DailyBars {
				if bar.Amount != 0 || bar.TurnoverRate != 0 {
					t.Fatalf("snapshot leaked masked values: %+v", bar)
				}
			}
		})
	}
}
