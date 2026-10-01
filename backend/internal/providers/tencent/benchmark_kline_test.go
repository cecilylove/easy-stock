package tencent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func TestBenchmarkEmptyAndInvalidSeriesAreNotNetworkOutages(t *testing.T) {
	for _, test := range []struct {
		body   string
		wanted error
	}{{`{"code":0,"data":{"sh000300":{"day":[]}}}`, foundation.ErrPriceNoData}, {`{"code":0,"data":null}`, foundation.ErrPriceNoData}, {`{"code":0,"data":{"sh000300":{"day":[["bad"]]}}}`, foundation.ErrInvalidPriceData}} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, test.body) }))
		client := NewPriceKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
		_, err := client.KLine(context.Background(), "000300.SH", "day", 1)
		upstream.Close()
		if !errors.Is(err, test.wanted) {
			t.Fatalf("benchmark failure class %+v %v", test, err)
		}
	}
}

func TestBenchmarkKLineRoutesKnownIndexesWithoutStockUnits(t *testing.T) {
	mappings := map[string]string{"000001.SH": "sh000001", "000300.SH": "sh000300", "000016.SH": "sh000016", "000852.SH": "sh000852", "000688.SH": "sh000688", "399001.SZ": "sz399001", "399006.SZ": "sz399006"}
	for symbol, native := range mappings {
		t.Run(symbol, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("param"); got != native+",day,,,2," {
					t.Errorf("wrong index param %s", got)
				}
				fmt.Fprintf(w, `{"code":0,"data":{"%s":{"day":[["2026-09-29","3000","3100","3200","2900","10"],["2026-09-30","3100","3150","3200","3000","20"]]}}}`, native)
			}))
			defer upstream.Close()
			client := NewPriceKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
			lines, err := client.KLine(context.Background(), symbol, "daily", 2)
			id, _ := BenchmarkIndexID(symbol)
			if err != nil || len(lines) != 2 || lines[1].Symbol != symbol || lines[1].Volume != 20 || lines[1].Meta.InstrumentID != id || lines[1].Meta.VolumeUnit != "provider_index_volume" || lines[1].Meta.BasisID != "tencent:index:"+id+":none" || lines[1].PreviousClose != 3100 || foundation.FieldAvailable(lines[1].Meta, "amount") {
				t.Fatalf("benchmark contract %+v %v", lines, err)
			}
			if lines[0].Time.Format("2006-01-02") != "2026-09-29" {
				t.Fatal("date label shifted")
			}
			if _, offset := lines[0].Time.Zone(); offset != 8*60*60 {
				t.Fatal("benchmark session not Shanghai date")
			}
		})
	}
}

func TestBenchmarkKLineDoesNotInventUnsupportedIndexOrMinute(t *testing.T) {
	client := NewPriceKLineClient(nil)
	for _, symbol := range []string{"899050.BJ", "399005.SZ", "000905.SH"} {
		if client.SupportsKLine(symbol, "day") {
			t.Errorf("unmapped index supported %s", symbol)
		}
	}
	if client.SupportsKLine("000300.SH", "5") {
		t.Fatal("stock minute capability applied to index")
	}
	// stock capability still separate and retains board-based units.
	if !client.SupportsKLine("688300.SH", "month") || !client.SupportsKLine("000002.SZ", "week") {
		t.Fatal("stock capability lost")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("unsupported benchmark requested") }))
	defer upstream.Close()
	client = NewPriceKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
	if _, err := client.KLine(context.Background(), "899050.BJ", "day", 2); err == nil {
		t.Fatal("unmapped BJ index accepted")
	}
}

func TestBenchmarkKLineWeeklyAndMonthlyLabels(t *testing.T) {
	for _, period := range []string{"weekly", "monthly"} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			canonical := "week"
			if strings.HasPrefix(period, "month") {
				canonical = "month"
			}
			fmt.Fprintf(w, `{"code":0,"data":{"sh000300":{"%s":[["2026-09-30","3000","3100","3200","2900","10"]]}}}`, canonical)
		}))
		client := NewPriceKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
		lines, err := client.KLine(context.Background(), "000300.SH", period, 1)
		if err != nil || len(lines) != 1 {
			t.Fatalf("period %s %+v %v", period, lines, err)
		}
		upstream.Close()
	}
}
