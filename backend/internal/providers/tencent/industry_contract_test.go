package tencent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func TestIndustryMomentumKeepsZeroAndMissingDistinct(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":[{"bd_code":"pt1","bd_name":"A","bd_zdf":"0","bd_zdf5":null,"bd_zdf20":"--","nzg_zdf":"NaN"},{"bd_code":"pt2","bd_name":"B","bd_zdf":1,"bd_zdf5":"0","bd_zdf20":"2"}]}`)
	}))
	defer upstream.Close()
	client := NewClient(WithIndustryBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	items, meta, err := client.IndustryMomentum(context.Background(), 20)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if !foundation.FieldAvailable(items[0].Meta, "change_percent") || foundation.FieldAvailable(items[0].Meta, "five_day_change_percent") || foundation.FieldAvailable(items[0].Meta, "twenty_day_change_percent") || foundation.FieldAvailable(items[0].Meta, "leader_change_percent") {
		t.Fatalf("missing/zero mask: %+v", items[0].Meta)
	}
	if !foundation.FieldAvailable(items[1].Meta, "five_day_change_percent") || foundation.FieldAvailable(meta, "five_day_change_percent") {
		t.Fatalf("row/list mask: row=%+v list=%+v", items[1].Meta, meta)
	}
	if items[0].Meta.Provider != "tencent" || items[0].Meta.NativeCode != "pt1" {
		t.Fatalf("identity lost: %+v", items[0].Meta)
	}
}

func writeIndustryRows(w http.ResponseWriter, total, start, count int) {
	rows := make([]map[string]any, 0, count)
	for index := start; index < start+count; index++ {
		rows = append(rows, map[string]any{"code": fmt.Sprintf("sh%06d", 600000+index), "name": "fixture", "zxj": "10", "zdf": "0", "zd": nil, "turnover": "1", "volume": "2"})
	}
	json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"total": total, "rank_list": rows}})
}

func TestIndustryStocksPaginationCompletenessAndFields(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		count, _ := strconv.Atoi(r.URL.Query().Get("count"))
		writeIndustryRows(w, 205, offset, min(count, 205-offset))
	}))
	defer upstream.Close()
	client := NewClient(WithIndustryStocksBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	stocks, meta, err := client.IndustryStocks(context.Background(), "pt1", 500)
	if err != nil || calls != 2 || len(stocks) != 205 || !meta.MemberSet.Complete || meta.MemberSet.HasMore || meta.MemberSet.Total != 205 || meta.MemberSet.Returned != 205 {
		t.Fatalf("calls=%d len=%d meta=%+v err=%v", calls, len(stocks), meta, err)
	}
	if !foundation.FieldAvailable(stocks[0].Meta, "change_percent") || foundation.FieldAvailable(stocks[0].Meta, "change") || foundation.FieldAvailable(stocks[0].Meta, "five_day_change_percent") {
		t.Fatalf("fields: %+v", stocks[0].Meta)
	}
	if stocks[0].Volume != 100 || stocks[0].Amount != 20000 {
		t.Fatalf("units: %+v", stocks[0])
	}
	before := calls
	if _, _, err := client.IndustryStocks(context.Background(), "BK1", 10); err == nil || calls != before {
		t.Fatalf("foreign code reached upstream: err=%v calls=%d", err, calls)
	}
}

func TestIndustryStocksPartialPagesNeverClaimFullSet(t *testing.T) {
	for _, testCase := range []string{"cap", "repeat", "failed_page", "short_page"} {
		t.Run(testCase, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				count, _ := strconv.Atoi(r.URL.Query().Get("count"))
				if testCase == "failed_page" && offset > 0 {
					http.Error(w, "fixture failure", 502)
					return
				}
				if testCase == "repeat" {
					offset = 0
				}
				if testCase == "short_page" {
					count = 2
				}
				writeIndustryRows(w, 600, offset, count)
			}))
			defer upstream.Close()
			client := NewClient(WithIndustryStocksBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
			stocks, meta, err := client.IndustryStocks(context.Background(), "pt1", 500)
			if err != nil || len(stocks) == 0 || meta.MemberSet.Complete || !meta.Partial || !meta.MemberSet.HasMore || meta.MemberSet.Total != 600 || meta.MemberSet.Returned != len(stocks) {
				t.Fatalf("len=%d meta=%+v err=%v", len(stocks), meta, err)
			}
			if testCase == "repeat" && (calls != 2 || len(stocks) != 200) {
				t.Fatalf("duplicate protection calls=%d len=%d", calls, len(stocks))
			}
		})
	}
}

func TestIndustryStocksExhaustedButFilteredIsIncompleteWithoutMorePages(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"total":2,"rank_list":[{"code":"sh600001","name":"stock","zxj":"10"},{"code":"sh000001","name":"index","zxj":"10"}]}}`)
	}))
	defer upstream.Close()
	client := NewClient(WithIndustryStocksBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	stocks, meta, err := client.IndustryStocks(context.Background(), "pt1", 20)
	if err != nil || len(stocks) != 1 || meta.MemberSet.Complete || meta.MemberSet.HasMore {
		t.Fatalf("len=%d meta=%+v err=%v", len(stocks), meta, err)
	}
}

func TestIndustryStocksCancelledRequestDoesNotReturnSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewClient()
	if _, _, err := client.IndustryStocks(ctx, "pt1", 500); err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}
