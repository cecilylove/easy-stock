package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/sina"
)

func intradayTestLines(date string) []foundation.KLine {
	day, _ := time.ParseInLocation("2006-01-02", date, stockIntradayZone)
	lines := []foundation.KLine{}
	for _, session := range [][2]int{{571, 690}, {781, 900}} {
		for minute := session[0]; minute <= session[1]; minute++ {
			lines = append(lines, foundation.KLine{Symbol: "000001.SZ", Time: day.Add(time.Duration(minute) * time.Minute), Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 100,
				Meta: foundation.SourceMeta{Source: "sina", FetchedAt: time.Date(2026, 9, 30, 16, 0, 0, 0, stockIntradayZone), FieldsKnown: true, AvailableFields: []string{"open", "high", "low", "close", "volume"}, VolumeUnit: "shares"}})
		}
	}
	return lines
}

func requestIntraday(t *testing.T, s *Server, date string) (stockIntradayData, int) {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/intraday?symbol=000001.SZ&date="+date, nil))
	var payload struct {
		Data stockIntradayData `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return payload.Data, w.Code
}

func TestIntradayDateSelectionCoverageAndNoTodayFallback(t *testing.T) {
	var calls atomic.Int32
	samples := append(intradayTestLines("2026-09-29")[100:], intradayTestLines("2026-09-30")...)
	s := NewServer(Config{Intraday: klineProviderFunc(func(_ context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
		calls.Add(1)
		if symbol != "000001.SZ" || period != "1" || limit != 1000 {
			t.Errorf("wrong raw sample request %s %s %d", symbol, period, limit)
		}
		return samples, nil
	})})
	defer s.Close()
	for _, test := range []struct {
		date, status string
		count        int
	}{{"2026-09-30", "available", 240}, {"2026-09-29", "partial", 140}, {"2026-09-28", "unavailable", 0}} {
		data, code := requestIntraday(t, s, test.date)
		if code != 200 || data.Availability != test.status || len(data.Lines) != test.count || data.TradeDate != test.date || data.PreviousClose != nil {
			t.Fatalf("%s: %d %+v", test.date, code, data)
		}
		if strings.Join(data.AvailableDates, ",") != "2026-09-30,2026-09-29" || data.Meta.Source != "sina" || data.Meta.TradeDate != test.date {
			t.Fatalf("coverage/meta %+v", data)
		}
		for _, line := range data.Lines {
			if line.Time.In(stockIntradayZone).Format("2006-01-02") != test.date {
				t.Fatal("another day substituted")
			}
		}
	}
	_, _ = requestIntraday(t, s, "2026-09-30")
	if calls.Load() != 3 {
		t.Fatalf("date cache keys overlap/repeated call uncached: %d", calls.Load())
	}
}

func TestIntradayCompletenessRequiresEveryMinuteAndReliableBaseline(t *testing.T) {
	lines := intradayTestLines("2026-09-30")
	now := time.Date(2026, 9, 30, 14, 59, 0, 0, stockIntradayZone)
	if selectStockIntraday(lines, "000001.SZ", "2026-09-30", now).Availability != "partial" {
		t.Fatal("unfinished day called complete")
	}
	now = now.Add(time.Minute)
	missing := append(append([]foundation.KLine{}, lines[:60]...), lines[61:]...)
	if selectStockIntraday(missing, "000001.SZ", "2026-09-30", now).Availability != "partial" {
		t.Fatal("endpoints hid missing middle minute")
	}
	for i := range lines {
		lines[i].PreviousClose = 9.5
	}
	if selectStockIntraday(lines, "000001.SZ", "2026-09-30", now).PreviousClose != nil {
		t.Fatal("unadvertised numeric baseline accepted")
	}
	for i := range lines {
		lines[i].Meta.AvailableFields = append(lines[i].Meta.AvailableFields, "previous_close")
	}
	data := selectStockIntraday(lines, "000001.SZ", "2026-09-30", now)
	if data.PreviousClose == nil || *data.PreviousClose != 9.5 {
		t.Fatal("explicit consistent baseline lost")
	}
	lines[len(lines)-1].PreviousClose = 9
	if selectStockIntraday(lines, "000001.SZ", "2026-09-30", now).PreviousClose != nil {
		t.Fatal("changing minute baseline accepted")
	}
}

func TestIntradaySinaClosingAuctionLabelsDoNotInventMissingMinutes(t *testing.T) {
	lines := intradayTestLines("2026-09-30")
	lines = append(append([]foundation.KLine{}, lines[:237]...), lines[239])
	data := selectStockIntraday(lines, "000001.SZ", "2026-09-30", time.Now())
	if data.Availability != "available" || len(data.Lines) != 238 {
		t.Fatalf("actual Sina session mislabeled %+v", data)
	}
	data = selectStockIntraday(lines[:237], "000001.SZ", "2026-09-30", time.Now())
	if data.Availability != "partial" {
		t.Fatal("missing actual closing candle accepted")
	}
}

func TestIntradayRejectsNegativeOrZeroTransactionPrices(t *testing.T) {
	for _, low := range []float64{-1, 0} {
		lines := intradayTestLines("2026-09-30")
		lines[0].Low = low
		s := NewServer(Config{Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) { return lines, nil })})
		data, code := requestIntraday(t, s, "2026-09-30")
		s.Close()
		if code != 502 || data.Availability != "unavailable" || len(data.Lines) != 0 {
			t.Fatalf("nonpositive transaction prices accepted: %d %+v", code, data)
		}
	}
}

func TestIntradayConcurrentJoinAndCancellation(t *testing.T) {
	started, release, cancelled := make(chan struct{}, 2), make(chan struct{}), make(chan struct{}, 2)
	var calls atomic.Int32
	s := NewServer(Config{Intraday: klineProviderFunc(func(ctx context.Context, _ string, _ string, _ int) ([]foundation.KLine, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return intradayTestLines("2026-09-30"), nil
		case <-ctx.Done():
			cancelled <- struct{}{}
			return nil, ctx.Err()
		}
	})})
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done1, done2 := make(chan struct{}), make(chan int, 1)
	go func() {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/quotes/intraday?symbol=000001.SZ&date=2026-09-30", nil).WithContext(ctx)
		s.ServeHTTP(httptest.NewRecorder(), r)
		close(done1)
	}()
	<-started
	go func() {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/intraday?symbol=000001.SZ&date=2026-09-30", nil))
		done2 <- w.Code
	}()
	deadline := time.Now().Add(time.Second)
	for {
		s.stockIntraday.mu.Lock()
		entry := s.stockIntraday.entries["000001.SZ:2026-09-30"]
		joined := entry.flight != nil && entry.flight.waiters == 2
		s.stockIntraday.mu.Unlock()
		if joined {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second caller did not join")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done1
	select {
	case <-cancelled:
		t.Fatal("one cancelled waiter cancelled remaining viewer")
	default:
	}
	close(release)
	if code := <-done2; code != 200 || calls.Load() != 1 {
		t.Fatalf("joined request status=%d calls=%d", code, calls.Load())
	}
}

func TestIntradayLastWaiterAndCloseCancelUpstream(t *testing.T) {
	for _, closeServer := range []bool{false, true} {
		t.Run(map[bool]string{false: "last-waiter", true: "close"}[closeServer], func(t *testing.T) {
			started, cancelled := make(chan struct{}), make(chan struct{})
			s := NewServer(Config{Intraday: klineProviderFunc(func(ctx context.Context, _ string, _ string, _ int) ([]foundation.KLine, error) {
				close(started)
				<-ctx.Done()
				close(cancelled)
				return nil, ctx.Err()
			})})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer s.Close()
			done := make(chan struct{})
			go func() {
				r := httptest.NewRequest(http.MethodGet, "/api/v1/quotes/intraday?symbol=000001.SZ&date=2026-09-30", nil).WithContext(ctx)
				s.ServeHTTP(httptest.NewRecorder(), r)
				close(done)
			}()
			<-started
			if closeServer {
				s.closeStockIntraday()
			} else {
				cancel()
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("upstream not cancelled")
			}
			<-done
			s.stockIntraday.mu.Lock()
			entry := s.stockIntraday.entries["000001.SZ:2026-09-30"]
			saved := entry.hasValue
			cooldown := entry.retryAt
			s.stockIntraday.mu.Unlock()
			if saved || !cooldown.IsZero() {
				t.Fatal("abandoned request cached or penalized")
			}
		})
	}
}

func TestIntradayStaleCacheKeepsRequestedDayAndOriginalFetchTime(t *testing.T) {
	var fail atomic.Bool
	s := NewServer(Config{Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		if fail.Load() {
			return nil, errors.New("timeout")
		}
		return intradayTestLines("2026-09-30"), nil
	})})
	defer s.Close()
	data, code := requestIntraday(t, s, "2026-09-30")
	if code != 200 {
		t.Fatal(code)
	}
	fetched := data.Meta.FetchedAt
	s.stockIntraday.now = func() time.Time { return time.Now().Add(6 * time.Second) }
	fail.Store(true)
	data, code = requestIntraday(t, s, "2026-09-30")
	if code != 200 || !data.Meta.Stale || data.TradeDate != "2026-09-30" || len(data.Lines) != 240 || data.Meta.FetchedAt != fetched || !data.Lines[0].Meta.Stale {
		t.Fatalf("stale contract %d %+v", code, data)
	}
}

func TestIntradayShanghaiDateAndSourcePreservation(t *testing.T) {
	line := intradayTestLines("2026-09-30")[0]
	line.Time = line.Time.UTC()
	data := selectStockIntraday([]foundation.KLine{line}, "000001.SZ", "2026-09-30", time.Now())
	if len(data.Lines) != 1 || data.Meta.FetchedAt != line.Meta.FetchedAt || data.Meta.Source != "sina" || data.Meta.Partial != true {
		t.Fatalf("timezone/source loss: %+v", data)
	}
}

func TestIntradayErrorsValidationAndAuthenticationMakeNoFallback(t *testing.T) {
	var calls atomic.Int32
	s := NewServer(Config{Token: "secret", Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		calls.Add(1)
		return nil, errors.New("https://upstream.invalid?credential=private")
	})})
	defer s.Close()
	for _, test := range []struct {
		query, token, origin string
		status               int
	}{
		{"symbol=000001.SZ&date=2026-09-30", "", "", 401},
		{"symbol=000001.SZ&date=2026-09-30", "secret", "https://external.invalid", 403},
		{"symbol=000001.SZ&date=bad", "secret", "", 400},
		{"symbol=000001.SZ&date=2099-01-01", "secret", "", 400},
		{"symbol=bad&date=2026-09-30", "secret", "", 400},
		{"symbol=000001.SZ&date=2026-09-30&provider=eastmoney", "secret", "", 400},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/quotes/intraday?"+test.query, nil)
		if test.token != "" {
			r.Header.Set("Authorization", "Bearer "+test.token)
		}
		if test.origin != "" {
			r.Header.Set("Origin", test.origin)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s: %d %s", test.query, w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid/auth request attempted upstream")
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/quotes/intraday?symbol=000001.SZ&date=2026-09-30", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 502 || strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), `"availability":"unavailable"`) || calls.Load() != 1 {
		t.Fatalf("unsafe/unstructured failure %d %s", w.Code, w.Body.String())
	}
}

func TestLiveRecentSinaIntradaySample(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_INTRADAY") != "1" {
		t.Skip("explicit opt-in only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lines, err := sina.NewClient().KLine(ctx, "000001.SZ", "1", 1000)
	if err != nil {
		t.Fatal(err)
	}
	lines, err = normalizeKLineContract(lines, "000001.SZ", "1", "source", "")
	if err != nil {
		t.Fatal(err)
	}
	data := selectStockIntraday(lines, "000001.SZ", "2026-09-30", time.Now())
	t.Logf("source=%s sample_count=%d requested_count=%d coverage=%v availability=%s", data.Meta.Source, len(lines), len(data.Lines), data.AvailableDates, data.Availability)
	if len(data.Lines) > 0 {
		t.Logf("first=%s last=%s", data.Lines[0].Time.Format(time.RFC3339), data.Lines[len(data.Lines)-1].Time.Format(time.RFC3339))
		minutes := map[string]bool{}
		for _, line := range data.Lines {
			minutes[line.Time.Format("15:04")] = true
		}
		missing := []string{}
		for _, session := range [][2]int{{571, 690}, {781, 900}} {
			for minute := session[0]; minute <= session[1]; minute++ {
				key := time.Date(2026, 9, 30, minute/60, minute%60, 0, 0, stockIntradayZone).Format("15:04")
				if !minutes[key] {
					missing = append(missing, key)
				}
			}
		}
		t.Logf("missing_regular_grid=%v", missing)
	}
}

type historyIntradayProviderFunc func(context.Context, string, string) (foundation.StockIntradayHistory, error)

func (f historyIntradayProviderFunc) HistoryIntraday(ctx context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
	return f(ctx, symbol, date)
}

func intradayTestArchive(date string) foundation.StockIntradayHistory {
	day, _ := time.ParseInLocation("2006-01-02", date, stockIntradayZone)
	meta := foundation.SourceMeta{Source: "sina:historical-intraday", Provider: "sina", Period: "1", EffectiveAdjustment: "none", BasisID: "sina:historical-intraday:none", VolumeUnit: "shares", FieldsKnown: true, AvailableFields: []string{"close", "average_price", "volume", "previous_close"}, FetchedAt: day.Add(16 * time.Hour)}
	archive := foundation.StockIntradayHistory{Symbol: "000001.SZ", TradeDate: date, PreviousClose: 9.5, AvailableDates: []string{date}, Meta: meta}
	for _, session := range [][2]int{{570, 690}, {781, 900}} {
		for minute := session[0]; minute <= session[1]; minute++ {
			archive.Lines = append(archive.Lines, foundation.KLine{Symbol: archive.Symbol, Time: day.Add(time.Duration(minute) * time.Minute), Close: 10.5, AveragePrice: 10.3, PreviousClose: 9.5, Volume: 100, Meta: meta})
		}
	}
	return archive
}

func TestIntradayHistoryPreferredAndRawFieldContract(t *testing.T) {
	var histories, samples atomic.Int32
	s := NewServer(Config{HistoryIntraday: historyIntradayProviderFunc(func(_ context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
		histories.Add(1)
		if symbol != "000001.SZ" {
			t.Errorf("symbol=%s", symbol)
		}
		archive := intradayTestArchive(date)
		archive.AvailableDates = []string{"2026-09-30", "2026-09-23", "2026-09-18"}
		return archive, nil
	}), Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		samples.Add(1)
		return nil, errors.New("must not fallback")
	})})
	defer s.Close()
	for _, date := range []string{"2026-09-18", "2026-09-23"} {
		data, code := requestIntraday(t, s, date)
		if code != 200 || data.Availability != "available" || len(data.Lines) != 241 || data.PreviousClose == nil || *data.PreviousClose != 9.5 || data.Meta.Source != "sina:historical-intraday" || data.Meta.Capability != "stock-intraday:history" || len(data.AvailableDates) != 3 {
			t.Fatalf("archive result %d %+v", code, data)
		}
		for _, line := range data.Lines {
			if line.Open != 0 || line.High != 0 || line.Low != 0 || line.Amount != 0 || line.AveragePrice != 10.3 || foundation.FieldAvailable(line.Meta, "amount") {
				t.Fatalf("fabricated archive field %+v", line)
			}
		}
	}
	_, _ = requestIntraday(t, s, "2026-09-18")
	if histories.Load() != 2 || samples.Load() != 0 {
		t.Fatalf("archive calls=%d sample=%d", histories.Load(), samples.Load())
	}
}

func TestIntradayHistoryUnavailableOnlyFallsBackToSameDay(t *testing.T) {
	for _, scenario := range []string{"archive-error", "date-absent", "no-same-day"} {
		t.Run(scenario, func(t *testing.T) {
			s := NewServer(Config{HistoryIntraday: historyIntradayProviderFunc(func(_ context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
				archive := intradayTestArchive(date)
				archive.AvailableDates = []string{"2026-09-30", "2026-09-18"}
				archive.Lines = []foundation.KLine{}
				if scenario == "archive-error" {
					return archive, errors.New("private remote credentials")
				}
				return archive, nil
			}), Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
				if scenario == "no-same-day" {
					return intradayTestLines("2026-09-30"), nil
				}
				return intradayTestLines("2026-09-23")[100:], nil
			})})
			defer s.Close()
			data, code := requestIntraday(t, s, "2026-09-23")
			if code != 200 || strings.Contains(data.Message, "private") {
				t.Fatalf("result %d %+v", code, data)
			}
			if scenario == "no-same-day" {
				if data.Availability != "unavailable" || len(data.Lines) != 0 || strings.Join(data.AvailableDates, ",") != "2026-09-30,2026-09-18" {
					t.Fatalf("substituted other date %+v", data)
				}
				return
			}
			if data.Availability != "partial" || len(data.Lines) != 140 || data.Meta.Source != "sina" || data.Meta.FallbackReason == "" {
				t.Fatalf("invalid sample fallback %+v", data)
			}
			for _, line := range data.Lines {
				if line.Time.In(stockIntradayZone).Format("2006-01-02") != "2026-09-23" || line.Meta.Source != "sina" || line.AveragePrice != 0 {
					t.Fatal("source/date data merged")
				}
			}
		})
	}
}

func TestIntradayArchiveRefreshFailureDoesNotOverwriteCachedDay(t *testing.T) {
	var fail atomic.Bool
	var histories, samples atomic.Int32
	s := NewServer(Config{HistoryIntraday: historyIntradayProviderFunc(func(_ context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
		histories.Add(1)
		archive := intradayTestArchive(date)
		if fail.Load() {
			archive.Lines = nil
			return archive, &foundation.PriceHTTPStatusError{Provider: "sina", StatusCode: 503}
		}
		return archive, nil
	}), Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		samples.Add(1)
		return intradayTestLines("2026-09-30"), nil
	})})
	defer s.Close()
	first, code := requestIntraday(t, s, "2026-09-23")
	if code != 200 || len(first.Lines) != 241 {
		t.Fatalf("initial archive %d %+v", code, first)
	}
	fetched := first.Meta.FetchedAt
	clock := time.Now().Add(6 * time.Second)
	s.stockIntraday.now = func() time.Time { return clock }
	fail.Store(true)
	second, code := requestIntraday(t, s, "2026-09-23")
	if code != 200 || second.Availability != "available" || !second.Meta.Stale || len(second.Lines) != 241 || !second.Lines[0].Meta.Stale || second.TradeDate != "2026-09-23" || second.Meta.FetchedAt != fetched || second.Meta.Source != "sina:historical-intraday" {
		t.Fatalf("archive refresh erased cached day: %d %+v", code, second)
	}
	s.sourceHealth.mu.RLock()
	failed := s.sourceHealth.capabilities["sina"]["stock-intraday:history"]
	s.sourceHealth.mu.RUnlock()
	_, _ = requestIntraday(t, s, "2026-09-23")
	s.sourceHealth.mu.RLock()
	after := s.sourceHealth.capabilities["sina"]["stock-intraday:history"]
	s.sourceHealth.mu.RUnlock()
	if histories.Load() != 2 || samples.Load() != 1 || !failed.failed || after.checkedAt != failed.checkedAt {
		t.Fatalf("stale/cache renewed requests or observation histories=%d samples=%d before=%+v after=%+v", histories.Load(), samples.Load(), failed, after)
	}
	s.stockIntraday.mu.Lock()
	saved := s.stockIntraday.entries["000001.SZ:2026-09-23"].value
	s.stockIntraday.mu.Unlock()
	if saved.Meta.Stale || len(saved.Lines) != 241 || saved.Lines[0].Meta.Stale {
		t.Fatal("stale response mutated original cache")
	}
}

func TestIntradayFreshArchiveMissingDayReplacesOldCacheWithoutStale(t *testing.T) {
	var missing atomic.Bool
	s := NewServer(Config{HistoryIntraday: historyIntradayProviderFunc(func(_ context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
		archive := intradayTestArchive(date)
		if missing.Load() {
			archive.Lines = nil
			archive.AvailableDates = []string{"2026-09-30"}
		}
		return archive, nil
	}), Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		return intradayTestLines("2026-09-30"), nil
	})})
	defer s.Close()
	_, code := requestIntraday(t, s, "2026-09-23")
	if code != 200 {
		t.Fatal(code)
	}
	s.stockIntraday.now = func() time.Time { return time.Now().Add(6 * time.Second) }
	missing.Store(true)
	data, code := requestIntraday(t, s, "2026-09-23")
	if code != 200 || data.Availability != "unavailable" || data.Meta.Stale || len(data.Lines) != 0 || strings.Join(data.AvailableDates, ",") != "2026-09-30" {
		t.Fatalf("fresh missing archive disguised as old data %d %+v", code, data)
	}
}

func TestIntradayArchiveFailureWithoutCachedDayIsStructured(t *testing.T) {
	s := NewServer(Config{HistoryIntraday: historyIntradayProviderFunc(func(_ context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
		archive := intradayTestArchive(date)
		archive.Lines = nil
		return archive, &foundation.PriceHTTPStatusError{Provider: "sina", StatusCode: 503}
	}), Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		return intradayTestLines("2026-09-30"), nil
	})})
	defer s.Close()
	data, code := requestIntraday(t, s, "2026-09-23")
	if code != 502 || data.Availability != "unavailable" || data.Meta.Stale || len(data.Lines) != 0 || data.TradeDate != "2026-09-23" {
		t.Fatalf("uncached failure looked fresh available %d %+v", code, data)
	}
}

func TestIntradayTypedErrorsStaySymbolScopedUnlessServiceFailed(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		market bool
	}{{"missing-month-404", &foundation.PriceHTTPStatusError{Provider: "sina", StatusCode: 404}, false}, {"no-data", foundation.ErrPriceNoData, false}, {"invalid-data", foundation.ErrInvalidPriceData, false}, {"service-503", &foundation.PriceHTTPStatusError{Provider: "sina", StatusCode: 503}, true}, {"rate-429", &foundation.PriceHTTPStatusError{Provider: "sina", StatusCode: 429}, true}, {"deadline", context.DeadlineExceeded, true}} {
		for _, historyFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/history=%v", test.name, historyFailure), func(t *testing.T) {
				cfg := Config{Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) { return nil, test.err })}
				if historyFailure {
					cfg.HistoryIntraday = historyIntradayProviderFunc(func(_ context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
						archive := intradayTestArchive(date)
						archive.Lines = nil
						return archive, test.err
					})
					cfg.Intraday = klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
						return intradayTestLines("2026-09-30"), nil
					})
				}
				s := NewServer(cfg)
				defer s.Close()
				s.intradaySourceID = "sina"
				data, code := requestIntraday(t, s, "2026-09-23")
				if code != 502 || data.Availability != "unavailable" {
					t.Fatalf("error not structured %d %+v", code, data)
				}
				capability := "stock-intraday:recent-sample"
				if historyFailure {
					capability = "stock-intraday:history"
				}
				if !test.market {
					capability += ":symbol:000001.SZ"
				}
				s.sourceHealth.mu.RLock()
				observation, ok := s.sourceHealth.capabilities["sina"][capability]
				s.sourceHealth.mu.RUnlock()
				if !ok || !observation.failed {
					t.Fatalf("missing classified diagnostic %s %+v", capability, observation)
				}
				if !historyFailure {
					entry := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "sina")
					if test.market && entry.Status != "degraded" || !test.market && entry.Status != "unknown" {
						t.Fatalf("whole supplier falsely classified %+v", entry)
					}
				}
				if len(s.kLineRoutes.failures) != 0 {
					t.Fatal("intraday errors poisoned generic price cooldown")
				}
			})
		}
	}
}

func TestIntradayHistoryRejectsMixedBasisInvalidFieldsAndGrid(t *testing.T) {
	mutations := []func(*foundation.StockIntradayHistory){
		func(a *foundation.StockIntradayHistory) { a.TradeDate = "2026-09-30" },
		func(a *foundation.StockIntradayHistory) { a.Meta.Stale = true },
		func(a *foundation.StockIntradayHistory) { a.Meta.EffectiveAdjustment = "qfq" },
		func(a *foundation.StockIntradayHistory) { a.Lines[120].Meta.BasisID = "different" },
		func(a *foundation.StockIntradayHistory) { a.Lines[120].Meta.EffectiveAdjustment = "hfq" },
		func(a *foundation.StockIntradayHistory) { a.Lines[120].PreviousClose = 10 },
		func(a *foundation.StockIntradayHistory) { a.Lines[120].AveragePrice = -1 },
		func(a *foundation.StockIntradayHistory) { a.Lines[120].Close = 0 },
		func(a *foundation.StockIntradayHistory) { a.Lines[120].Volume = -1 },
		func(a *foundation.StockIntradayHistory) { a.Lines = append(a.Lines[:60], a.Lines[61:]...) },
	}
	for i, mutation := range mutations {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			archive := intradayTestArchive("2026-09-23")
			mutation(&archive)
			if _, err := stockIntradayFromHistory(archive, "000001.SZ", "2026-09-23"); !errors.Is(err, foundation.ErrInvalidPriceData) {
				t.Fatalf("invalid archive accepted or not classified: %v", err)
			}
		})
	}
	archive := intradayTestArchive("2026-09-23")
	extra := archive.Lines[120]
	extra.Time = extra.Time.Add(90 * time.Minute)
	archive.Lines = append(archive.Lines, extra)
	if data, err := stockIntradayFromHistory(archive, "000001.SZ", "2026-09-23"); err != nil || len(data.Lines) != 242 {
		t.Fatalf("real 13:00 rejected: %v", err)
	}
}

func TestIntradayHistorySkipsUnfinishedToday(t *testing.T) {
	now := time.Date(2026, 9, 30, 14, 59, 0, 0, stockIntradayZone)
	if stockIntradayHistoryReady("2026-09-30", now) || stockIntradayHistoryReady("2026-10-01", now) || !stockIntradayHistoryReady("2026-09-23", now) || !stockIntradayHistoryReady("2026-09-30", now.Add(time.Minute)) {
		t.Fatal("today future archive slots treated as trades")
	}
}

func TestIntradayHistoryFailureObservationSurvivesSuccessfulSampleFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("private upstream data"))
	}))
	defer upstream.Close()
	history := sina.NewHistoryIntradayClient(sina.NewClient(sina.WithIntradayHistoryBaseURL(upstream.URL)))
	s := NewServer(Config{HistoryIntraday: history, Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		return intradayTestLines("2026-09-23"), nil
	})})
	defer s.Close()
	data, code := requestIntraday(t, s, "2026-09-23")
	if code != 200 || len(data.Lines) == 0 || data.Meta.FallbackReason == "" {
		t.Fatalf("sample fallback failed: %d %+v", code, data)
	}
	s.sourceHealth.mu.RLock()
	historical := s.sourceHealth.capabilities["sina"]["stock-intraday:history"]
	recent := s.sourceHealth.capabilities["sina"]["stock-intraday:recent-sample"]
	s.sourceHealth.mu.RUnlock()
	if !historical.failed || historical.lastFailure.IsZero() || recent.failed || recent.lastSuccess.IsZero() {
		t.Fatalf("capabilities falsely merged: history=%+v recent=%+v", historical, recent)
	}
}

func TestIntradayInjectedSampleDoesNotImplicitlyEnablePublicHistory(t *testing.T) {
	s := NewServer(Config{Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) { return nil, nil })})
	defer s.Close()
	if s.historyIntradayProvider != nil {
		t.Fatal("injected sample test implicitly enabled public archive")
	}
}

func TestIntradayHistoryCanceledBeforeFallback(t *testing.T) {
	started := make(chan struct{})
	var samples atomic.Int32
	s := NewServer(Config{HistoryIntraday: historyIntradayProviderFunc(func(ctx context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
		close(started)
		<-ctx.Done()
		return foundation.StockIntradayHistory{}, ctx.Err()
	}), Intraday: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		samples.Add(1)
		return nil, nil
	})})
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/quotes/intraday?symbol=000001.SZ&date=2026-09-23", nil).WithContext(ctx))
		close(done)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled history request hung")
	}
	if samples.Load() != 0 {
		t.Fatal("cancel launched another upstream")
	}
	if entry := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "sina"); entry.CheckedAt != nil {
		t.Fatal("caller cancellation renewed or failed provider health")
	}
}
