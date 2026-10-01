package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type fixedHotStockProvider struct{}

func (fixedHotStockProvider) HotStockRanks(context.Context, int) []foundation.HotStockRankList {
	fetchedAt := time.Date(2026, 8, 19, 10, 0, 0, 0, time.Local)
	return []foundation.HotStockRankList{
		{Source: "ths", SourceName: "同花顺", FetchedAt: fetchedAt, Items: []foundation.HotStockRankItem{
			{Symbol: "600519.SH", Name: "贵州茅台", Rank: 1},
			{Symbol: "000001.SZ", Name: "平安银行", Rank: 2},
		}},
		{Source: "eastmoney", SourceName: "东方财富", FetchedAt: fetchedAt, Items: []foundation.HotStockRankItem{
			{Symbol: "300750.SZ", Rank: 1},
			{Symbol: "600519.SH", Rank: 20},
		}},
	}
}

func TestHotStockRanksHandlerBuildsDeduplicatedConsensusUnion(t *testing.T) {
	server := NewServer(Config{HotStocks: fixedHotStockProvider{}, StockDirectory: &countingStockDirectoryProvider{}})
	defer server.Close()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/stocks/hot-ranks", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data hotStockRankData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.Total != 3 || len(payload.Data.Stocks) != 3 {
		t.Fatalf("unexpected union: %+v", payload.Data)
	}
	first := payload.Data.Stocks[0]
	if first.Symbol != "600519.SH" || first.SourceCount != 2 || first.Ranks["ths"] != 1 || first.Ranks["eastmoney"] != 20 {
		t.Fatalf("unexpected consensus leader: %+v", first)
	}
	if len(payload.Data.Sources) != 2 || !payload.Data.Sources[0].Available || payload.Data.Sources[0].Count != 2 {
		t.Fatalf("source status missing: %+v", payload.Data.Sources)
	}
}

type observedHotStockProvider struct {
	calls atomic.Int32
	load  func(context.Context) []foundation.HotStockRankList
}

func (p *observedHotStockProvider) HotStockRanks(ctx context.Context, _ int) []foundation.HotStockRankList {
	p.calls.Add(1)
	return p.load(ctx)
}

func requestHotStockRanks(t *testing.T, server *Server, path string, ctx context.Context) hotStockRankData {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
	if recorder.Code != http.StatusOK {
		t.Fatalf("hot ranks status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data hotStockRankData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data
}

func successfulHotStockList(source string, at time.Time) foundation.HotStockRankList {
	return foundation.HotStockRankList{
		Source: source, FetchedAt: at,
		Items: []foundation.HotStockRankItem{{Symbol: "600519.SH", Rank: 1}},
	}
}

func TestHotStockRanksObservesFreshSourcesWithoutRenewingFromCache(t *testing.T) {
	fetchedAt := time.Now()
	provider := &observedHotStockProvider{load: func(context.Context) []foundation.HotStockRankList {
		return []foundation.HotStockRankList{successfulHotStockList("ths", fetchedAt), successfulHotStockList("cffex", fetchedAt)}
	}}
	server := NewServer(Config{HotStocks: provider, StockDirectory: &countingStockDirectoryProvider{}})
	defer server.Close()
	requestHotStockRanks(t, server, "/api/v1/stocks/hot-ranks", context.Background())
	for _, id := range []string{"ths", "cffex"} {
		source := sourceByID(t, server.sourceHealth.snapshot(time.Now()), id)
		if !source.OK || source.Status != "available" || source.CheckedAt == nil || !source.CheckedAt.Equal(fetchedAt) {
			t.Fatalf("fresh %s result not observed: %+v", id, source)
		}
	}
	requestHotStockRanks(t, server, "/api/v1/stocks/hot-ranks", context.Background())
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("cached request contacted provider %d times", calls)
	}
	for _, id := range []string{"ths", "cffex"} {
		source := sourceByID(t, server.sourceHealth.snapshot(fetchedAt.Add(sourceObservationTTL+time.Second)), id)
		if source.Status != "unknown" || source.CheckedAt == nil || !source.CheckedAt.Equal(fetchedAt) {
			t.Fatalf("cache renewed %s observation: %+v", id, source)
		}
	}
}

func TestHotStockRanksPartialFailureKeepsHealthySourceAndMarksFailedSource(t *testing.T) {
	provider := &observedHotStockProvider{load: func(context.Context) []foundation.HotStockRankList {
		return []foundation.HotStockRankList{
			successfulHotStockList("ths", time.Now()),
			{Source: "cffex", Error: "failed https://upstream/?token=private"},
		}
	}}
	server := NewServer(Config{HotStocks: provider, StockDirectory: &countingStockDirectoryProvider{}})
	defer server.Close()
	data := requestHotStockRanks(t, server, "/api/v1/stocks/hot-ranks", context.Background())
	if data.Total != 1 || data.Stale || len(data.Sources) != 2 || !data.Sources[0].Available || data.Sources[1].Available {
		t.Fatalf("partial union was lost: %+v", data)
	}
	if source := sourceByID(t, server.sourceHealth.snapshot(time.Now()), "ths"); source.Status != "available" {
		t.Fatalf("healthy source was blamed: %+v", source)
	}
	failed := sourceByID(t, server.sourceHealth.snapshot(time.Now()), "cffex")
	if failed.Status != "degraded" || failed.LastFailure == nil || strings.Contains(failed.Message, "private") {
		t.Fatalf("failure was missing or leaked secrets: %+v", failed)
	}
}

func TestHotStockRanksFailedRefreshObservesFailuresWhileReturningOldUnion(t *testing.T) {
	var fail atomic.Bool
	provider := &observedHotStockProvider{load: func(context.Context) []foundation.HotStockRankList {
		if fail.Load() {
			return []foundation.HotStockRankList{{Source: "ths", Error: "unavailable"}, {Source: "cffex"}}
		}
		return []foundation.HotStockRankList{successfulHotStockList("ths", time.Now()), successfulHotStockList("cffex", time.Now())}
	}}
	server := NewServer(Config{HotStocks: provider, StockDirectory: &countingStockDirectoryProvider{}})
	defer server.Close()
	requestHotStockRanks(t, server, "/api/v1/stocks/hot-ranks", context.Background())
	fail.Store(true)
	data := requestHotStockRanks(t, server, "/api/v1/stocks/hot-ranks?refresh=1", context.Background())
	if !data.Stale || data.Total != 1 {
		t.Fatalf("last useful snapshot was lost: %+v", data)
	}
	for _, id := range []string{"ths", "cffex"} {
		source := sourceByID(t, server.sourceHealth.snapshot(time.Now()), id)
		if source.Status != "degraded" || source.LastSuccess == nil || source.LastFailure == nil {
			t.Fatalf("stale union hid %s failure: %+v", id, source)
		}
	}
}

func TestHotStockRanksCallerCancellationDoesNotBlameSources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := &observedHotStockProvider{load: func(ctx context.Context) []foundation.HotStockRankList {
		return []foundation.HotStockRankList{{Source: "ths", Error: ctx.Err().Error()}, {Source: "cffex", Error: ctx.Err().Error()}}
	}}
	server := NewServer(Config{HotStocks: provider, StockDirectory: &countingStockDirectoryProvider{}})
	defer server.Close()
	requestHotStockRanks(t, server, "/api/v1/stocks/hot-ranks", ctx)
	for _, id := range []string{"ths", "cffex"} {
		source := sourceByID(t, server.sourceHealth.snapshot(time.Now()), id)
		if source.Status != "unknown" || source.CheckedAt != nil || source.LastFailure != nil {
			t.Fatalf("caller cancellation blamed %s: %+v", id, source)
		}
	}
}

func TestHotStockRanksObservesServiceDeadlineAndSameSourcePartialFailure(t *testing.T) {
	server := NewServer(Config{HotStocks: fixedHotStockProvider{}})
	defer server.Close()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	server.observeHotStockRanks(ctx, []foundation.HotStockRankList{{Source: "ths", Error: "timeout"}})
	if source := sourceByID(t, server.sourceHealth.snapshot(time.Now()), "ths"); source.Status != "degraded" {
		t.Fatalf("service deadline not observed: %+v", source)
	}
	partial := successfulHotStockList("cffex", time.Now())
	partial.Error = "remaining ranks unavailable"
	server.observeHotStockRanks(context.Background(), []foundation.HotStockRankList{partial})
	if source := sourceByID(t, server.sourceHealth.snapshot(time.Now()), "cffex"); source.Status != "degraded" || source.LastSuccess == nil || source.LastFailure == nil {
		t.Fatalf("partial success concealed failure: %+v", source)
	}
}
