package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
)

func TestPriceMissingSymbolsDoNotTripMarketBreaker(t *testing.T) {
	for _, mode := range []string{"missing", "empty", "null", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			goodSina, goodTencent := 0, 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("symbol") != "" {
					if r.URL.Query().Get("symbol") == "sz009999" {
						if mode == "invalid" {
							fmt.Fprint(w, "<html>no bar</html>")
						} else {
							fmt.Fprint(w, "callback(null);")
						}
						return
					}
					goodSina++
					fmt.Fprint(w, `callback([{"day":"2026-09-30","open":"3.8","close":"4.26","high":"4.4","low":"3.67","volume":"100"}]);`)
					return
				}
				parts := strings.Split(r.URL.Query().Get("param"), ",")
				key := parts[1]
				if parts[5] != "" {
					key = parts[5] + key
				}
				if parts[0] == "sz009999" {
					switch mode {
					case "empty":
						fmt.Fprintf(w, `{"code":0,"data":{"sz009999":{"%s":[]}}}`, key)
					case "null":
						fmt.Fprintf(w, `{"code":0,"data":{"sz009999":{"%s":null}}}`, key)
					case "invalid":
						fmt.Fprintf(w, `{"code":0,"data":{"sz009999":{"%s":[["bad"]]}}}`, key)
					default:
						fmt.Fprint(w, `{"code":0,"data":null}`)
					}
					return
				}
				goodTencent++
				fmt.Fprintf(w, `{"code":0,"data":{"sz000002":{"%s":[["2026-09-30","3.8","4.26","4.4","3.67","100"]]}}}`, key)
			}))
			defer upstream.Close()
			server := NewServer(Config{KLinePrimary: sina.NewClient(sina.WithKLineBaseURL(upstream.URL), sina.WithHTTPClient(upstream.Client())), KLineFallback: tencent.NewPriceKLineClient(tencent.NewClient(tencent.WithKLineBaseURL(upstream.URL), tencent.WithHTTPClient(upstream.Client()))), KLineStrictTencent: tencent.NewStockKLineClient(tencent.NewClient(tencent.WithKLineBaseURL(upstream.URL), tencent.WithHTTPClient(upstream.Client())))})
			defer server.Close()
			server.kLinePrimarySourceID = "sina"
			server.kLineFallbackSourceID = "tencent"
			for i := 0; i < 2; i++ {
				if _, err := server.loadKLine(context.Background(), "009999.SZ", "day", 1); err == nil {
					t.Fatal("missing symbol accepted")
				}
				if _, err := server.loadAdjustedKLine(context.Background(), "009999.SZ", "day", 1, "qfq"); err == nil {
					t.Fatal("missing strict symbol accepted")
				}
			}
			for _, id := range []string{"sina", "tencent"} {
				entry := sourceByID(t, server.sourceHealth.snapshot(time.Now()), id)
				if entry.Status != "unknown" || entry.CheckedAt != nil {
					t.Fatalf("symbol failure claimed whole source outage %+v", entry)
				}
			}
			if !server.kLineRoutes.ready("sina:SZ:stock:day:source") || !server.kLineRoutes.ready("tencent:SZ:stock:day:source") || !server.kLineRoutes.ready("tencent:SZ:day:qfq") {
				t.Fatal("symbol no-data/invalid tripped market breaker")
			}
			if _, err := server.loadKLine(context.Background(), "000002.SZ", "day", 1); err != nil {
				t.Fatalf("good default blocked after bad symbol %v", err)
			}
			if _, err := server.loadAdjustedKLine(context.Background(), "000002.SZ", "day", 1, "qfq"); err != nil {
				t.Fatalf("good strict blocked after bad symbol %v", err)
			}
			if goodSina != 1 || goodTencent != 1 {
				t.Fatalf("good sources skipped Sina=%d Tencent=%d", goodSina, goodTencent)
			}
			for _, id := range []string{"sina", "tencent"} {
				server.sourceHealth.mu.RLock()
				capabilities := server.sourceHealth.capabilities[id]
				_, symbolRecorded := capabilities["stock-kline:day:source:symbol:009999.SZ"]
				server.sourceHealth.mu.RUnlock()
				if !symbolRecorded {
					t.Fatalf("%s lost symbol scoped diagnostic", id)
				}
			}
		})
	}
}

func TestPriceFailureClassificationDoesNotParseEnglishErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		market bool
	}{{foundation.ErrPriceNoData, false}, {foundation.ErrInvalidPriceData, false}, {&foundation.PriceHTTPStatusError{Provider: "tencent", StatusCode: 404}, false}, {&foundation.PriceHTTPStatusError{Provider: "tencent", StatusCode: 429}, true}, {&foundation.PriceHTTPStatusError{Provider: "sina", StatusCode: 503}, true}, {context.Canceled, false}, {context.DeadlineExceeded, true}, {io.EOF, true}, {errors.New("provider unavailable"), true}} {
		if got := marketPriceFailure(test.err); got != test.market {
			t.Fatalf("classification %v=%v want%v", test.err, got, test.market)
		}
	}
}

type adjustedPriceFunc func(context.Context, string, string, int, string) ([]foundation.KLine, error)

func (f adjustedPriceFunc) KLineAdjusted(ctx context.Context, symbol, period string, limit int, adjust string) ([]foundation.KLine, error) {
	return f(ctx, symbol, period, limit, adjust)
}

func TestStrictNetworkFailuresStillCooldownAndSuccessResets(t *testing.T) {
	calls := 0
	networkFailed := true
	server := NewServer(Config{KLineStrictTencent: adjustedPriceFunc(func(context.Context, string, string, int, string) ([]foundation.KLine, error) {
		calls++
		if networkFailed {
			return nil, io.EOF
		}
		bars := fallbackTestBars()
		bars[0].Meta.Source = "tencent:stock-kline"
		return bars, nil
	})})
	defer server.Close()
	clock := time.Now()
	server.kLineRoutes.now = func() time.Time { return clock }
	for i := 0; i < 2; i++ {
		_, _ = server.loadAdjustedKLine(context.Background(), "009999.SZ", "day", 1, "qfq")
	}
	networkFailed = false
	if _, err := server.loadAdjustedKLine(context.Background(), "000002.SZ", "day", 1, "qfq"); err == nil || calls != 2 {
		t.Fatal("network breaker not preserved")
	}
	clock = clock.Add(31 * time.Second)
	if _, err := server.loadAdjustedKLine(context.Background(), "000002.SZ", "day", 1, "qfq"); err != nil {
		t.Fatal(err)
	}
	networkFailed = true
	_, _ = server.loadAdjustedKLine(context.Background(), "000002.SZ", "day", 1, "qfq")
	if !server.kLineRoutes.ready("tencent:SZ:day:qfq") {
		t.Fatal("success did not reset network failures")
	}
}

func TestStrictCallerCancellationNeverPoisonsCapability(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := NewServer(Config{KLineStrictTencent: adjustedPriceFunc(func(context.Context, string, string, int, string) ([]foundation.KLine, error) {
		cancel()
		return nil, context.Canceled
	})})
	defer server.Close()
	if _, err := server.loadAdjustedKLine(ctx, "000002.SZ", "day", 1, "qfq"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost %v", err)
	}
	if !server.kLineRoutes.ready("tencent:SZ:day:qfq") || sourceByID(t, server.sourceHealth.snapshot(time.Now()), "tencent").CheckedAt != nil {
		t.Fatal("caller cancel marked source failure")
	}
}

func TestKLineProviderSelectionKeepsStrictSourcesSeparate(t *testing.T) {
	tx := &adjustedTestProvider{source: "tencent:stock-kline"}
	implicitCalls := 0
	server := NewServer(Config{KLinePrimary: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		implicitCalls++
		return nil, errors.New("must not call default")
	}), KLineStrictTencent: tx})
	defer server.Close()
	for _, test := range []struct {
		query string
		want  string
	}{
		{"adjust=qfq", "qfq"}, {"provider=tencent&adjust=hfq", "hfq"}, {"provider=tencent&adjust=source", "none"},
	} {
		tx.adjustment = ""
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&period=day&"+test.query, nil))
		if response.Code != 200 {
			t.Fatalf("%s status=%d %s", test.query, response.Code, response.Body.String())
		}
		actual := tx.adjustment
		if actual != test.want {
			t.Fatalf("%s adjustment %s", test.query, actual)
		}
	}
	if implicitCalls != 0 {
		t.Fatal("strict selected provider used source-default route")
	}
}

func TestStrictProviderDoesNotAdoptArbitraryAdjustedPrimary(t *testing.T) {
	foreign := &adjustedTestProvider{source: "eastmoney"}
	server := NewServer(Config{KLinePrimary: foreign})
	defer server.Close()
	if server.kLineStrictTencent == foreign {
		t.Fatal("arbitrary primary adopted as strict Tencent")
	}
	if _, ok := server.kLineStrictTencent.(*tencent.StockKLineClient); !ok {
		t.Fatalf("wrong strict default %T", server.kLineStrictTencent)
	}
	if server.strictKLineProvider("eastmoney") != nil {
		t.Fatal("retired EastMoney strict still available")
	}
}

func TestStrictKLineRejectsInjectedWrongSupplierIdentity(t *testing.T) {
	for _, test := range []struct{ source, returned string }{{"tencent", "eastmoney"}, {"tencent", "fixture"}} {
		provider := &adjustedTestProvider{source: test.returned}
		server := NewServer(Config{KLineStrictTencent: provider})
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&provider="+test.source+"&adjust=qfq", nil))
		server.Close()
		if response.Code != 502 {
			t.Fatalf("supplier mismatch silently accepted %s/%s: %d %s", test.source, test.returned, response.Code, response.Body.String())
		}
	}
}

func TestKLineTransactionPriceValidationPreservesAdjustedNegativePrices(t *testing.T) {
	for _, adjust := range []string{"none", "qfq", "hfq"} {
		bars := fallbackTestBars()
		bars[0].Meta.Source = "eastmoney"
		bars[0].Open, bars[0].High, bars[0].Low, bars[0].Close = -2, 1, -3, 0
		_, err := normalizeKLineContract(bars, "000002.SZ", "day", adjust, adjust)
		if (adjust == "none") != (err != nil) {
			t.Fatalf("wrong price rule for %s: %v", adjust, err)
		}
	}
	bars := fallbackTestBars()
	bars[0].Meta.Source = "tencent:stock-kline"
	bars[0].Meta.EffectiveAdjustment = "none"
	bars[0].Low = 0
	if _, err := normalizeKLineContract(bars, "000002.SZ", "day", "source", ""); err == nil {
		t.Fatal("default unadjusted effective none zero accepted")
	}
}

func TestKLineProviderValidationDoesNotObserveUnsupported(t *testing.T) {
	transport := &captureOfflineTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = previous }()
	server := NewServer(nil)
	defer server.Close()
	for _, query := range []string{"provider=tencent&symbol=920001.BJ&period=day&adjust=qfq", "provider=tencent&symbol=000002.SZ&period=5", "provider=unknown&symbol=000002.SZ", "symbol=000002.SZ&asof=2026-01-01", "symbol=000002.SZ&period=5&adjust=hfq"} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?"+query, nil))
		if response.Code != 400 {
			t.Errorf("%s returned %d", query, response.Code)
		}
	}
	if len(transport.hosts) != 0 {
		t.Fatal("unsupported strict request attempted upstream")
	}
	for _, id := range []string{"tencent", "eastmoney", "sina"} {
		if sourceByID(t, server.sourceHealth.snapshot(time.Now()), id).CheckedAt != nil {
			t.Fatalf("unattempted source %s blamed", id)
		}
	}
}

func TestDefaultKLineFallbackAndCapabilityCooldown(t *testing.T) {
	primaryCalls, txCalls := 0, 0
	server := fallbackTestServer(klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		primaryCalls++
		return nil, errors.New("EOF")
	}), nil)
	server.kLineRoutes = newKLineRouteState()
	server.kLineFallbackSourceID = "tencent"
	server.kLineFallback = klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		txCalls++
		lines := fallbackTestBars()
		lines[0].Meta = foundation.SourceMeta{Source: "tencent:stock-kline", FetchedAt: time.Now(), EffectiveAdjustment: "none", VolumeUnit: "shares"}
		return lines, nil
	})
	for index := 0; index < 3; index++ {
		lines, err := server.loadKLine(context.Background(), "000002.SZ", "day", 1)
		if err != nil || len(lines) != 1 || lines[0].Meta.EffectiveAdjustment != "none" || lines[0].Meta.FallbackReason == "" {
			t.Fatalf("tertiary %+v %v", lines, err)
		}
	}
	if primaryCalls != 2 || txCalls != 3 {
		t.Fatalf("cooldown primary=%d Tencent=%d", primaryCalls, txCalls)
	}
	// Cooling down day source requests cannot poison other periods or strict modes.
	if !server.kLineRoutes.ready("eastmoney:SZ:stock:week:source") || !server.kLineRoutes.ready("eastmoney:SZ:day:qfq") || !server.kLineRoutes.ready("eastmoney:SZ:index:day:source") {
		t.Fatal("capability cooldown is too broad")
	}
}

func TestKLineCooldownClockRecoversWithoutSleeping(t *testing.T) {
	clock := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	state := newKLineRouteState()
	state.now = func() time.Time { return clock }
	key := "sina:SZ:day:source"
	state.record(key, true)
	if !state.ready(key) {
		t.Fatal("first failure should not suppress retry")
	}
	state.record(key, true)
	if state.ready(key) {
		t.Fatal("second failure did not start cooldown")
	}
	clock = clock.Add(29 * time.Second)
	if state.ready(key) {
		t.Fatal("cooldown ended too soon")
	}
	clock = clock.Add(time.Second)
	if !state.ready(key) {
		t.Fatal("cooldown did not recover")
	}
	state.record(key, false)
	state.record(key, true)
	if !state.ready(key) {
		t.Fatal("success did not reset consecutive failure budget")
	}
}

func TestDefaultKLineSkipsUnsupportedTertiaryWithoutFailure(t *testing.T) {
	server := fallbackTestServer(klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) { return nil, errors.New("EOF") }), klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) { return nil, errors.New("EOF") }))
	server.kLineFallback = tencent.NewPriceKLineClient(nil)
	server.kLineFallbackSourceID = "tencent"
	if _, err := server.loadKLine(context.Background(), "000002.SZ", "5", 1); err == nil {
		t.Fatal("all unavailable accepted")
	}
	if sourceByID(t, server.sourceHealth.snapshot(time.Now()), "tencent").CheckedAt != nil {
		t.Fatal("unsupported Tencent minute capability blamed")
	}
}

func TestKLineContractNormalizesUnitsAndMissingFields(t *testing.T) {
	bars := fallbackTestBars()
	bars[0].Volume = 123
	bars[0].Meta.Source = "eastmoney"
	normalized, err := normalizeKLineContract(bars, "000002.SZ", "day", "qfq", "qfq")
	if err != nil || normalized[0].Volume != 12300 || normalized[0].Meta.VolumeUnit != "shares" || normalized[0].Meta.BasisID == "" {
		t.Fatalf("normalization %+v %v", normalized, err)
	}
	repeated, err := normalizeKLineContract(normalized, "000002.SZ", "day", "qfq", "qfq")
	if err != nil || repeated[0].Volume != 12300 {
		t.Fatal("double converted already normalized shares")
	}
	bars[0].Meta.Source = "sina"
	normalized, err = normalizeKLineContract(bars, "000002.SZ", "day", "source", "")
	if err != nil || normalized[0].Volume != 123 || normalized[0].Meta.EffectiveAdjustment != "source" || foundation.FieldAvailable(normalized[0].Meta, "amount") {
		t.Fatalf("Sina invented raw/amount %+v %v", normalized, err)
	}
	bars[0].High = 3
	if _, err := normalizeKLineContract(bars, "000002.SZ", "day", "source", ""); err == nil {
		t.Fatal("invalid OHLC accepted")
	}
	bars = fallbackTestBars()
	bars[0].Meta.Source = "sina"
	other := bars[0]
	other.Time = other.Time.Add(time.Minute)
	other.Meta.Source = "eastmoney"
	if _, err := normalizeKLineContract(append(bars, other), "000002.SZ", "day", "source", ""); err == nil {
		t.Fatal("mixed sources accepted")
	}
}

func TestTencentStrictAPIReportsActualBasisAndNoHistoricalAmount(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("param") != "sz000002,day,,,120," {
			t.Errorf("unexpected Tencent param %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"sz000002":{"day":[["2026-09-30","3.8","4.26","4.4","3.67","123"]]}}}`))
	}))
	defer upstream.Close()
	provider := tencent.NewStockKLineClient(tencent.NewClient(tencent.WithKLineBaseURL(upstream.URL), tencent.WithHTTPClient(upstream.Client())))
	server := NewServer(Config{KLineStrictTencent: provider})
	defer server.Close()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&provider=tencent&adjust=source", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"effective_adjustment":"none"`) || !strings.Contains(response.Body.String(), `"requested_adjustment":"source"`) || !strings.Contains(response.Body.String(), `"volume":12300`) || !strings.Contains(response.Body.String(), `"volume_unit":"shares"`) {
		t.Fatalf("Tencent HTTP contract %d %s", response.Code, response.Body.String())
	}
}

func TestKLineStrictFailureNeverUsesOtherStrictSupplier(t *testing.T) {
	server := NewServer(Config{KLineStrictTencent: &adjustedTestProvider{err: errors.New("EOF"), source: "tencent:stock-kline"}})
	defer server.Close()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&adjust=qfq", nil))
	if response.Code != 502 || !strings.Contains(response.Body.String(), "不切换") {
		t.Fatalf("strict fallback violated %d %s", response.Code, response.Body.String())
	}
	if sourceByID(t, server.sourceHealth.snapshot(time.Now()), "eastmoney").CheckedAt != nil {
		t.Fatal("retired strict EastMoney attempted or blamed")
	}
}
