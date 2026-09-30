package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/sina"
)

func TestMonthlyKLineFallsBackToSinaWhenPrimaryFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("scale") != "7200" || r.URL.Query().Get("datalen") != "60" {
			t.Errorf("monthly fallback used wrong request: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`callback([{"day":"2026-09-30","open":"3.100","high":"4.400","low":"2.980","close":"4.260","volume":"8133390099"}]);`))
	}))
	defer upstream.Close()
	server := NewServer(Config{
		KLinePrimary:  observedKLineProvider{err: errors.New("primary unavailable")},
		KLineFallback: sina.NewClient(sina.WithKLineBaseURL(upstream.URL)),
	})
	defer server.Close()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&period=month&limit=60", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("monthly fallback status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []foundation.KLine `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Close != 4.26 || payload.Data[0].Meta.Source != "sina" {
		t.Fatalf("unusable monthly fallback: %+v", payload.Data)
	}
}

func TestLiveAPIMonthlyKLineWithSinaFallback(t *testing.T) {
	requireLive(t)
	// Force the primary failure so a successful EastMoney request cannot hide
	// a regression in the monthly fallback that the stock detail page needs.
	server := NewServer(Config{KLinePrimary: observedKLineProvider{err: errors.New("primary unavailable")}})
	defer server.Close()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&period=month&limit=5", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("live monthly fallback status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []foundation.KLine `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 5 {
		t.Fatalf("live monthly fallback returned %d bars, want 5", len(payload.Data))
	}
	for i, bar := range payload.Data {
		if bar.Symbol != "000002.SZ" || bar.Close <= 0 || bar.Meta.Source != "sina" || bar.Meta.SourceURL == "" {
			t.Fatalf("unusable live monthly bar: %+v", bar)
		}
		if i > 0 && (bar.Time.Format("2006-01") == payload.Data[i-1].Time.Format("2006-01") || !bar.Time.After(payload.Data[i-1].Time)) {
			t.Fatalf("bars must be chronological and belong to distinct calendar months: %+v", payload.Data)
		}
	}
}
