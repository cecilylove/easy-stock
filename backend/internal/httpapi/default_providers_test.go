package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
)

type captureOfflineTransport struct {
	mu    sync.Mutex
	hosts []string
}

func (t *captureOfflineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.hosts = append(t.hosts, r.URL.Hostname())
	t.mu.Unlock()
	return nil, errors.New("isolated test: upstream offline")
}

func TestDefaultServerRestoresEastMoneyCapabilities(t *testing.T) {
	s := NewServer(nil)
	defer s.Close()
	if _, ok := s.kLinePrimary.(*sina.Client); !ok || s.kLinePrimarySourceID != "sina" {
		t.Fatalf("K primary=%T %s", s.kLinePrimary, s.kLinePrimarySourceID)
	}
	if _, ok := s.kLineFallback.(*tencent.PriceKLineClient); !ok || s.kLineFallbackSourceID != "tencent" {
		t.Fatalf("K fallback=%T %s", s.kLineFallback, s.kLineFallbackSourceID)
	}
	entry, _ := s.dataSources.Lookup("eastmoney")
	caps := entry.Capabilities
	for name, provider := range map[string]any{"auction": caps.Auction, "directory": caps.Directory, "business": caps.Business, "concept": caps.Directory, "market-pools": caps.Pools, "limit-up": caps.LimitUp} {
		if _, ok := provider.(*eastmoney.Client); !ok {
			t.Errorf("%s=%T, want EastMoney", name, provider)
		}
	}
	if _, ok := s.kLineStrictTencent.(*tencent.StockKLineClient); !ok {
		t.Fatal("selectable strict Tencent provider missing")
	}
	if s.stockDirectorySourceID != "eastmoney" || s.futuresSourceID != "eastmoney" || s.futuresExchangeSourceID != "cffex" || s.marketIndexSourceID != "tencent" || s.marketIndustrySourceID != "tencent" || s.marketFlowSourceID != "sina" {
		t.Fatal("default source attribution is incorrect")
	}
}

func TestPersistedProviderSnapshotsAreRestoredWithoutModifyingStorage(t *testing.T) {
	for _, source := range []string{"eastmoney:kline", "sina:kline"} {
		t.Run(source, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "themes.db")
			store, err := duanxianxia.OpenStore(path)
			if err != nil {
				t.Fatal(err)
			}
			meta := foundation.SourceMeta{Source: source, FetchedAt: time.Now()}
			overview, _ := json.Marshal(foundation.ThemeProgress{Data: []foundation.ThemeOverview{{Name: "preserved"}}, Meta: meta})
			ladder, _ := json.Marshal(shortTermProgress[limitUpLadderData]{Data: &limitUpLadderData{Meta: meta}})
			if err := store.SaveOverview(context.Background(), overview); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveLadder(context.Background(), ladder); err != nil {
				t.Fatal(err)
			}
			_ = store.Close()
			s := NewServer(Config{ThemeRadarDBPath: path})
			defer s.Close()
			if len(s.themeProgress.value.Data) == 0 || s.limitUpProgress.value.Data == nil || !s.themeProgress.value.Meta.Stale || !s.limitUpProgress.value.Data.Meta.Stale {
				t.Fatalf("cached snapshots were not restored as stale: %+v %+v", s.themeProgress.value, s.limitUpProgress.value)
			}
			storedOverview, err := s.themeRadarStore.LoadOverview(context.Background())
			if err != nil || string(storedOverview) != string(overview) {
				t.Fatal("historical overview was changed")
			}
			storedLadder, err := s.themeRadarStore.LoadLadder(context.Background())
			if err != nil || string(storedLadder) != string(ladder) {
				t.Fatal("historical ladder was changed")
			}
		})
	}
}

func TestDefaultMarketFailureObservationsMatchActualProvider(t *testing.T) {
	for _, test := range []struct{ path, source string }{
		{"/api/v1/market/indexes?scope=core", "tencent"},
		{"/api/v1/market/index-series?id=sse&period=day", "tencent"},
		{"/api/v1/market/industries", "tencent"},
		{"/api/v1/market/flows?dimension=stock", "sina"},
		{"/api/v1/stocks/directory", "eastmoney"},
		{"/api/v1/market/futures-members?contract=IF2610&trade_date=2026-09-30", "cffex"},
	} {
		t.Run(test.path, func(t *testing.T) {
			transport := &captureOfflineTransport{}
			original := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = original })
			s := NewServer(nil)
			defer s.Close()
			response := httptest.NewRecorder()
			s.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != http.StatusBadGateway {
				t.Fatalf("status=%d %s", response.Code, response.Body.String())
			}
			entry := sourceByID(t, s.sourceHealth.snapshot(time.Now()), test.source)
			if entry.Status != "degraded" || entry.LastFailure == nil || len(transport.hosts) == 0 {
				t.Fatalf("entry=%+v hosts=%v", entry, transport.hosts)
			}
		})
	}
}

func TestUnsupportedIndexDoesNotFailProviderObservation(t *testing.T) {
	transport := &captureOfflineTransport{}
	original := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	s := NewServer(nil)
	defer s.Close()
	for _, path := range []string{"/api/v1/market/index-series?id=not-supported&period=day", "/api/v1/market/index-series?id=sse&period=not-supported"} {
		response := httptest.NewRecorder()
		s.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d %s", response.Code, response.Body.String())
		}
	}
	if entry := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "eastmoney"); entry.Status != "unknown" || len(transport.hosts) != 0 {
		t.Fatalf("entry=%+v hosts=%v", entry, transport.hosts)
	}
}
