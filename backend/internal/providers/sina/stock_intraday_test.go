package sina

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func archiveTestBody(t *testing.T, native string) string {
	t.Helper()
	fixtures := minuteFixtures(t)
	dates := []string{}
	for date := range fixtures {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	segments := []string{}
	for _, date := range dates {
		segments = append(segments, fixtures[date])
	}
	body, _ := json.Marshal(strings.Join(segments, ","))
	return "var MLC_" + native + "_2026_09=" + string(body) + ";\n/* public checksum */"
}

func TestHistoricalIntradayMonthCacheNativeFieldsAndIsolation(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/sz000001/hisdata/2026/09.js" || r.Method != "GET" || r.Header.Get("Referer") == "" {
			t.Errorf("invalid request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(archiveTestBody(t, "sz000001")))
	}))
	defer s.Close()
	c := NewHistoryIntradayClient(NewClient(WithIntradayHistoryBaseURL(s.URL)))
	defer c.Close()
	first, err := c.HistoryIntraday(context.Background(), "000001.SZ", "2026-09-18")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Lines) != 241 || first.PreviousClose != 11.61 || strings.Join(first.AvailableDates, ",") != "2026-09-30,2026-09-23,2026-09-18" {
		t.Fatalf("invalid count/header/dates: %+v", first)
	}
	stamp := first.Meta.FetchedAt
	var shares float64
	for _, line := range first.Lines {
		shares += line.Volume
		if line.Open != 0 || line.High != 0 || line.Low != 0 || line.Amount != 0 || line.AveragePrice <= 0 || line.PreviousClose != 11.61 || !foundation.FieldAvailable(line.Meta, "average_price") || foundation.FieldAvailable(line.Meta, "open") || foundation.FieldAvailable(line.Meta, "amount") {
			t.Fatalf("fabricated/missing native fields: %+v", line)
		}
	}
	if shares != 85303779 || first.Meta.VolumeUnit != "shares" || first.Meta.EffectiveAdjustment != "none" {
		t.Fatalf("wrong raw units: %v %+v", shares, first.Meta)
	}
	first.Meta.AvailableFields[0] = "poison"
	first.Lines[0].Meta.AvailableFields[0] = "poison"
	first.Lines[0].Close = -1
	first.AvailableDates[0] = "poison"
	second, err := c.HistoryIntraday(context.Background(), "000001.SZ", "2026-09-30")
	if err != nil || calls.Load() != 1 || len(second.Lines) != 241 || second.Lines[0].Close != 11.36 || second.Meta.AvailableFields[0] != "close" || !second.Meta.FetchedAt.Equal(stamp) {
		t.Fatalf("month reuse/isolation: %v calls=%d %+v", err, calls.Load(), second.Meta)
	}
	missing, err := c.HistoryIntraday(context.Background(), "000001.SZ", "2026-09-25")
	if err != nil || len(missing.Lines) != 0 || len(missing.AvailableDates) != 3 || missing.Meta.TradeDate != "2026-09-25" || calls.Load() != 1 {
		t.Fatalf("missing day substituted: %+v %v", missing, err)
	}
}

func TestHistoricalIntradayArchiveRejectsExecutableSuffixAndMalformedData(t *testing.T) {
	valid := archiveTestBody(t, "sz000001")
	for _, body := range []string{valid + "alert(1)", valid + "/* unterminated", strings.Replace(valid, "MLC_sz000001", "MLC_sh600000", 1), `var MLC_sz000001_2026_09="invalid";`, `var MLC_sz000001_2026_09="";`} {
		if _, err := parseIntradayArchive([]byte(body), "sz000001", "2026/09"); err == nil {
			t.Fatalf("accepted invalid archive: %.80s", body)
		}
	}
	if _, err := parseIntradayArchive([]byte(valid+" /* another\nblock */ \n"), "sz000001", "2026/09"); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(strings.Join([]string{minuteFixtures(t)["2026-09-18"], minuteFixtures(t)["2026-09-18"]}, ","))
	if _, err := parseIntradayArchive([]byte("var MLC_sz000001_2026_09="+string(encoded)+";"), "sz000001", "2026/09"); err == nil {
		t.Fatal("duplicate dates accepted")
	}
	if _, err := parseIntradayArchive([]byte(strings.Replace(valid, "_2026_09", "_2026_08", 1)), "sz000001", "2026/08"); err == nil {
		t.Fatal("wrong month accepted")
	}
}

func TestHistoricalIntradayTransportErrorsAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"http", 503, "private upstream text"}, {"size", 200, strings.Repeat("a", maxIntradayArchiveBytes+1)}, {"parse", 200, "not an archive"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			c := NewHistoryIntradayClient(NewClient(WithIntradayHistoryBaseURL(s.URL)))
			defer c.Close()
			_, err := c.HistoryIntraday(context.Background(), "000001.SZ", "2026-09-18")
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("missing/unsafe error: %v", err)
			}
			if tc.status == 200 && !errors.Is(err, foundation.ErrInvalidPriceData) {
				t.Fatalf("archive validation misclassified as transport: %v", err)
			}
			if tc.status == 503 {
				var status *foundation.PriceHTTPStatusError
				if !errors.As(err, &status) || status.StatusCode != 503 {
					t.Fatalf("HTTP status lost: %v", err)
				}
			}
		})
	}
}

func waitArchiveWaiters(t *testing.T, c *HistoryIntradayClient, count int) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		c.mu.Lock()
		found := false
		for _, entry := range c.months {
			if entry.flight != nil && entry.flight.waiters == count {
				found = true
			}
		}
		c.mu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("did not join %d month waiters", count)
}

func TestHistoricalIntradayJoinedMonthCancellationAndClose(t *testing.T) {
	body := archiveTestBody(t, "sz000001")
	var calls atomic.Int32
	started, release, canceled := make(chan struct{}, 3), make(chan struct{}), make(chan struct{}, 3)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			_, _ = w.Write([]byte(body))
		case <-r.Context().Done():
			canceled <- struct{}{}
		}
	}))
	defer s.Close()
	c := NewHistoryIntradayClient(NewClient(WithIntradayHistoryBaseURL(s.URL)))
	defer c.Close()
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	done1, done2 := make(chan error, 1), make(chan error, 1)
	go func() { _, e := c.HistoryIntraday(ctx1, "000001.SZ", "2026-09-18"); done1 <- e }()
	<-started
	go func() { _, e := c.HistoryIntraday(ctx2, "000001.SZ", "2026-09-30"); done2 <- e }()
	waitArchiveWaiters(t, c, 2)
	cancel1()
	if err := <-done1; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
	select {
	case <-canceled:
		t.Fatal("one waiter canceled shared upstream")
	default:
	}
	cancel2()
	if err := <-done2; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("last waiter did not cancel upstream")
	}
	done3 := make(chan error, 1)
	go func() { _, e := c.HistoryIntraday(context.Background(), "000001.SZ", "2026-09-18"); done3 <- e }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("new caller did not restart canceled month")
	}
	_ = c.Close()
	if err := <-done3; !errors.Is(err, context.Canceled) {
		t.Fatalf("Close lost cancel: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("joined calls=%d", calls.Load())
	}
	if _, err := c.HistoryIntraday(context.Background(), "000001.SZ", "2026-09-18"); !errors.Is(err, context.Canceled) {
		t.Fatal("closed provider made request")
	}
}

func TestHistoricalIntradayCacheExpiryAndBound(t *testing.T) {
	var calls atomic.Int32
	body := archiveTestBody(t, "sz000001")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = w.Write([]byte(body)) }))
	defer s.Close()
	c := NewHistoryIntradayClient(NewClient(WithIntradayHistoryBaseURL(s.URL)))
	defer c.Close()
	now := time.Now()
	c.now = func() time.Time { return now }
	if _, err := c.HistoryIntraday(context.Background(), "000001.SZ", "2026-09-18"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	if _, err := c.HistoryIntraday(context.Background(), "000001.SZ", "2026-09-18"); err != nil || calls.Load() != 2 {
		t.Fatalf("expiry: %v calls=%d", err, calls.Load())
	}
	c.mu.Lock()
	for i := 0; i < 32; i++ {
		c.months[time.Date(2000+i, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006")] = &intradayMonthEntry{accessedAt: now.Add(-time.Hour)}
	}
	delete(c.months, "sz000001:2026/09")
	c.mu.Unlock()
	if _, err := c.HistoryIntraday(context.Background(), "000001.SZ", "2026-09-18"); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	size := len(c.months)
	c.mu.Unlock()
	if size != 32 {
		t.Fatalf("month cache unbounded: %d", size)
	}
}

func TestLiveSinaHistoricalIntraday(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_INTRADAY_HISTORY") != "1" {
		t.Skip("explicit opt-in only")
	}
	c := NewHistoryIntradayClient(nil)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	for _, date := range []string{"2026-09-18", "2026-09-23", "2026-09-30"} {
		data, err := c.HistoryIntraday(ctx, "000001.SZ", date)
		if err != nil {
			t.Fatal(err)
		}
		if len(data.Lines) != 241 {
			t.Fatalf("%s count=%d", date, len(data.Lines))
		}
		var shares float64
		for _, line := range data.Lines {
			shares += line.Volume
		}
		t.Logf("date=%s count=%d first=%s last=%s previous=%v close=%v average=%v shares=%v archive_dates=%d", date, len(data.Lines), data.Lines[0].Time.Format("15:04"), data.Lines[240].Time.Format("15:04"), data.PreviousClose, data.Lines[240].Close, data.Lines[240].AveragePrice, shares, len(data.AvailableDates))
	}
}
