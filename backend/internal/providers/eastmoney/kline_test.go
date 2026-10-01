package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestRequiredKLineValuesAndOptionalFieldPresence(t *testing.T) {
	meta := foundation.SourceMeta{Source: "eastmoney"}
	if _, err := parseKLine("2026-09-30,bad,bad,bad,bad,bad,bad", "000002.SZ", meta); err == nil {
		t.Fatal("invalid required numbers accepted")
	}
	bar, err := parseKLine("2026-09-30,-2,-1,1,-3,100,1000,0,--,0,-", "000002.SZ", meta)
	if err != nil || bar.Open != -2 || !bar.Meta.FieldsKnown || foundation.FieldAvailable(bar.Meta, "change_percent") || foundation.FieldAvailable(bar.Meta, "turnover_rate") {
		t.Fatalf("adjusted price/mask: %+v %v", bar, err)
	}
}

func TestEastMoneyRetryHonorsCancelledBudget(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer upstream.Close()
	client := NewClient(WithBaseURL(upstream.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := client.KLine(ctx, "000002.SZ", "day", 1)
	if err == nil {
		t.Fatal("timeout should fail")
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("retry swallowed fallback budget: %v", time.Since(start))
	}
}

func TestClientKLineParsesEastMoneyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/qt/stock/kline/get" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("secid"); got != "0.000001" {
			t.Fatalf("secid = %q, want 0.000001", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"rc": 0,
			"data": {
				"klines": [
					"2026-06-12,10.00,10.50,10.80,9.90,123456,123456789.00,8.50,5.00,0.50,1.20"
				],
				"name": "平安银行",
				"code": "000001"
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	got, err := client.KLine(context.Background(), "000001.SZ", "day", 1)
	if err != nil {
		t.Fatalf("KLine returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(KLine) = %d, want 1", len(got))
	}
	if got[0].Symbol != "000001.SZ" || got[0].Close != 10.50 || got[0].Volume != 123456 {
		t.Fatalf("unexpected kline: %+v", got[0])
	}
	wantDate := time.Date(2026, 6, 12, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	if !got[0].Time.Equal(wantDate) {
		t.Fatalf("Time = %v, want %v", got[0].Time, wantDate)
	}
	if got[0].Meta.Source != "eastmoney" || got[0].Meta.SourceURL == "" {
		t.Fatalf("unexpected meta: %+v", got[0].Meta)
	}
}

func TestExplicitKLineAdjustmentUsesSourceParameter(t *testing.T) {
	for adjustment, expected := range map[string]string{"none": "0", "qfq": "1", "hfq": "2"} {
		t.Run(adjustment, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("fqt") != expected {
					t.Errorf("fqt=%s", r.URL.Query().Get("fqt"))
				}
				_, _ = w.Write([]byte(`{"rc":0,"data":{"klines":["2026-09-30,10,11,12,9,100,1000"]}}`))
			}))
			defer upstream.Close()
			client := NewClient(WithBaseURL(upstream.URL))
			if _, err := client.KLineAdjusted(context.Background(), "000001.SZ", "day", 1, adjustment); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestClientKLineRetriesTransientHTTPFailure(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			http.Error(w, "temporary upstream error", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"rc": 0,
			"data": {"klines": ["2026-06-12,10.00,10.50,10.80,9.90,123456,123456789.00,8.50,5.00,0.50,1.20"]}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	got, err := client.KLine(context.Background(), "000001.SZ", "day", 1)
	if err != nil {
		t.Fatalf("KLine returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(KLine) = %d, want 1", len(got))
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestEastMoneyKLineUsesShanghaiTimeIndependentOfHost(t *testing.T) {
	value, err := parseKLineTime("2026-09-30 09:31")
	if err != nil || value.Format(time.RFC3339) != "2026-09-30T09:31:00+08:00" {
		t.Fatalf("market timestamp changed with server timezone: %v %v", value, err)
	}
}

func TestParseKLineSupportsIntradayTime(t *testing.T) {
	got, err := parseKLine("2026-08-12 14:35,10.00,10.50,10.80,9.90,123,456,8.5,5,0.5,1.2", "000001.SZ", foundation.SourceMeta{})
	if err != nil {
		t.Fatalf("parseKLine returned error: %v", err)
	}
	if got.Time.Hour() != 14 || got.Time.Minute() != 35 {
		t.Fatalf("Time = %v, want 14:35", got.Time)
	}
}
