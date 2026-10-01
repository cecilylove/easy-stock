package hotstock

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestTHSOnlyClientDoesNotRequestEastMoney(t *testing.T) {
	var eastMoneyRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ths" {
			eastMoneyRequests.Add(1)
			http.Error(w, "unexpected EastMoney request", 500)
			return
		}
		fmt.Fprint(w, `{"status_code":0,"data":{"stock_list":[{"order":1,"code":"600519","name":"贵州茅台"}]}}`)
	}))
	defer server.Close()
	client := NewTHSClient(WithHTTPClient(server.Client()), WithSourceURLs(server.URL+"/ths", server.URL+"/eastmoney"))
	lists := client.HotStockRanks(context.Background(), 100)
	if eastMoneyRequests.Load() != 0 || len(lists) != 1 || lists[0].Source != "ths" || len(lists[0].Items) != 1 {
		t.Fatalf("calls=%d lists=%+v", eastMoneyRequests.Load(), lists)
	}
}

func TestHotStockRanksLoadsBothSourcesAndNormalizesSymbols(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ths":
			_, _ = fmt.Fprint(w, `{"status_code":0,"data":{"stock_list":[{"order":1,"code":"600519","name":"贵州茅台"},{"order":2,"code":"000001","name":"平安银行"}]}}`)
		case "/eastmoney":
			if request.Method != http.MethodPost {
				t.Fatalf("eastmoney method = %s", request.Method)
			}
			_, _ = fmt.Fprint(w, `{"status":0,"code":0,"data":[{"sc":"SH600519","rk":1},{"sc":"SZ300750","rk":2}]}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithSourceURLs(server.URL+"/ths", server.URL+"/eastmoney"),
	)
	lists := client.HotStockRanks(context.Background(), 100)
	if len(lists) != 2 {
		t.Fatalf("list count = %d", len(lists))
	}
	if lists[0].Source != "ths" || lists[0].Items[0].Symbol != "600519.SH" || lists[0].Items[1].Symbol != "000001.SZ" {
		t.Fatalf("unexpected ths list: %+v", lists[0])
	}
	if lists[1].Items[1].Symbol != "300750.SZ" || lists[1].Items[1].Rank != 2 {
		t.Fatalf("unexpected eastmoney list: %+v", lists[1])
	}
}
