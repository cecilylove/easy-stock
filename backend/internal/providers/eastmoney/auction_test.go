package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuctionTraceOnlyReturnsIndicativePoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/qt/stock/trends2/get" || r.URL.Query().Get("secid") != "1.600519" || r.URL.Query().Get("ndays") != "1" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rc":0,"data":{"trends":["2026-09-30 09:15,10,10,10,10,0,0.00,10","2026-09-30 09:25,10.20,10.30,10.30,10.20,0,0.00,10","2026-09-30 09:26,10.35,10.35,10.35,10.35,12,12420.00,10.35","2026-09-30 09:30,10.40,10.40,10.40,10.40,4,4160.00,10.40"]}}`))
	}))
	defer server.Close()
	client := NewClient(WithQuoteBaseURL(server.URL))
	got, err := client.AuctionTrace(context.Background(), "600519.SH")
	if err != nil {
		t.Fatal(err)
	}
	if got.Symbol != "600519.SH" || got.TradeDate != "2026-09-30" || len(got.Points) != 2 || got.Points[1].Price != 10.30 || got.Points[1].Amount != 0 {
		t.Fatalf("pre-open points confused with execution: %+v", got)
	}
	if got.Meta.Source != "eastmoney:pre-open" {
		t.Fatalf("source metadata missing: %+v", got)
	}
}

func TestAuctionTraceFallsBackToQuoteMirror(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unavailable", http.StatusBadGateway) }))
	defer primary.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"rc":0,"data":{"trends":["2026-09-30 09:15,10,10,10,10,0,0,10"]}}`))
	}))
	defer mirror.Close()
	client := NewClient(WithQuoteBaseURL(primary.URL), WithQuoteFallbackBaseURLs(mirror.URL))
	got, err := client.AuctionTrace(context.Background(), "600519.SH")
	if err != nil || len(got.Points) != 1 || !strings.Contains(got.Meta.FallbackReason, "备用节点") || !strings.Contains(got.Meta.SourceURL, mirror.URL) {
		t.Fatalf("mirror failed: %+v err=%v", got, err)
	}
}

func TestAuctionTraceReservesTimeForFinalMirror(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(3500 * time.Millisecond):
			http.Error(w, "slow", http.StatusBadGateway)
		}
	}))
	defer slow.Close()
	last := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"rc":0,"data":{"trends":["2026-09-30 09:15,10,10,10,10,0,0,10"]}}`))
	}))
	defer last.Close()
	client := NewClient(WithQuoteBaseURL(slow.URL), WithQuoteFallbackBaseURLs(slow.URL, last.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	got, err := client.AuctionTrace(ctx, "600519.SH")
	if err != nil || len(got.Points) != 1 || !strings.Contains(got.Meta.SourceURL, last.URL) {
		t.Fatalf("last mirror starved: %+v err=%v", got, err)
	}
}

func TestAuctionTraceRejectsInvalidAndUnorderedPoints(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []string
	}{
		{"missing preopen", []string{"2026-09-30 09:30,10,10,10,10,1,10,10"}},
		{"malformed", []string{"bad,row"}},
		{"mixed days", []string{"2026-09-29 09:15,10,10,10,10,0,0,10", "2026-09-30 09:25,10,10,10,10,0,0,10"}},
		{"unordered", []string{"2026-09-30 09:25,10,10,10,10,0,0,10", "2026-09-30 09:15,10,10,10,10,0,0,10"}},
		{"NaN price", []string{"2026-09-30 09:15,NaN,NaN,NaN,NaN,0,0,NaN"}},
		{"infinite volume", []string{"2026-09-30 09:15,10,10,10,10,+Inf,0,10"}},
		{"infinite amount", []string{"2026-09-30 09:15,10,10,10,10,0,+Inf,10"}},
		{"negative amount", []string{"2026-09-30 09:15,10,10,10,10,0,-1,10"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseAuctionTrends("600519.SH", test.rows); err == nil {
				t.Fatal("invalid source must not produce auction prices")
			}
		})
	}
}

func TestAuctionTraceNeverAttributes09_26OrLaterToAuction(t *testing.T) {
	trace, err := parseAuctionTrends("000001.SZ", []string{
		"2026-09-30 09:15,10,10,10,10,0,0,10",
		"2026-09-30 09:25,10,10,10,10,0,0,10",
		"2026-09-30 09:26,10,10,10,10,0,0,10",
		"2026-09-30 09:27,10,10,10,10,3,30,10",
	})
	if err != nil || len(trace.Points) != 2 {
		t.Fatalf("09:26+ cannot be mistaken for indicative auction prices: %+v err=%v", trace, err)
	}
	if trace.Points[0].Time.Location().String() != "CST" || !strings.HasPrefix(trace.Points[0].Time.Format(time.RFC3339), "2026-09-30T09:15") {
		t.Fatalf("unexpected auction timezone: %+v", trace.Points[0])
	}
}
