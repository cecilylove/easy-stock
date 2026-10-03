package eastmoney

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

func TestAnnouncementBodiesBoundConcurrencyAndReportContentQuality(t *testing.T) {
	var active, maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/security/ann" {
			rows := make([]string, 12)
			for i := range rows {
				rows[i] = fmt.Sprintf(`{"art_code":"A%d","title":"公告%d","notice_date":"2026-09-30"}`, i, i)
			}
			fmt.Fprintf(w, `{"success":1,"data":{"list":[%s]}}`, strings.Join(rows, ","))
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
		switch r.URL.Query().Get("art_code") {
		case "A0":
			http.Error(w, "no body", http.StatusNotFound)
		case "A1":
			fmt.Fprintf(w, `{"data":{"notice_content":%q}}`, strings.Repeat("字", 8100))
		default:
			fmt.Fprint(w, `{"data":{"notice_content":"真实公告正文"}}`)
		}
	}))
	defer server.Close()
	items, meta, err := NewClient(WithAnnouncementBaseURL(server.URL), WithHTTPClient(server.Client())).MarketAnnouncements(context.Background(), "", "", "", 12)
	if err != nil || len(items) != 12 {
		t.Fatalf("%+v %v", items, err)
	}
	if maximum.Load() > 4 || maximum.Load() < 2 {
		t.Fatalf("unexpected body concurrency %d", maximum.Load())
	}
	if !meta.Partial || len(meta.MissingIDs) != 2 || items[0].ContentStatus != "unavailable" || items[1].ContentStatus != "truncated" || items[2].ContentStatus != "available" {
		t.Fatalf("body status lost %+v %+v", items, meta)
	}
}
func TestRawBillboardDetailDoesNotVisitAnotherSupplier(t *testing.T) {
	var thsCalls atomic.Int32
	ths := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { thsCalls.Add(1); fmt.Fprint(w, "unwanted") }))
	defer ths.Close()
	raw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"result":{"data":[{"OPERATEDEPT_NAME":"样本席位","BUY_AMT_REAL":10,"SELL_AMT_REAL":0,"RANK":1}]}}`)
	}))
	defer raw.Close()
	_, _, err := NewClient(WithF10BaseURL(raw.URL), WithTHSBaseURL(ths.URL), WithHTTPClient(raw.Client())).MarketBillboardDetail(context.Background(), "600001.SH", "2026-09-30", "日涨幅")
	if err != nil || thsCalls.Load() != 0 {
		t.Fatalf("implicit supplier access: %d %v", thsCalls.Load(), err)
	}
}
