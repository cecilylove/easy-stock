package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestStockCatalogCollectsPagesWithoutInventingMembership(t *testing.T) {
	var pages []string
	var pagesMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pagesMu.Lock()
		pages = append(pages, page)
		pagesMu.Unlock()
		if r.URL.Query().Get("node") != "hs_a" || r.URL.Query().Get("sort") != "symbol" {
			t.Errorf("unexpected directory query: %s", r.URL.RawQuery)
		}
		rows := make([]map[string]any, 0, stockCatalogPageSize)
		if page == "1" {
			for i := 0; i < stockCatalogPageSize; i++ {
				rows = append(rows, map[string]any{"symbol": fmt.Sprintf("sh600%03d", i), "name": "沪股", "trade": "12.3", "changepercent": 1.2})
			}
		} else {
			rows = append(rows, map[string]any{"symbol": "sz000002", "name": "万科A", "trade": 4.2})
			rows = append(rows, map[string]any{"symbol": "bj830001", "name": "不属于沪深节点范围"})
			rows = append(rows, map[string]any{"symbol": "sh510300", "name": "ETF"})
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer server.Close()
	items, err := NewClient(WithStockCatalogBaseURL(server.URL), WithHTTPClient(server.Client())).StockCatalog(context.Background())
	sort.Strings(pages)
	if err != nil || len(items) != stockCatalogPageSize+1 || strings.Join(pages, ",") != "1,2,3,4" {
		t.Fatalf("catalog len=%d pages=%v err=%v", len(items), pages, err)
	}
	if items[0].Symbol != "600000.SH" || items[0].Price != 12.3 || items[stockCatalogPageSize].Name != "万科A" || items[stockCatalogPageSize].Symbol != "000002.SZ" {
		t.Fatalf("incorrect symbol/name/quote normalization: first=%+v last=%+v", items[0], items[stockCatalogPageSize])
	}
	for _, item := range items {
		if item.Industry != "" || len(item.Concepts) != 0 || item.Meta.Source != "sina:stock-directory" || item.Meta.FetchedAt.IsZero() {
			t.Fatalf("unsupported membership or missing source metadata: %+v", item)
		}
	}
}

func TestStockCatalogRejectsFailedAndRepeatedPages(t *testing.T) {
	for _, failure := range []string{"http", "repeat"} {
		t.Run(failure, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("page") == "2" && failure == "http" {
					http.Error(w, "offline", http.StatusServiceUnavailable)
					return
				}
				rows := make([]map[string]any, stockCatalogPageSize)
				for i := range rows {
					rows[i] = map[string]any{"symbol": fmt.Sprintf("sh600%03d", i), "name": "沪股"}
				}
				_ = json.NewEncoder(w).Encode(rows)
			}))
			defer server.Close()
			items, err := NewClient(WithStockCatalogBaseURL(server.URL), WithHTTPClient(server.Client())).StockCatalog(context.Background())
			if err == nil || items != nil {
				t.Fatalf("partial catalog returned as complete: len=%d err=%v", len(items), err)
			}
		})
	}
}

func TestStockCatalogDecodesGBKAndHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=gbk")
		body, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`[{"symbol":"sz000002","name":"万科A","trade":"4.2"}]`))
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := NewClient(WithStockCatalogBaseURL(server.URL), WithHTTPClient(server.Client()))
	items, err := client.StockCatalog(context.Background())
	if err != nil || len(items) != 1 || items[0].Name != "万科A" {
		t.Fatalf("GBK catalog=%+v err=%v", items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if items, err := client.StockCatalog(ctx); err == nil || items != nil {
		t.Fatalf("cancelled request returned a catalog: len=%d err=%v", len(items), err)
	}
}

func TestStockCatalogBeijingPagesAdvanceWithoutClaimingCoverage(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		t.Run(fmt.Sprintf("repeat=%v", repeat), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Query().Get("page") == "1" || repeat {
					rows := make([]map[string]any, stockCatalogPageSize)
					for i := range rows {
						rows[i] = map[string]any{"symbol": fmt.Sprintf("bj920%03d", i), "name": "北股"}
					}
					_ = json.NewEncoder(w).Encode(rows)
					return
				}
				_, _ = w.Write([]byte(`[{"symbol":"sh600000","name":"浦发银行"},{"symbol":"sz000002","name":"万科A"}]`))
			}))
			defer server.Close()
			items, err := NewClient(WithStockCatalogBaseURL(server.URL), WithHTTPClient(server.Client())).StockCatalog(context.Background())
			if requests.Load() != 4 {
				t.Fatalf("pagination did not stop after bounded batch: calls=%d", requests.Load())
			}
			if repeat {
				if err == nil || items != nil || !strings.Contains(err.Error(), "pagination did not advance") {
					t.Fatalf("repeated BJ page accepted: items=%+v err=%v", items, err)
				}
			} else if err != nil || len(items) != 2 || items[0].Symbol != "600000.SH" || items[1].Symbol != "000002.SZ" {
				t.Fatalf("BJ first page blocked SH/SZ directory: items=%+v err=%v", items, err)
			}
		})
	}
}
