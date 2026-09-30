package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type fakeAuctionProvider struct {
	trace foundation.AuctionTrace
	err   error
}

func (f fakeAuctionProvider) AuctionTrace(_ context.Context, symbol string) (foundation.AuctionTrace, error) {
	if f.err != nil {
		return foundation.AuctionTrace{}, f.err
	}
	trace := f.trace
	trace.Symbol = symbol
	return trace, nil
}

func TestAuctionTraceAuthAndRequestValidation(t *testing.T) {
	s := NewServer(Config{Token: "private", Auction: fakeAuctionProvider{err: errors.New("should never fetch")}})
	defer s.Close()
	for _, test := range []struct {
		path   string
		token  string
		status int
	}{
		{"/api/v1/quotes/auction?symbol=600519.SH", "", http.StatusUnauthorized},
		{"/api/v1/quotes/auction?symbol=oops", "private", http.StatusBadRequest},
		{"/api/v1/quotes/auction?symbol=600519.SH", "private", http.StatusBadGateway},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != test.status {
			t.Fatalf("%s status=%d want=%d", test.path, rec.Code, test.status)
		}
	}
}

func TestInjectedAuctionFailureDoesNotBlameEastMoney(t *testing.T) {
	s := NewServer(Config{Auction: fakeAuctionProvider{err: errors.New("mock upstream failed")}})
	defer s.Close()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/auction?symbol=600519.SH", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d", rec.Code)
	}
	if source := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "eastmoney"); source.Status != "unknown" {
		t.Fatalf("mock failure misattributed to EastMoney: %+v", source)
	}
}

func TestAuctionDetailHistoricalIsNotSourceFailure(t *testing.T) {
	china := time.FixedZone("CST", 8*60*60)
	yesterday := time.Now().In(china).AddDate(0, 0, -1)
	s := NewServer(Config{Auction: fakeAuctionProvider{trace: foundation.AuctionTrace{
		TradeDate: yesterday.Format("2006-01-02"),
		Points:    []foundation.AuctionPoint{{Time: yesterday, Price: 10}},
		Meta:      foundation.SourceMeta{Source: "eastmoney:pre-open", FetchedAt: time.Now()},
	}}})
	defer s.Close()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/auction?symbol=600519.SH&detail=1", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("no-current response=%d %s", rec.Code, rec.Body.String())
	}
	if source := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "eastmoney"); source.Status == "degraded" {
		t.Fatalf("historical response misattributed as source outage: %+v", source)
	}
}

func TestAuctionTraceDoesNotRelabelHistoricalDayAsToday(t *testing.T) {
	china := time.FixedZone("CST", 8*60*60)
	yesterday := time.Now().In(china).AddDate(0, 0, -1)
	s := NewServer(Config{Auction: fakeAuctionProvider{trace: foundation.AuctionTrace{
		TradeDate: yesterday.Format("2006-01-02"),
		Points:    []foundation.AuctionPoint{{Time: yesterday, Price: 10}},
		Meta:      foundation.SourceMeta{Source: "eastmoney:pre-open", FetchedAt: time.Now()},
	}}})
	defer s.Close()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/auction?symbol=600519.SH", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"historical"`) {
		t.Fatalf("old data looked current: %d %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Data foundation.AuctionTrace `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || len(result.Data.Points) != 0 {
		t.Fatalf("historical price leaked into current trace: %+v err=%v", result, err)
	}
}

func TestAuctionTraceReturnsCurrentIndicativePoints(t *testing.T) {
	china := time.FixedZone("CST", 8*60*60)
	today := time.Now().In(china)
	s := NewServer(Config{Auction: fakeAuctionProvider{trace: foundation.AuctionTrace{
		TradeDate: today.Format("2006-01-02"),
		Points:    []foundation.AuctionPoint{{Time: today, Price: 10}},
		Meta:      foundation.SourceMeta{Source: "eastmoney:pre-open", FetchedAt: time.Now()},
	}}})
	defer s.Close()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/auction?symbol=600519.SH", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ready"`) {
		t.Fatalf("current trace unavailable: %d %s", rec.Code, rec.Body.String())
	}
}
