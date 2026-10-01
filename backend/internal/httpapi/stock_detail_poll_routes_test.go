package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type countedDetailQuotes struct{ calls atomic.Int32 }

func (p *countedDetailQuotes) Realtime(_ context.Context, symbols []string) ([]foundation.Quote, error) {
	p.calls.Add(1)
	return []foundation.Quote{{Symbol: symbols[0], Price: 10.1, Meta: foundation.SourceMeta{Source: "test", FetchedAt: time.Now()}}}, nil
}

type countedDetailLines struct{ calls atomic.Int32 }

func (p *countedDetailLines) KLine(_ context.Context, symbol, _ string, _ int) ([]foundation.KLine, error) {
	p.calls.Add(1)
	return []foundation.KLine{{Symbol: symbol, Time: time.Now(), Open: 10, High: 11, Low: 9, Close: 10.1, Meta: foundation.SourceMeta{Source: "test", FetchedAt: time.Now()}}}, nil
}

func TestStockDetailPollRoutesDeduplicateWithoutChangingSharedEndpoints(t *testing.T) {
	quotes := &countedDetailQuotes{}
	lines := &countedDetailLines{}
	s := NewServer(Config{Realtime: quotes, KLinePrimary: lines, KLineFallback: lines})
	defer s.Close()
	for range 3 {
		for _, path := range []string{
			"/api/v1/quotes/realtime?symbols=000001.SZ&detail=1",
			"/api/v1/quotes/kline?symbol=000001.SZ&period=1&limit=240&detail=1",
		} {
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s => %d %s", path, rec.Code, rec.Body.String())
			}
			var result map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
		}
	}
	if quotes.calls.Load() != 1 || lines.calls.Load() != 1 {
		t.Fatalf("detail reads hit upstream quote=%d kline=%d", quotes.calls.Load(), lines.calls.Load())
	}
	for _, path := range []string{
		"/api/v1/quotes/realtime?symbols=000001.SZ",
		"/api/v1/quotes/kline?symbol=000001.SZ&period=1&limit=240",
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("shared path=%s => %d", path, rec.Code)
		}
	}
	if quotes.calls.Load() != 2 || lines.calls.Load() != 2 {
		t.Fatalf("shared endpoints unexpectedly cached quote=%d kline=%d", quotes.calls.Load(), lines.calls.Load())
	}
}
