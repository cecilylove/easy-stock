package futuresposition

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExchangeOnlyTrendNeverQueriesEastMoneyPrimary(t *testing.T) {
	var primaryRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/sj/ccpm/") {
			primaryRequests.Add(1)
			http.Error(w, "unexpected primary provider", 500)
			return
		}
		if !strings.Contains(r.URL.Path, "/202610/01/") {
			http.NotFound(w, r)
			return
		}
		for rank := 1; rank <= 20; rank++ {
			fmt.Fprintf(w, "20261001,IF2610,%d,member,100,0,long,10,1,short,20,2\n", rank)
		}
	}))
	defer server.Close()
	client := NewExchangeClient()
	client.http = server.Client()
	client.dataURL = server.URL + "/primary"
	client.cffexURL = server.URL
	client.now = func() time.Time { return time.Date(2026, 10, 1, 16, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600)) }
	series, err := client.Trend(context.Background(), "IF", 60)
	if err != nil {
		t.Fatal(err)
	}
	if primaryRequests.Load() != 0 || len(series.Rows) != 1 || series.Meta.Source != "cffex:futures-position" || series.Rows[0].NetPosition != -200 {
		t.Fatalf("primary calls=%d series=%+v", primaryRequests.Load(), series)
	}
	if strings.Contains(series.Meta.FallbackReason, "东方财富") || !strings.Contains(series.Meta.FallbackReason, "仅提供单日持仓") {
		t.Fatalf("misleading source notice: %s", series.Meta.FallbackReason)
	}
}

func TestExchangeOnlyFailureNamesCurrentSource(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := NewExchangeClient()
	client.http = server.Client()
	client.cffexURL = server.URL
	_, err := client.Trend(context.Background(), "IF", 60)
	if err == nil || !strings.Contains(err.Error(), "中金所") || strings.Contains(err.Error(), "东方财富") {
		t.Fatalf("misleading current-source error: %v", err)
	}
}
