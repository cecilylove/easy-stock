package stockanalysis

import (
	"easy-stock/backend/internal/foundation"
	"reflect"
	"testing"
	"time"
)

func TestFinancialDisclosureDateSurvivesCurrentSnapshotButNotFutureCutoffBaseline(t *testing.T) {
	lines := syntheticTrendLines("600519.SH", 65, 10, .1, 500_000_000)
	published := time.Date(2026, 8, 15, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	input := Input{Symbol: "600519.SH", Quote: foundation.Quote{Symbol: "600519.SH", Name: "fixture", Price: lines[len(lines)-1].Close}, KLines: lines, Fundamentals: &foundation.StockFundamentals{ReportDate: "2026-06-30", PublishedAt: published, Revenue: 100, RevenueYearOverYear: 100, NetProfit: 10, ROE: 20, Meta: foundation.SourceMeta{Source: "sina:financials"}}}
	analysis, err := Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	current := BuildResearchSnapshot(input, analysis, published.AddDate(0, 0, 1))
	found := false
	for _, source := range current.Sources {
		if source.ID == "f-financial" {
			found = source.Provider == "sina:financials" && source.TimeStatus == "dated" && source.PublishedAt.Equal(published)
		}
	}
	if !found {
		t.Fatal("disclosure date lost")
	}
	past := BuildResearchSnapshot(input, analysis, published.AddDate(0, 0, -1))
	for _, source := range past.Sources {
		if source.ID == "f-financial" {
			t.Fatal("future disclosure retained")
		}
	}
	input.Fundamentals = nil
	without, err := Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(past.Baseline, without.Scorecard) {
		t.Fatalf("future finance survived in baseline %+v vs %+v", past.Baseline, without.Scorecard)
	}
}
