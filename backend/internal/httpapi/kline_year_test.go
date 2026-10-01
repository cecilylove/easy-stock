package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func annualTestMonth(date string, open, high, low, close, volume float64) foundation.KLine {
	stamp, _ := time.Parse(time.RFC3339, date+"T15:00:00+08:00")
	return foundation.KLine{Symbol: "000002.SZ", Time: stamp, Open: open, High: high, Low: low, Close: close, Volume: volume, Amount: volume * 10, TurnoverRate: 2, Meta: foundation.SourceMeta{Source: "fixture", FetchedAt: stamp}}
}

func TestAggregateYearKLinesPreservesOHLCAndDoesNotDoubleCountMonths(t *testing.T) {
	december := annualTestMonth("2025-12-31", 12, 16, 11, 15, 200)
	months := []foundation.KLine{
		annualTestMonth("2026-02-27", 16, 20, 14, 18, 400),
		annualTestMonth("2025-01-31", 10, 13, 8, 12, 100),
		annualTestMonth("2025-12-30", 12, 14, 11, 13, 50),
		december,
		annualTestMonth("2026-01-30", 15, 18, 13, 16, 300),
	}
	months[0].Meta.Stale = true
	bars := aggregateYearKLines(months, 2)
	if len(bars) != 2 {
		t.Fatalf("year count = %d", len(bars))
	}
	if bars[0].Open != 10 || bars[0].Close != 15 || bars[0].High != 16 || bars[0].Low != 8 || bars[0].Volume != 300 || bars[0].Amount != 3000 {
		t.Fatalf("wrong annual OHLC/volume: %+v", bars[0])
	}
	if bars[1].PreviousClose != 15 || bars[1].Volume != 700 || bars[1].TurnoverRate != 0 || !bars[1].Meta.Stale || bars[1].Time != months[0].Time || !strings.Contains(bars[1].Meta.FallbackReason, "按自然年聚合") {
		t.Fatalf("wrong unfinished/stale year: %+v", bars[1])
	}
	limited := aggregateYearKLines(months, 1)
	if len(limited) != 1 || limited[0].PreviousClose != 15 {
		t.Fatalf("lost previous year baseline: %+v", limited)
	}
}

func TestAggregateYearAcceptsNegativeAdjustedPricesAndRejectsNonFiniteFields(t *testing.T) {
	first := annualTestMonth("2025-01-31", -2, 1, -3, -1, 100)
	last := annualTestMonth("2025-12-31", 1, 4, 0, 3, 200)
	invalid := annualTestMonth("2025-06-30", 1, 2, 0, 1, 100)
	invalid.Amount = math.Inf(1)
	invalidHigh := invalid
	invalidHigh.Time = invalidHigh.Time.AddDate(0, 1, 0)
	invalidHigh.Amount = 1000
	invalidHigh.High = math.NaN()
	bars := aggregateYearKLines([]foundation.KLine{first, invalid, invalidHigh, last}, 1)
	if len(bars) != 1 || bars[0].Open != -2 || bars[0].Low != -3 || bars[0].High != 4 || bars[0].Close != 3 || bars[0].Volume != 300 {
		t.Fatalf("lost legal adjusted monthly bars: %+v", bars)
	}
	if _, err := json.Marshal(bars); err != nil {
		t.Fatalf("non-finite aggregate: %v", err)
	}
}

func TestAnnualFieldMaskNeverCarriesMonthPreviousClose(t *testing.T) {
	first := annualTestMonth("2025-06-30", 10, 11, 9, 10, 100)
	first.PreviousClose = 999
	first.Meta.FieldsKnown = true
	first.Meta.AvailableFields = []string{"open", "high", "low", "close", "volume", "amount", "turnover_rate", "previous_close", "change_percent"}
	last := annualTestMonth("2025-12-31", 10, 12, 9, 11, 100)
	last.Meta.FieldsKnown = true
	last.Meta.AvailableFields = []string{"open", "high", "low", "close", "volume"}
	next := last
	next.Time = next.Time.AddDate(1, 0, 0)
	bars := aggregateYearKLines([]foundation.KLine{first, last, next}, 2)
	if len(bars) != 2 || bars[0].PreviousClose != 0 || bars[0].ChangePercent != 0 || foundation.FieldAvailable(bars[0].Meta, "previous_close") || foundation.FieldAvailable(bars[0].Meta, "turnover_rate") || foundation.FieldAvailable(bars[0].Meta, "amount") || foundation.FieldAvailable(bars[0].Meta, "change_percent") || bars[0].Meta.Period != "year" {
		t.Fatalf("month mask leaked into year %+v", bars)
	}
	if bars[1].PreviousClose != 11 || !foundation.FieldAvailable(bars[1].Meta, "previous_close") || !foundation.FieldAvailable(bars[1].Meta, "change_percent") {
		t.Fatalf("actual previous-year baseline missing %+v", bars[1])
	}
	last.Time, _ = time.Parse(time.RFC3339, "2025-11-30T15:00:00+08:00")
	bars = aggregateYearKLines([]foundation.KLine{first, last, next}, 2)
	if bars[1].PreviousClose != 0 || foundation.FieldAvailable(bars[1].Meta, "change_percent") {
		t.Fatal("incomplete prior year used as full year baseline")
	}
}

func TestAnnualRejectsMixedPriceBasis(t *testing.T) {
	first := annualTestMonth("2025-06-30", 10, 11, 9, 10, 100)
	last := annualTestMonth("2025-12-31", 10, 12, 9, 11, 100)
	first.Meta.BasisID = "tencent:qfq"
	last.Meta.BasisID = "eastmoney:qfq"
	if result := aggregateYearKLines([]foundation.KLine{first, last}, 1); len(result) != 0 {
		t.Fatal("mixed annual price bases accepted")
	}
}

func TestYearKLineRouteFetchesMonthlyHistoryAndFallsBack(t *testing.T) {
	var periods []string
	provider := klineProviderFunc(func(_ context.Context, _ string, period string, limit int) ([]foundation.KLine, error) {
		periods = append(periods, period)
		if period != "month" || limit != 36 {
			t.Errorf("period=%s limit=%d", period, limit)
		}
		return []foundation.KLine{annualTestMonth("2024-12-31", 8, 10, 7, 9, 100), annualTestMonth("2025-12-31", 9, 12, 8, 11, 100), annualTestMonth("2026-09-30", 11, 14, 10, 13, 100)}, nil
	})
	server := NewServer(Config{KLinePrimary: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		return nil, errors.New("fixture primary failure")
	}), KLineFallback: provider})
	defer server.Close()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&period=year&limit=2", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []foundation.KLine `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 2 || payload.Data[0].PreviousClose != 9 || payload.Data[1].Close != 13 || len(periods) != 1 {
		t.Fatalf("wrong year response: %+v", payload.Data)
	}
}
