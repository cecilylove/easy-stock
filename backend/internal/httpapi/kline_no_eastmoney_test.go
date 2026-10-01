package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/tencent"
)

type priceNoEMTransport struct {
	mu    sync.Mutex
	hosts []string
	fail  bool
	empty bool
}

func (t *priceNoEMTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.hosts = append(t.hosts, r.URL.Hostname())
	t.mu.Unlock()
	if t.fail {
		return nil, errors.New("offline fixture")
	}
	if r.URL.Hostname() != "web.ifzq.gtimg.cn" {
		return nil, errors.New("Sina unavailable fixture")
	}
	param := strings.Split(r.URL.Query().Get("param"), ",")
	if len(param) < 6 {
		return nil, fmt.Errorf("invalid Tencent param")
	}
	key := param[1]
	if param[5] != "" {
		key = param[5] + key
	}
	row := `[["2026-09-30","10","11","12","9","123"]]`
	if t.empty {
		row = "[]"
	}
	body := fmt.Sprintf(`{"code":0,"data":{"%s":{"%s":%s}}}`, param[0], key, row)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
func (t *priceNoEMTransport) assertNoEastMoney(tst *testing.T) {
	tst.Helper()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, host := range t.hosts {
		if strings.Contains(host, "eastmoney") {
			tst.Fatalf("retired price source requested: %s", host)
		}
	}
}

func TestDefaultAndStrictPriceRequestsNeverCallEastMoney(t *testing.T) {
	for _, mode := range []string{"healthy", "empty", "failed"} {
		t.Run(mode, func(t *testing.T) {
			transport := &priceNoEMTransport{empty: mode == "empty", fail: mode == "failed"}
			original := http.DefaultTransport
			http.DefaultTransport = transport
			defer func() { http.DefaultTransport = original }()
			server := NewServer(nil)
			defer server.Close()
			for _, query := range []string{"period=day", "period=week", "period=month", "period=year", "period=1&detail=1", "period=5", "period=120", "period=day&adjust=none", "period=week&adjust=qfq", "period=month&adjust=hfq", "period=year&adjust=hfq", "provider=eastmoney&adjust=qfq"} {
				response := httptest.NewRecorder()
				server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&"+query, nil))
				if strings.Contains(query, "provider=eastmoney") && response.Code != 400 {
					t.Fatalf("retired provider status %d", response.Code)
				}
				transport.assertNoEastMoney(t)
			}
			for _, symbol := range []string{"000001.SH", "399001.SZ", "399006.SZ", "000688.SH", "000300.SH", "899050.BJ"} {
				lines, err := server.loadKLine(context.Background(), symbol, "day", 300)
				if mode == "healthy" && symbol != "899050.BJ" {
					if err != nil || len(lines) != 1 || lines[0].Meta.InstrumentID == symbol || lines[0].Meta.VolumeUnit != "provider_index_volume" || lines[0].Volume != 123 {
						t.Fatalf("research benchmark %s %+v %v", symbol, lines, err)
					}
				}
				transport.assertNoEastMoney(t)
			}
		})
	}
}

func TestIndexAndStockCooldownsAreIndependent(t *testing.T) {
	server := fallbackTestServer(klineProviderFunc(func(_ context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
		if symbol == "000002.SZ" {
			return nil, errors.New("stock unavailable")
		}
		bars := fallbackTestBars()
		bars[0].Symbol = symbol
		return bars, nil
	}), nil)
	server.kLineRoutes = newKLineRouteState()
	server.kLinePrimarySourceID = "sina"
	for i := 0; i < 2; i++ {
		_, _ = server.loadKLine(context.Background(), "000002.SZ", "day", 1)
	}
	bars, err := server.loadKLine(context.Background(), "399001.SZ", "day", 1)
	if err != nil || len(bars) != 1 || bars[0].Meta.InstrumentID != "szse" || bars[0].Meta.VolumeUnit != "provider_index_volume" || bars[0].Meta.Capability != "index-kline:day:source" {
		t.Fatalf("stock failure poisoned index/units: %+v %v", bars, err)
	}
}

func TestStrictRetiredSourceFailsBeforeAnyRequest(t *testing.T) {
	transport := &priceNoEMTransport{}
	original := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = original }()
	server := NewServer(nil)
	defer server.Close()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&provider=eastmoney&adjust=hfq", nil))
	if response.Code != 400 || !strings.Contains(response.Body.String(), "退役") || len(transport.hosts) != 0 {
		t.Fatalf("retired source silently switched: %d %s %+v", response.Code, response.Body.String(), transport.hosts)
	}
	if sourceByID(t, server.sourceHealth.snapshot(time.Now()), "eastmoney").CheckedAt != nil {
		t.Fatal("retired source blamed")
	}
}

func TestBenchmarkContractKeepsCanonicalIdentityAndProviderVolume(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"sh000300":{"day":[["2026-09-30","3000","3100","3200","2900","123"]]}}}`)
	}))
	defer upstream.Close()
	server := NewServer(Config{KLinePrimary: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		return nil, errors.New("unavailable")
	}), KLineFallback: tencent.NewPriceKLineClient(tencent.NewClient(tencent.WithKLineBaseURL(upstream.URL), tencent.WithHTTPClient(upstream.Client())))})
	defer server.Close()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000300.SH", nil))
	var payload struct {
		Data []foundation.KLine `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || response.Code != 200 || len(payload.Data) != 1 {
		t.Fatalf("benchmark HTTP %d %s", response.Code, response.Body.String())
	}
	bar := payload.Data[0]
	if bar.Meta.InstrumentID != "csi300" || bar.Meta.VolumeUnit != "provider_index_volume" || bar.Meta.TimeZone != "Asia/Shanghai" || bar.Time.Format("2006-01-02") != "2026-09-30" || bar.Meta.BasisID != "tencent:index:csi300:none" || bar.Volume != 123 {
		t.Fatalf("index incorrectly coerced to stock %+v", bar)
	}
}
