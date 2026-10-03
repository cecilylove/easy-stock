package eastmoney

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestLimitUpNumericPresenceAndCacheClone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"rc":0,"data":{"pool":[{"c":"600001","p":0,"zdp":"0","amount":null,"ltsz":"--","hs":"NaN","lbc":0,"zbc":0,"zttj":{"days":0,"ct":0}},{"c":"600002","p":2000,"amount":2}]}}`)
	}))
	defer server.Close()
	client := NewClient(WithTopicBaseURL(server.URL))
	date := time.Date(2026, 8, 7, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	events, err := client.LimitUpPool(context.Background(), date)
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	event := events[0]
	if !event.Meta.FieldsKnown || event.Meta.TradeDate != "2026-08-07" {
		t.Fatalf("missing strict date/meta: %+v", event.Meta)
	}
	for _, field := range []string{"price", "change_percent", "streak", "open_count", "days", "count"} {
		if !foundation.LimitUpFieldAvailable(event, field) {
			t.Errorf("valid zero %s missing", field)
		}
	}
	for _, field := range []string{"amount", "float_market_cap", "turnover_rate"} {
		if foundation.LimitUpFieldAvailable(event, field) {
			t.Errorf("missing/nonfinite %s available", field)
		}
	}
	events[0].Meta.AvailableFields[0] = "mutated"
	events[0].Meta.FieldSources = map[string]string{"price": "mutated"}
	again, err := client.LimitUpPool(context.Background(), date)
	if err != nil || again[0].Meta.AvailableFields[0] == "mutated" || again[0].Meta.FieldSources["price"] != "" || foundation.LimitUpFieldAvailable(again[1], "open_count") {
		t.Fatalf("cache or row mask contaminated: %+v %v", again, err)
	}
}
func integrityTradingDates(days int) []string {
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	var dates []string
	for offset := days - 1; offset >= 0; offset-- {
		date := now.AddDate(0, 0, -offset)
		if foundation.IsAStockTradingDay(date) {
			dates = append(dates, date.Format("2006-01-02"))
		}
	}
	return dates
}
func TestRecentLimitUpOrdinaryAndProgressiveKeepEveryFailedDate(t *testing.T) {
	const days = 40
	dates := integrityTradingDates(days)
	if len(dates) < 3 {
		t.Fatal("need trading dates")
	}
	missing := []string{dates[0], dates[1]}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		date, _ := time.Parse("20060102", r.URL.Query().Get("date"))
		key := date.Format("2006-01-02")
		if key == missing[0] || key == missing[1] {
			fmt.Fprint(w, `{"rc":42,"data":null}`)
			return
		}
		if key == dates[2] {
			fmt.Fprint(w, `{"rc":0,"data":{"pool":[]}}`)
			return
		}
		fmt.Fprint(w, `{"rc":0,"data":{"pool":[{"c":"600001","p":1000}]}}`)
	}))
	defer server.Close()
	client := NewClient(WithTopicBaseURL(server.URL))
	for _, progressive := range []bool{false, true} {
		var events []foundation.LimitUpEvent
		var err error
		if progressive {
			events, err = client.ProgressiveRecentLimitUps(context.Background(), days, func(items []foundation.LimitUpEvent) {
				if len(items) > 0 {
					items[0].Meta.AvailableFields[0] = "mutated"
				}
			})
		} else {
			events, err = client.RecentLimitUps(context.Background(), days)
		}
		var typed *foundation.LimitUpCoverageError
		if !errors.As(err, &typed) || !reflect.DeepEqual(typed.MissingDates, missing) || len(typed.CoveredDates) != len(dates)-2 || len(events) != len(dates)-3 {
			t.Fatalf("history completeness lost: events=%d coverage=%+v err=%v", len(events), typed, err)
		}
		for _, event := range events {
			if !event.Meta.Partial || !reflect.DeepEqual(event.Meta.MissingIDs, missing) || event.Meta.AvailableFields[0] == "mutated" {
				t.Fatalf("partial/meta mutation lost: %+v", event)
			}
		}
	}
}
func TestLimitUpValidEmptyDiffersFromMissingPool(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		wantErr       bool
	}{
		{"empty", `{"rc":0,"data":{"pool":[]}}`, false},
		{"null data", `{"rc":0,"data":null}`, true},
		{"null pool", `{"rc":0,"data":{"pool":null}}`, true},
		{"missing pool", `{"rc":0,"data":{}}`, true},
		{"invalid row", `{"rc":0,"data":{"pool":[{"c":"invalid"}]}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.payload) }))
			defer server.Close()
			events, err := NewClient(WithTopicBaseURL(server.URL)).LimitUpPool(context.Background(), time.Now())
			if (err != nil) != tc.wantErr || len(events) != 0 {
				t.Fatalf("events=%+v err=%v", events, err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"rc":0,"data":{"pool":[]}}`) }))
	defer server.Close()
	events, err := NewClient(WithTopicBaseURL(server.URL)).RecentLimitUps(context.Background(), 40)
	if err != nil || len(events) != 0 {
		t.Fatalf("all legal empty dates should succeed: %+v %v", events, err)
	}
}
func TestRecentLimitUpCancellationStopsWork(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{}, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	client := NewClient(WithTopicBaseURL(server.URL))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.RecentLimitUps(ctx, 40); done <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		var typed *foundation.LimitUpCoverageError
		if !errors.Is(err, context.Canceled) || !errors.As(err, &typed) || len(typed.MissingDates) == 0 {
			t.Fatalf("cancellation coverage lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop immediately")
	}
	if calls.Load() > 3 {
		t.Fatalf("queued work continued: %d", calls.Load())
	}
	before := calls.Load()
	_, err := client.RecentLimitUps(ctx, 40)
	if !errors.Is(err, context.Canceled) || calls.Load() != before {
		t.Fatalf("canceled context started requests: %v", err)
	}
}
