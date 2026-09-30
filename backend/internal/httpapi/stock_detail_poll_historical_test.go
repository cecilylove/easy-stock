package httpapi

import (
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestDetailHistoricalSourceValueIsNotCalledToday(t *testing.T) {
	china := time.FixedZone("CST", 8*3600)
	today := time.Date(2026, 9, 30, 9, 20, 0, 0, china)
	yesterday := today.AddDate(0, 0, -1)
	quotes := markHistoricalDetailQuotes([]foundation.Quote{{Symbol: "000001.SZ", Price: 11.2, TradeTime: yesterday, Meta: foundation.SourceMeta{Source: "sina", FetchedAt: today}}}, today)
	if !quotes[0].Meta.Stale || !strings.Contains(quotes[0].Meta.FallbackReason, "历史快照") {
		t.Fatalf("yesterday's quote was marked current: %+v", quotes[0])
	}
	copyOf := cloneDetailQuotesAsStale(quotes)
	if !strings.Contains(copyOf[0].Meta.FallbackReason, "历史快照") {
		t.Fatalf("backoff erased historical reason: %+v", copyOf[0])
	}
	lines := markHistoricalDetailKLines([]foundation.KLine{{Time: yesterday, Close: 11.2, Meta: foundation.SourceMeta{Source: "sina", FetchedAt: today}}}, today)
	if !lines[0].Meta.Stale || !strings.Contains(lines[0].Meta.FallbackReason, "历史快照") {
		t.Fatalf("yesterday's minute line was marked current: %+v", lines[0])
	}
	if cloneDetailKLinesAsStale(lines)[0].Meta.FallbackReason != lines[0].Meta.FallbackReason {
		t.Fatal("cached old day lost its source-date warning")
	}
	current := markHistoricalDetailKLines([]foundation.KLine{{Time: today, Close: 11.2, Meta: foundation.SourceMeta{Source: "sina", FetchedAt: today}}}, today)
	if current[0].Meta.Stale {
		t.Fatal("today's line unexpectedly marked stale")
	}
}
