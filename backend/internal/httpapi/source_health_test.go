package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"github.com/gorilla/websocket"
)

func sourceByID(t *testing.T, items []foundation.SourceHealth, id string) foundation.SourceHealth {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("source %s not found", id)
	return foundation.SourceHealth{}
}

func TestSourceHealthUnknownUntilRealObservation(t *testing.T) {
	server := NewServer(nil)
	defer server.Close()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	items := server.sourceHealth.snapshot(time.Now())
	if len(items) != 7 {
		t.Fatalf("catalog should contain the seven integrated sources, got %d", len(items))
	}
	for _, id := range []string{"duanxianxia", "eastmoney", "cffex", "sina", "tencent", "cls", "ths"} {
		source := sourceByID(t, items, id)
		if source.OK || source.Status != "unknown" || source.CheckedAt != nil {
			t.Fatalf("%s was declared healthy without a request: %+v", id, source)
		}
	}
	for _, id := range []string{"tradingview", "tushare"} {
		if sourceID(id) != "" {
			t.Fatalf("unintegrated source %s should not accept observations", id)
		}
		for _, source := range items {
			if source.ID == id {
				t.Fatalf("unintegrated source should not appear in catalog: %+v", source)
			}
		}
	}
}

func TestDetailQuoteObservesRealFetchWithoutRenewingOnCacheHit(t *testing.T) {
	server := NewServer(Config{Realtime: &observedRealtimeProvider{}})
	defer server.Close()
	fetch := func() {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/realtime?symbols=000001.SZ&detail=1", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("quote failed: %d %s", response.Code, response.Body.String())
		}
	}
	fetch()
	first := sourceByID(t, server.sourceHealth.snapshot(time.Now()), "sina")
	if first.Status != "available" || first.CheckedAt == nil || first.LastSuccess == nil {
		t.Fatalf("real detail fetch was not observed: %+v", first)
	}
	fetch()
	cached := sourceByID(t, server.sourceHealth.snapshot(time.Now()), "sina")
	if !cached.CheckedAt.Equal(*first.CheckedAt) || !cached.LastSuccess.Equal(*first.LastSuccess) {
		t.Fatalf("cached detail fetch renewed health: first=%+v cached=%+v", first, cached)
	}
}

func TestSourceHealthTracksFreshFallbackFailureAndExpiry(t *testing.T) {
	tracker := newSourceHealthTracker()
	now := time.Now()
	meta := foundation.SourceMeta{Source: "cffex:futures-position", FetchedAt: now}
	tracker.success(meta)
	entry := sourceByID(t, tracker.snapshot(now), "cffex")
	if !entry.OK || entry.Status != "available" || entry.LastSuccess == nil {
		t.Fatalf("fresh result not observed: %+v", entry)
	}
	tracker.success(foundation.SourceMeta{Source: "cffex:futures-position", FetchedAt: now.Add(-time.Minute)})
	if checked := sourceByID(t, tracker.snapshot(now), "cffex").CheckedAt; !checked.Equal(now) {
		t.Fatalf("cached response changed observation time: %v", checked)
	}
	tracker.failure("cffex", errors.New("upstream URL with secret token"))
	entry = sourceByID(t, tracker.snapshot(time.Now()), "cffex")
	if entry.OK || entry.Status != "degraded" || entry.LastFailure == nil || entry.LastSuccess == nil {
		t.Fatalf("failed result not recorded: %+v", entry)
	}
	if entry.Message == "" || entry.Message == "upstream URL with secret token" {
		t.Fatalf("upstream error leaked into source catalog: %+v", entry)
	}
	tracker.success(foundation.SourceMeta{Source: "sina", FetchedAt: time.Now()})
	if source := sourceByID(t, tracker.snapshot(time.Now()), "sina"); source.Status != "available" {
		t.Fatalf("fallback provider should be independently available: %+v", source)
	}
	entry = sourceByID(t, tracker.snapshot(time.Now().Add(sourceObservationTTL+time.Second)), "cffex")
	if entry.OK || entry.Status != "unknown" || entry.LastFailure == nil {
		t.Fatalf("expired observation should not remain current: %+v", entry)
	}
}

func TestSourceHealthCategoriesDescribeImplementedCapabilities(t *testing.T) {
	items := newSourceHealthTracker().snapshot(time.Now())
	for _, test := range []struct {
		id      string
		present []string
		absent  []string
	}{
		{id: "cls", present: []string{"news"}, absent: []string{"calendar"}},
		{id: "tencent", present: []string{"index", "kline", "sector", "sector-stocks", "us-sector"}, absent: []string{"quote", "hk"}},
		{id: "cffex", present: []string{"futures", "futures-members", "futures-consensus"}, absent: []string{"quote", "kline", "margin"}},
		{id: "sina", present: []string{"quote", "kline", "money-flow", "stock-directory"}, absent: []string{"concept", "auction"}},
		{id: "ths", present: []string{"hot-ranks"}, absent: []string{"quote", "kline", "theme", "billboard-labels"}},
		{id: "eastmoney", present: []string{"auction", "stock-directory", "concept", "business", "fundamentals", "market-pools", "margin", "billboard", "announcement", "report", "hot-ranks", "futures"}, absent: []string{"kline", "index"}},
	} {
		t.Run(test.id, func(t *testing.T) {
			categories := make(map[string]bool)
			for _, category := range strings.Split(sourceByID(t, items, test.id).Category, ",") {
				categories[category] = true
			}
			for _, category := range test.present {
				if !categories[category] {
					t.Fatalf("implemented capability %s missing for %s", category, test.id)
				}
			}
			for _, category := range test.absent {
				if categories[category] {
					t.Fatalf("unsupported capability %s advertised for %s", category, test.id)
				}
			}
		})
	}
}

type observedRealtimeProvider struct {
	mu  sync.RWMutex
	err error
}

func (p *observedRealtimeProvider) setError(err error) {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
}

func (p *observedRealtimeProvider) Realtime(_ context.Context, _ []string) ([]foundation.Quote, error) {
	p.mu.RLock()
	err := p.err
	p.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return []foundation.Quote{{Symbol: "000001.SZ", Meta: foundation.SourceMeta{Source: "sina", FetchedAt: time.Now()}}}, nil
}

func TestWebSocketQuoteSnapshotObservesSuccessAndFailure(t *testing.T) {
	provider := &observedRealtimeProvider{}
	s := NewServer(Config{Realtime: provider})
	s.realtimeSourceID = "sina" // Explicitly identify the mock provider.
	defer s.Close()
	remote := httptest.NewServer(s)
	defer remote.Close()
	for _, failed := range []bool{false, true} {
		if failed {
			provider.setError(errors.New("realtime unavailable"))
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(remote.URL, "http")+"/api/v1/ws/stream?symbols=000001.SZ", nil)
		if err != nil {
			t.Fatal(err)
		}
		var message streamMessage
		if err := conn.ReadJSON(&message); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		conn.Close()
		entry := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "sina")
		if !failed && (message.Type != "quotes" || entry.Status != "available") {
			t.Fatalf("websocket success not observed: message=%+v source=%+v", message, entry)
		}
		if failed && (message.Type != "error" || entry.Status != "degraded") {
			t.Fatalf("websocket failure not observed: message=%+v source=%+v", message, entry)
		}
	}
}

func TestCapabilityObservationsDoNotPromoteOtherFailedFunctions(t *testing.T) {
	tracker := newSourceHealthTracker()
	now := time.Now()
	tracker.observe(foundation.SourceObservation{SourceID: "eastmoney", Capability: "stock-kline:day", AttemptAt: now, Failed: true})
	tracker.observe(foundation.SourceObservation{SourceID: "eastmoney", Capability: "limit-up-pool", AttemptAt: now.Add(time.Second), Meta: foundation.SourceMeta{Source: "eastmoney", FetchedAt: now.Add(time.Second), Capability: "limit-up-pool"}})
	source := sourceByID(t, tracker.snapshot(now.Add(2*time.Second)), "eastmoney")
	if len(source.Capabilities) != 2 {
		t.Fatalf("capability history lost: %+v", source)
	}
	for _, row := range source.Capabilities {
		if row.Capability == "stock-kline:day" && row.Status != "degraded" {
			t.Fatalf("another capability erased failure: %+v", row)
		}
		if row.Capability == "limit-up-pool" && row.Status != "available" {
			t.Fatalf("successful capability not recorded: %+v", row)
		}
	}
	old := tracker.snapshot(now.Add(11 * time.Minute))
	for _, row := range sourceByID(t, old, "eastmoney").Capabilities {
		if row.Status != "unknown" {
			t.Fatalf("capability did not expire: %+v", row)
		}
	}
}

type observedKLineProvider struct {
	source string
	err    error
}

func (p observedKLineProvider) KLine(_ context.Context, symbol, _ string, _ int) ([]foundation.KLine, error) {
	if p.err != nil {
		return nil, p.err
	}
	return []foundation.KLine{{Symbol: symbol, Time: time.Now(), Open: 10, High: 11, Low: 9, Close: 10, Meta: foundation.SourceMeta{Source: p.source, FetchedAt: time.Now()}}}, nil
}

func TestInjectedProviderFailureDoesNotBlameDefaultSource(t *testing.T) {
	s := NewServer(Config{
		Realtime:      &observedRealtimeProvider{err: errors.New("custom quote failed")},
		KLinePrimary:  observedKLineProvider{err: errors.New("custom kline failed")},
		KLineFallback: observedKLineProvider{err: errors.New("custom fallback failed")},
	})
	defer s.Close()
	for _, path := range []string{"/api/v1/quotes/realtime?symbols=000001.SZ", "/api/v1/quotes/kline?symbol=000001.SZ"} {
		response := httptest.NewRecorder()
		s.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusBadGateway {
			t.Fatalf("%s status=%d", path, response.Code)
		}
	}
	for _, id := range []string{"sina", "cffex"} {
		if entry := sourceByID(t, s.sourceHealth.snapshot(time.Now()), id); entry.Status != "unknown" {
			t.Fatalf("custom provider failure attributed to %s: %+v", id, entry)
		}
	}
}

func TestKLineFallbackObservesBothProviderOutcomes(t *testing.T) {
	server := NewServer(Config{
		KLinePrimary:  observedKLineProvider{err: errors.New("primary unavailable")},
		KLineFallback: observedKLineProvider{source: "sina"},
	})
	server.kLinePrimarySourceID = "tencent" // Explicitly identify the injected mock; no production stock-K fallback is claimed.
	defer server.Close()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000001.SZ", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("fallback response status = %d", response.Code)
	}
	items := server.sourceHealth.snapshot(time.Now())
	if primary := sourceByID(t, items, "tencent"); primary.Status != "degraded" || primary.OK {
		t.Fatalf("failed primary was marked available: %+v", primary)
	}
	if fallback := sourceByID(t, items, "sina"); fallback.Status != "available" || !fallback.OK {
		t.Fatalf("successful fallback was not observed: %+v", fallback)
	}
}

type observedMarketOverview struct {
	fakeMarketOverviewProvider
}

func (p *observedMarketOverview) MarketIndexSeries(_ context.Context, _, _ string, _ int) (foundation.MarketIndexSeries, error) {
	meta := foundation.SourceMeta{Source: "tencent:index-kline", FetchedAt: time.Now()}
	return foundation.MarketIndexSeries{Meta: meta}, nil
}

func (p *observedMarketOverview) MarketBillboardDetail(_ context.Context, _, _, _ string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "cffex:billboard-seats", FetchedAt: time.Now()}
	return foundation.MarketBillboardDetail{Meta: meta}, meta, nil
}

func TestIndependentMarketHandlersRecordActualSources(t *testing.T) {
	s := NewServer(Config{MarketOverview: &observedMarketOverview{}})
	defer s.Close()
	for _, path := range []string{
		"/api/v1/market/index-series?id=sse&period=day&limit=30",
		"/api/v1/market/billboard/detail?symbol=000001.SZ&trade_date=2026-09-29&reason=test",
	} {
		recorder := httptest.NewRecorder()
		s.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
	for _, id := range []string{"tencent", "cffex"} {
		if item := sourceByID(t, s.sourceHealth.snapshot(time.Now()), id); item.Status != "available" {
			t.Fatalf("independent market handler did not observe %s: %+v", id, item)
		}
	}
}

func TestServiceDeadlineIsObservedButClientCancellationIsNot(t *testing.T) {
	tracker := newSourceHealthTracker()
	cache := newMarketOverviewCache(time.Minute)
	loader := func(ctx context.Context) (int, foundation.SourceMeta, error) {
		<-ctx.Done()
		return 0, foundation.SourceMeta{}, ctx.Err()
	}
	deadline, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, _, _ = loadMarketOverview(deadline, cache, tracker, "margin", loader, "cffex")
	if source := sourceByID(t, tracker.snapshot(time.Now()), "cffex"); source.Status != "degraded" {
		t.Fatalf("service deadline was not observed: %+v", source)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	_, _, _ = loadMarketOverview(cancelled, cache, tracker, "other", loader, "sina")
	if source := sourceByID(t, tracker.snapshot(time.Now()), "sina"); source.Status != "unknown" {
		t.Fatalf("client cancellation was attributed to provider: %+v", source)
	}
}

func TestCustomMarketProviderFailureDoesNotBlameDefaultSource(t *testing.T) {
	provider := &fakeMarketOverviewProvider{fail: true}
	s := NewServer(Config{MarketOverview: provider})
	defer s.Close()
	for _, path := range []string{
		"/api/v1/market/margin-balance?limit=5",
		"/api/v1/market/billboard/detail?symbol=000001.SZ&trade_date=2026-09-29&reason=test",
	} {
		recorder := httptest.NewRecorder()
		s.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadGateway {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
	}
	if entry := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "cffex"); entry.Status != "unknown" {
		t.Fatalf("custom market provider failure blamed default exchange source: %+v", entry)
	}
}

func TestSingleSourceMarketFailureWithoutCacheIsObserved(t *testing.T) {
	tracker := newSourceHealthTracker()
	cache := newMarketOverviewCache(time.Minute)
	loader := func(context.Context) (int, foundation.SourceMeta, error) {
		return 0, foundation.SourceMeta{}, errors.New("offline")
	}
	if _, _, err := loadMarketOverview(context.Background(), cache, tracker, "margin", loader, "cffex"); err == nil {
		t.Fatal("expected upstream error")
	}
	if source := sourceByID(t, tracker.snapshot(time.Now()), "cffex"); source.Status != "degraded" || source.LastFailure == nil {
		t.Fatalf("first failed request remained unknown: %+v", source)
	}
}

func TestMarketOverviewCacheHitDoesNotRenewFallbackFailure(t *testing.T) {
	tracker := newSourceHealthTracker()
	cache := newMarketOverviewCache(time.Minute)
	calls := 0
	loader := func(context.Context) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
		calls++
		meta := foundation.SourceMeta{Source: "tencent:index", FetchedAt: time.Now(), FallbackReason: "新浪资金榜不可用，已切换注入的备用来源"}
		return []foundation.MarketIndexSnapshot{{ID: "sse", Meta: meta}}, meta, nil
	}
	if _, _, err := loadMarketOverview(context.Background(), cache, tracker, "indexes", loader); err != nil {
		t.Fatal(err)
	}
	first := sourceByID(t, tracker.snapshot(time.Now()), "sina")
	if first.Status != "degraded" || first.LastFailure == nil {
		t.Fatalf("failed primary not observed: %+v", first)
	}
	if _, _, err := loadMarketOverview(context.Background(), cache, tracker, "indexes", loader); err != nil {
		t.Fatal(err)
	}
	cached := sourceByID(t, tracker.snapshot(time.Now()), "sina")
	if calls != 1 || !cached.LastFailure.Equal(*first.LastFailure) {
		t.Fatalf("cache hit renewed fallback observation: calls=%d before=%+v after=%+v", calls, first, cached)
	}
}

func TestMarketOverviewStaleFallbackRecordsOnlyRefreshAttempt(t *testing.T) {
	tracker := newSourceHealthTracker()
	cache := newMarketOverviewCache(-time.Second)
	meta := foundation.SourceMeta{Source: "cffex:index", FetchedAt: time.Now()}
	if _, _, err := loadMarketOverview(context.Background(), cache, tracker, "indexes", func(context.Context) (int, foundation.SourceMeta, error) { return 1, meta, nil }); err != nil {
		t.Fatal(err)
	}
	loader := func(context.Context) (int, foundation.SourceMeta, error) {
		return 0, foundation.SourceMeta{}, errors.New("offline")
	}
	if _, stale, err := loadMarketOverview(context.Background(), cache, tracker, "indexes", loader, "cffex"); err != nil || !stale.Stale {
		t.Fatalf("stale fallback failed: meta=%+v error=%v", stale, err)
	}
	first := sourceByID(t, tracker.snapshot(time.Now()), "cffex")
	if first.Status != "degraded" || first.LastFailure == nil {
		t.Fatalf("stale refresh not observed: %+v", first)
	}
	// The original successful fetch time must never override the failure.
	tracker.success(meta)
	after := sourceByID(t, tracker.snapshot(time.Now()), "cffex")
	if after.OK || !after.LastFailure.Equal(*first.LastFailure) {
		t.Fatalf("cached source metadata masked a refresh failure: before=%+v after=%+v", first, after)
	}
}

func TestCompositeMarketCacheDoesNotBlamePreviousSource(t *testing.T) {
	tracker := newSourceHealthTracker()
	cache := newMarketOverviewCache(-time.Second)
	meta := foundation.SourceMeta{Source: "tencent:index", FetchedAt: time.Now()}
	if _, _, err := loadMarketOverview(context.Background(), cache, tracker, "indexes", func(context.Context) (int, foundation.SourceMeta, error) { return 1, meta, nil }); err != nil {
		t.Fatal(err)
	}
	if _, stale, err := loadMarketOverview(context.Background(), cache, tracker, "indexes", func(context.Context) (int, foundation.SourceMeta, error) {
		return 0, foundation.SourceMeta{}, errors.New("both upstreams failed")
	}); err != nil || !stale.Stale {
		t.Fatalf("expected stale fallback: meta=%+v err=%v", stale, err)
	}
	if item := sourceByID(t, tracker.snapshot(time.Now()), "tencent"); item.Status != "available" || item.LastFailure != nil {
		t.Fatalf("composed refresh blamed previous source: %+v", item)
	}
}

func TestIndustryFallbackFromProgressObservesBothProviders(t *testing.T) {
	tracker := newSourceHealthTracker()
	// Fused theme observations preserve each provider's actual result.
	meta := foundation.SourceMeta{Source: "sina:mock-fallback", FetchedAt: time.Now(), FallbackReason: "腾讯行业强度不可用，已切换注入的备用来源"}
	tracker.observe(foundation.SourceObservation{Meta: meta})
	if source := sourceByID(t, tracker.snapshot(time.Now()), "tencent"); source.Status != "degraded" {
		t.Fatalf("failed primary not recorded: %+v", source)
	}
	if source := sourceByID(t, tracker.snapshot(time.Now()), "sina"); source.Status != "available" {
		t.Fatalf("successful fallback not recorded: %+v", source)
	}
}

func TestSymbolPriceObservationDoesNotDegradeMarketAndIsBounded(t *testing.T) {
	tracker := newSourceHealthTracker()
	base := time.Now()
	tracker.observe(foundation.SourceObservation{SourceID: "tencent", Capability: "stock-kline:day:qfq", Meta: foundation.SourceMeta{Source: "tencent:stock-kline", FetchedAt: base}})
	for i := 0; i < 200; i++ {
		tracker.observe(foundation.SourceObservation{SourceID: "tencent", Capability: "stock-kline:day:qfq:symbol:" + time.Duration(i).String(), AttemptAt: base.Add(time.Duration(i+1) * time.Millisecond), Failed: true})
	}
	row := sourceByID(t, tracker.snapshot(base.Add(time.Second)), "tencent")
	if row.Status != "available" || row.LastFailure != nil {
		t.Fatalf("one instrument degraded whole supplier: %+v", row)
	}
	if len(row.Capabilities) != 129 {
		t.Fatalf("instrument records not bounded independently: %d", len(row.Capabilities))
	}
	for _, capability := range row.Capabilities {
		if strings.Contains(capability.Capability, ":symbol:") && !strings.Contains(capability.Message, "该标的") {
			t.Fatalf("symbol scope mislabeled: %+v", capability)
		}
	}
	tracker.observe(foundation.SourceObservation{SourceID: "tencent", Capability: "stock-kline:day:qfq", AttemptAt: base.Add(2 * time.Second), Failed: true})
	if row = sourceByID(t, tracker.snapshot(base.Add(2*time.Second)), "tencent"); row.Status != "degraded" {
		t.Fatal("transport failure no longer degrades market")
	}
}

func TestExchangeFallbackPreservesEastMoneyFailureObservation(t *testing.T) {
	tracker := newSourceHealthTracker()
	meta := foundation.SourceMeta{Source: "cffex:futures-position", FetchedAt: time.Now(), FallbackReason: "东方财富期指数据不可用，降级为中金所最近交易日快照"}
	tracker.fallback(meta)
	tracker.success(meta)
	if source := sourceByID(t, tracker.snapshot(time.Now()), "cffex"); source.Status != "available" || source.LastFailure != nil {
		t.Fatalf("exchange request not observed independently: %+v", source)
	}
	if source := sourceByID(t, tracker.snapshot(time.Now()), "eastmoney"); source.Status != "degraded" || source.LastFailure == nil {
		t.Fatalf("failed EastMoney primary not observed: %+v", source)
	}
}

func TestSourceHealthDistinguishesPrimaryFailureFromSuccessfulFallback(t *testing.T) {
	tracker := newSourceHealthTracker()
	meta := foundation.SourceMeta{
		Source: "tencent:index", FetchedAt: time.Now(),
		FallbackReason: "新浪资金榜不可用，已切换注入的备用来源",
	}
	tracker.fallback(meta)
	tracker.success(meta)
	if source := sourceByID(t, tracker.snapshot(time.Now()), "sina"); source.Status != "degraded" || source.OK {
		t.Fatalf("failed primary was reported healthy: %+v", source)
	}
	if source := sourceByID(t, tracker.snapshot(time.Now()), "tencent"); source.Status != "available" || !source.OK {
		t.Fatalf("successful fallback was not observed: %+v", source)
	}
}

func TestSourceHealthMarksStaleFallbackWithoutFalseRecovery(t *testing.T) {
	tracker := newSourceHealthTracker()
	meta := foundation.SourceMeta{Source: "tencent:index", FetchedAt: time.Now()}
	tracker.success(meta)
	meta.Stale = true
	meta.FallbackReason = "refresh failed"
	tracker.cacheFailure(meta, "tencent")
	tracker.success(meta)
	entry := sourceByID(t, tracker.snapshot(time.Now()), "tencent")
	if entry.OK || entry.Status != "degraded" || entry.LastSuccess == nil || entry.LastFailure == nil {
		t.Fatalf("cached fallback reported healthy: %+v", entry)
	}
	tracker.success(foundation.SourceMeta{Source: "tencent:index", FetchedAt: time.Now().Add(time.Millisecond)})
	if entry := sourceByID(t, tracker.snapshot(time.Now()), "tencent"); !entry.OK || entry.Status != "available" {
		t.Fatalf("fresh fetch did not recover source: %+v", entry)
	}
}
