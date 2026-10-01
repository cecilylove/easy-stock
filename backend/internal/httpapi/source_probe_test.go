package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

type probeQuotesFunc func(context.Context, []string) ([]foundation.Quote, error)

func (f probeQuotesFunc) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	return f(ctx, symbols)
}

type probeCatalogFunc func(context.Context) ([]foundation.StockCatalogEntry, error)

func (f probeCatalogFunc) StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error) {
	return f(ctx)
}

type probeIndexesFunc func(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)

func (f probeIndexesFunc) MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	return f(ctx, scope)
}

type probeNewsFunc func(context.Context, int) ([]foundation.NewsItem, error)

func (f probeNewsFunc) LatestNews(ctx context.Context, limit int) ([]foundation.NewsItem, error) {
	return f(ctx, limit)
}

type probeHotFunc func(context.Context, int) []foundation.HotStockRankList

func (f probeHotFunc) HotStockRanks(ctx context.Context, limit int) []foundation.HotStockRankList {
	return f(ctx, limit)
}

type probeFuturesFunc func(context.Context, string, int) (foundation.MarketFuturesPositionSeries, error)

func (f probeFuturesFunc) Trend(ctx context.Context, variety string, limit int) (foundation.MarketFuturesPositionSeries, error) {
	return f(ctx, variety, limit)
}

type probePoolFunc func(context.Context) (duanxianxia.LimitUpPoolSnapshot, error)

func (f probePoolFunc) FetchLimitUpPool(ctx context.Context) (duanxianxia.LimitUpPoolSnapshot, error) {
	return f(ctx)
}

func successfulProbeProviders(t *testing.T, calls *atomic.Int32) SourceProbeProviders {
	t.Helper()
	meta := func(id string) foundation.SourceMeta {
		calls.Add(1)
		return foundation.SourceMeta{Source: id, FetchedAt: time.Now()}
	}
	return SourceProbeProviders{
		EastMoney: probeCatalogFunc(func(context.Context) ([]foundation.StockCatalogEntry, error) {
			return []foundation.StockCatalogEntry{{BoardStock: foundation.BoardStock{Symbol: "000001.SZ", Name: "平安银行", Meta: meta("eastmoney")}}}, nil
		}),
		Sina: probeQuotesFunc(func(_ context.Context, symbols []string) ([]foundation.Quote, error) {
			if len(symbols) != 1 || symbols[0] != "000001.SZ" {
				t.Errorf("symbols=%v", symbols)
			}
			return []foundation.Quote{{Symbol: "000001.SZ", Name: "平安银行", Price: 10, Meta: meta("sina")}}, nil
		}),
		Tencent: probeIndexesFunc(func(_ context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
			if scope != "core" {
				t.Errorf("scope=%s", scope)
			}
			m := meta("tencent:index")
			return []foundation.MarketIndexSnapshot{{ID: "sse", Price: 3000, Meta: m}}, m, nil
		}),
		CLS: probeNewsFunc(func(_ context.Context, limit int) ([]foundation.NewsItem, error) {
			if limit != 1 {
				t.Errorf("news limit=%d", limit)
			}
			return []foundation.NewsItem{{Title: "新闻", Meta: meta("cls")}}, nil
		}),
		THS: probeHotFunc(func(_ context.Context, limit int) []foundation.HotStockRankList {
			if limit != 1 {
				t.Errorf("hot limit=%d", limit)
			}
			m := meta("ths")
			return []foundation.HotStockRankList{{Source: "ths", FetchedAt: m.FetchedAt, Items: []foundation.HotStockRankItem{{Symbol: "000001.SZ", Name: "平安银行", Rank: 1}}}}
		}),
		CFFEX: probeFuturesFunc(func(_ context.Context, variety string, limit int) (foundation.MarketFuturesPositionSeries, error) {
			if variety != "IF" || limit != 1 {
				t.Errorf("futures=%s/%d", variety, limit)
			}
			m := meta("cffex:futures-position")
			m.FallbackReason = "中金所最近交易日快照；仅提供单日持仓"
			return foundation.MarketFuturesPositionSeries{Variety: "IF", Meta: m, Rows: []foundation.MarketFuturesPositionRow{{TradeDate: "2026-09-30", LongPosition: 10, ShortPosition: 20}}}, nil
		}),
		Kaipanla: probePoolFunc(func(_ context.Context) (duanxianxia.LimitUpPoolSnapshot, error) {
			m := meta("duanxianxia:kaipanla-limit-up")
			return duanxianxia.LimitUpPoolSnapshot{TradeDate: "2026-09-30", FetchedAt: m.FetchedAt, Events: []foundation.LimitUpEvent{{Symbol: "600001.SH", Name: "测试", Meta: m}}}, nil
		}),
	}
}

func TestSourceProbeEndpointFreshResultsAndPassiveRead(t *testing.T) {
	var calls atomic.Int32
	p := successfulProbeProviders(t, &calls)
	s := NewServer(Config{SourceProbeProviders: &p})
	t.Cleanup(func() { _ = s.Close() })
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		s.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil))
		return r
	}
	if rec := get(); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"probes":[]`) || calls.Load() != 0 {
		t.Fatalf("initial GET=%s calls=%d", rec.Body.String(), calls.Load())
	}
	s.sourceHealth.markFailure("sina", "业务请求失败")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sources/check", nil))
	var response struct {
		Sources   []foundation.SourceHealth `json:"sources"`
		Probes    []SourceProbeResult       `json:"probes"`
		CheckedAt time.Time                 `json:"checked_at"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.CheckedAt.IsZero() || len(response.Probes) != 7 || calls.Load() != 7 {
		t.Fatalf("POST=%d %s calls=%d", rec.Code, rec.Body.String(), calls.Load())
	}
	for i, result := range response.Probes {
		if result.Status != "available" || result.CheckedAt.IsZero() || result.Scope == "" || result.ID != response.Sources[i].ID {
			t.Fatalf("result=%+v", result)
		}
	}
	if response.Sources[2].Status != "degraded" {
		t.Fatalf("probe masked business failure: %+v", response.Sources[2])
	}
	after := get()
	var persisted struct {
		Probes []SourceProbeResult `json:"probes"`
	}
	_ = json.Unmarshal(after.Body.Bytes(), &persisted)
	if calls.Load() != 7 || len(persisted.Probes) != 7 || !persisted.Probes[0].CheckedAt.Equal(response.Probes[0].CheckedAt) {
		t.Fatalf("GET refreshed probes: calls=%d body=%s", calls.Load(), after.Body.String())
	}
	// A later click always starts new actual requests; there is no result TTL cache.
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sources/check", nil))
	if rec.Code != 200 || calls.Load() != 14 {
		t.Fatalf("second click calls=%d", calls.Load())
	}
}

func TestSourceProbeRejectsEmptyStaleFallbackAndWrongSource(t *testing.T) {
	cases := []struct {
		name, id string
		modify   func(*SourceProbeProviders)
	}{
		{"empty quote", "sina", func(p *SourceProbeProviders) {
			p.Sina = probeQuotesFunc(func(context.Context, []string) ([]foundation.Quote, error) { return nil, nil })
		}},
		{"empty EM catalog", "eastmoney", func(p *SourceProbeProviders) {
			p.EastMoney = probeCatalogFunc(func(context.Context) ([]foundation.StockCatalogEntry, error) { return nil, nil })
		}},
		{"invalid EM catalog", "eastmoney", func(p *SourceProbeProviders) {
			p.EastMoney = probeCatalogFunc(func(context.Context) ([]foundation.StockCatalogEntry, error) {
				return []foundation.StockCatalogEntry{{BoardStock: foundation.BoardStock{Symbol: "bad", Name: "bad", Meta: foundation.SourceMeta{Source: "eastmoney", FetchedAt: time.Now()}}}}, nil
			})
		}},
		{"alternate EM catalog", "eastmoney", func(p *SourceProbeProviders) {
			p.EastMoney = probeCatalogFunc(func(context.Context) ([]foundation.StockCatalogEntry, error) {
				return []foundation.StockCatalogEntry{{BoardStock: foundation.BoardStock{Symbol: "000001.SZ", Name: "平安银行", Meta: foundation.SourceMeta{Source: "sina:stock-directory", FetchedAt: time.Now(), FallbackReason: "主源失败"}}}}, nil
			})
		}},
		{"cached EM catalog", "eastmoney", func(p *SourceProbeProviders) {
			p.EastMoney = probeCatalogFunc(func(context.Context) ([]foundation.StockCatalogEntry, error) {
				return []foundation.StockCatalogEntry{{BoardStock: foundation.BoardStock{Symbol: "000001.SZ", Name: "平安银行", Meta: foundation.SourceMeta{Source: "eastmoney", FetchedAt: time.Now().Add(-time.Hour)}}}}, nil
			})
		}},
		{"stale quote", "sina", func(p *SourceProbeProviders) {
			p.Sina = probeQuotesFunc(func(context.Context, []string) ([]foundation.Quote, error) {
				return []foundation.Quote{{Symbol: "000001.SZ", Name: "平安银行", Price: 10, Meta: foundation.SourceMeta{Source: "sina", Stale: true, FetchedAt: time.Now()}}}, nil
			})
		}},
		{"cached quote", "sina", func(p *SourceProbeProviders) {
			p.Sina = probeQuotesFunc(func(context.Context, []string) ([]foundation.Quote, error) {
				return []foundation.Quote{{Symbol: "000001.SZ", Name: "平安银行", Price: 10, Meta: foundation.SourceMeta{Source: "sina", FetchedAt: time.Now().Add(-time.Hour)}}}, nil
			})
		}},
		{"alternate quote", "sina", func(p *SourceProbeProviders) {
			p.Sina = probeQuotesFunc(func(context.Context, []string) ([]foundation.Quote, error) {
				return []foundation.Quote{{Symbol: "000001.SZ", Name: "平安银行", Price: 10, Meta: foundation.SourceMeta{Source: "eastmoney", FetchedAt: time.Now()}}}, nil
			})
		}},
		{"partial foreign indexes", "tencent", func(p *SourceProbeProviders) {
			p.Tencent = probeIndexesFunc(func(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
				m := foundation.SourceMeta{Source: "tencent:index", FetchedAt: time.Now()}
				return []foundation.MarketIndexSnapshot{{ID: "dow", Price: 10, Meta: m}}, m, nil
			})
		}},
		{"fallback indexes", "tencent", func(p *SourceProbeProviders) {
			p.Tencent = probeIndexesFunc(func(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
				m := foundation.SourceMeta{Source: "tencent:index", FetchedAt: time.Now(), FallbackReason: "cache"}
				return []foundation.MarketIndexSnapshot{{ID: "sse", Price: 10, Meta: m}}, m, nil
			})
		}},
		{"empty news", "cls", func(p *SourceProbeProviders) {
			p.CLS = probeNewsFunc(func(context.Context, int) ([]foundation.NewsItem, error) {
				return []foundation.NewsItem{{Meta: foundation.SourceMeta{Source: "cls", FetchedAt: time.Now()}}}, nil
			})
		}},
		{"wrong hot source", "ths", func(p *SourceProbeProviders) {
			p.THS = probeHotFunc(func(context.Context, int) []foundation.HotStockRankList {
				return []foundation.HotStockRankList{{Source: "eastmoney", FetchedAt: time.Now(), Items: []foundation.HotStockRankItem{{Symbol: "000001.SZ", Name: "平安银行", Rank: 1}}}}
			})
		}},
		{"hot error redacted", "ths", func(p *SourceProbeProviders) {
			p.THS = probeHotFunc(func(context.Context, int) []foundation.HotStockRankList {
				return []foundation.HotStockRankList{{Source: "ths", Error: "secret https://example.test?token=secret"}}
			})
		}},
		{"empty futures", "cffex", func(p *SourceProbeProviders) {
			p.CFFEX = probeFuturesFunc(func(context.Context, string, int) (foundation.MarketFuturesPositionSeries, error) {
				return foundation.MarketFuturesPositionSeries{Variety: "IF", Meta: foundation.SourceMeta{Source: "cffex", FetchedAt: time.Now()}}, nil
			})
		}},
		{"empty pool", "duanxianxia", func(p *SourceProbeProviders) {
			p.Kaipanla = probePoolFunc(func(context.Context) (duanxianxia.LimitUpPoolSnapshot, error) {
				return duanxianxia.LimitUpPoolSnapshot{FetchedAt: time.Now(), TradeDate: "2026-09-30"}, nil
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			p := successfulProbeProviders(t, &calls)
			tc.modify(&p)
			tracker := newSourceProbeTracker(p)
			results, _, err := tracker.check(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, result := range results {
				if result.ID == tc.id && (result.Status != "unavailable" || strings.Contains(result.Message, "secret") || strings.Contains(result.Message, "http")) {
					t.Fatalf("result=%+v", result)
				}
			}
		})
	}
}

func TestSourceProbeTimeoutAndUpstreamErrorsArePerSource(t *testing.T) {
	var calls atomic.Int32
	p := successfulProbeProviders(t, &calls)
	p.Sina = probeQuotesFunc(func(ctx context.Context, _ []string) ([]foundation.Quote, error) { <-ctx.Done(); return nil, ctx.Err() })
	p.CLS = probeNewsFunc(func(context.Context, int) ([]foundation.NewsItem, error) {
		return nil, errors.New("secret https://example.test?token=secret")
	})
	s := NewServer(Config{SourceProbeProviders: &p})
	t.Cleanup(func() { _ = s.Close() })
	s.sourceProbes.timeout = 20 * time.Millisecond
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sources/check", nil))
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("response=%d %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Probes []SourceProbeResult `json:"probes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &response)
	if response.Probes[2].Status != "unavailable" || response.Probes[2].Message != "检测超时，请稍后重试" || response.Probes[4].Status != "unavailable" || response.Probes[3].Status != "available" {
		t.Fatalf("probes=%+v", response.Probes)
	}
}

func awaitProbeCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSourceProbeConcurrentRequestsJoinAndCancelOnlyLastWaiter(t *testing.T) {
	var calls atomic.Int32
	p := successfulProbeProviders(t, &calls)
	started, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p.Sina = probeQuotesFunc(func(ctx context.Context, _ []string) ([]foundation.Quote, error) {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			close(cancelled)
			return nil, ctx.Err()
		case <-release:
			return []foundation.Quote{{Symbol: "000001.SZ", Name: "平安银行", Price: 10, Meta: foundation.SourceMeta{Source: "sina", FetchedAt: time.Now()}}}, nil
		}
	})
	tracker := newSourceProbeTracker(p)
	ctx, cancel := context.WithCancel(context.Background())
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, _, err := tracker.check(ctx); first <- err }()
	<-started
	go func() { _, _, err := tracker.check(context.Background()); second <- err }()
	awaitProbeCondition(t, func() bool {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		return tracker.active != nil && tracker.active.waiters == 2
	})
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first=%v", err)
	}
	select {
	case <-cancelled:
		t.Fatal("joined caller lost its upstream")
	default:
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 7 || len(tracker.snapshot()) != 7 {
		t.Fatalf("joined batch calls=%d results=%v", calls.Load(), tracker.snapshot())
	}
}

func TestSourceProbeLastWaiterCancellationAndCloseDoNotSaveFailures(t *testing.T) {
	for _, closeServer := range []bool{false, true} {
		t.Run(fmt.Sprint("close=", closeServer), func(t *testing.T) {
			var calls atomic.Int32
			p := successfulProbeProviders(t, &calls)
			started, cancelled := make(chan struct{}), make(chan struct{})
			p.Sina = probeQuotesFunc(func(ctx context.Context, _ []string) ([]foundation.Quote, error) {
				close(started)
				<-ctx.Done()
				close(cancelled)
				return nil, ctx.Err()
			})
			s := NewServer(Config{SourceProbeProviders: &p})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() { _, _, err := s.sourceProbes.check(ctx); finished <- err }()
			<-started
			s.sourceProbes.mu.Lock()
			batch := s.sourceProbes.active
			s.sourceProbes.mu.Unlock()
			if closeServer {
				_ = s.Close()
			} else {
				cancel()
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("upstream was not cancelled")
			}
			if err := <-finished; !errors.Is(err, context.Canceled) {
				t.Fatalf("check=%v", err)
			}
			<-batch.done
			if len(s.sourceProbes.snapshot()) != 0 {
				t.Fatalf("cancelled failures were saved: %+v", s.sourceProbes.snapshot())
			}
			if !closeServer {
				awaitProbeCondition(t, func() bool {
					s.sourceProbes.mu.Lock()
					defer s.sourceProbes.mu.Unlock()
					return s.sourceProbes.active == nil
				})
				// A new click may start immediately after the last waiter cancels.
				s.sourceProbes.probes = sourceProbeDefinitions(successfulProbeProviders(t, &calls))
				if results, _, err := s.sourceProbes.check(context.Background()); err != nil || len(results) != 7 {
					t.Fatalf("retry=%v %v", results, err)
				}
				_ = s.Close()
			}
		})
	}
}

func TestSourceProbeRunsAllRepresentativesConcurrently(t *testing.T) {
	started := make(chan string, 7)
	release := make(chan struct{})
	tracker := newSourceProbeTracker(SourceProbeProviders{})
	for i := range tracker.probes {
		id := tracker.probes[i].id
		tracker.probes[i].check = func(ctx context.Context, _ time.Time) error {
			started <- id
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	finished := make(chan error, 1)
	go func() { _, _, err := tracker.check(context.Background()); finished <- err }()
	defer tracker.close()
	for i := 0; i < 7; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			tracker.close()
			t.Fatal("sources did not start in parallel")
		}
	}
	// Release before reading the shared batch completion.
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestSourceProbeEndpointRejectsUntrustedRequestsBeforeCallingUpstream(t *testing.T) {
	var calls atomic.Int32
	p := successfulProbeProviders(t, &calls)
	s := NewServer(Config{Token: "test-token", EnforceLoopbackHost: true, SourceProbeProviders: &p})
	t.Cleanup(func() { _ = s.Close() })
	for _, tc := range []struct {
		origin, host, token string
		status              int
	}{
		{"https://evil.example", "127.0.0.1:20081", "test-token", 403},
		{"", "127.0.0.1:20081", "", 401},
		{"", "evil.example:20081", "test-token", 403},
	} {
		req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/v1/sources/check", nil)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != tc.status || calls.Load() != 0 {
			t.Fatalf("response=%d expected=%d calls=%d", rec.Code, tc.status, calls.Load())
		}
	}
}

type probeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f probeRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSourceProbeDefaultProvidersUseIndependentDirectUpstreams(t *testing.T) {
	// Intercept the actual default HTTP clients, retaining their parsers. No
	// public network, application cache, persistence service, or fallback runs.
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var eastMoneyCalls, calls, exchangeCalls atomic.Int32
	plain := []byte(`{"list":[["603629","利通电子",10.01,120000000,2,"09:31:02","算力租赁","2天2板",880000000,9200000000,"回封","2","10:22:08"]]}`)
	block, _ := aes.NewCipher([]byte("secretkey322yes!!aaaaaaaaaaaaaaa"))
	padding := block.BlockSize() - len(plain)%block.BlockSize()
	for i := 0; i < padding; i++ {
		plain = append(plain, byte(padding))
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, []byte("fixediv_16valued")).CryptBlocks(encrypted, plain)
	http.DefaultTransport = probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := ""
		switch r.URL.Host {
		case "push2his.eastmoney.com":
			return nil, fmt.Errorf("retired EastMoney K probe was called")
		case "data.eastmoney.com":
			eastMoneyCalls.Add(1)
			if r.URL.Path != "/dataapi/xuangu/list" {
				t.Errorf("EM directory probe endpoint=%s", r.URL.Path)
			}
			body = `{"code":0,"success":true,"result":{"nextpage":false,"currentpage":1,"count":1,"data":[{"SECUCODE":"000001.SZ","SECURITY_NAME_ABBR":"平安银行","INDUSTRY":"银行"}]}}`
		case "hq.sinajs.cn":
			body = `var hq_str_sz000001="平安银行,10.00,9.80,10.50,10.80,9.90,10.49,10.50,123456,123456789.00,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,2026-09-30,15:00:00,00";`
		case "qt.gtimg.cn":
			fields := make([]string, 33)
			fields[1], fields[2], fields[3], fields[30], fields[31], fields[32] = "上证指数", "000001", "3500", "20260930150000", "10", "0.3"
			body = `v_sh000001="` + strings.Join(fields, "~") + `";`
		case "www.cls.cn":
			body = `{"errno":0,"data":{"roll_data":[{"id":123,"title":"测试快讯","content":"最新电报","ctime":1781265600}]}}`
		case "eq.10jqka.com.cn":
			body = `{"status_code":0,"data":{"stock_list":[{"order":1,"code":"000001","name":"平安银行"}]}}`
		case "duanxianxia.com":
			if strings.HasSuffix(r.URL.Path, "datasource.json") {
				body = `{}`
			} else {
				body = base64.StdEncoding.EncodeToString(encrypted)
			}
		case "www.cffex.com.cn":
			exchangeCalls.Add(1)
			parts := strings.Split(r.URL.Path, "/")
			date := parts[len(parts)-3] + parts[len(parts)-2]
			for rank := 1; rank <= 20; rank++ {
				body += fmt.Sprintf("%s,IF2610,%d,member,100,0,long,10,1,short,20,2\n", date, rank)
			}
		default:
			return nil, fmt.Errorf("unexpected upstream %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	s := NewServer(nil)
	t.Cleanup(func() { _ = s.Close() })
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sources/check", nil))
	var response struct {
		Probes []SourceProbeResult `json:"probes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &response)
	if rec.Code != 200 || eastMoneyCalls.Load() != 1 || len(response.Probes) != 7 || calls.Load() < 8 {
		t.Fatalf("status=%d EM=%d calls=%d body=%s", rec.Code, eastMoneyCalls.Load(), calls.Load(), rec.Body.String())
	}
	for _, p := range response.Probes {
		if p.Status != "available" {
			t.Fatalf("default direct probe failed: %+v", p)
		}
	}
	// The exchange loader starts a four-day parallel batch. Ensure every
	// request has entered the isolated transport before restoring defaults.
	awaitProbeCondition(t, func() bool { return exchangeCalls.Load() == 4 })
	second := httptest.NewRecorder()
	s.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/sources/check", nil))
	if second.Code != 200 || eastMoneyCalls.Load() != 2 {
		t.Fatalf("manual directory probe reused cached catalog: %d %d", second.Code, eastMoneyCalls.Load())
	}
	awaitProbeCondition(t, func() bool { return exchangeCalls.Load() == 8 })
}
