package httpapi

import (
	"context"
	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

type replacementNews struct{ calls int }

func (p *replacementNews) LatestNews(context.Context, int) ([]foundation.NewsItem, error) {
	p.calls++
	return []foundation.NewsItem{{Title: "New supplier headline", Meta: foundation.SourceMeta{Source: "replacement:news", FetchedAt: time.Now()}}}, nil
}

func TestRemovedThemeRouteKeepsPersistedOverviewWithoutRestoringAdapter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "themes.db")
	store, err := duanxianxia.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(foundation.ThemeProgress{Meta: foundation.SourceMeta{Source: "duanxianxia:kaipanla", SnapshotID: "old-snapshot"}})
	if err := store.SaveOverview(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	store.Close()
	empty, _ := registry.New()
	routes := assembly.Routes{}
	s := NewServer(Config{DataSources: empty, ContentSources: empty, DataSourceRoutes: &routes, ThemeRadarDBPath: path})
	if err := s.StartupError(); err != nil {
		t.Fatal(err)
	}
	if s.themeRadarStore == nil || s.themeProgress.value.Meta.SnapshotID != "old-snapshot" || !s.themeProgress.value.Meta.Stale {
		t.Fatalf("removed route lost retained overview: %+v", s.themeProgress.value)
	}
	if len(s.sourceCatalog()) != 0 {
		t.Fatal("empty registration restored defaults")
	}
	s.Close()
	store, err = duanxianxia.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if retained, err := store.LoadOverview(context.Background()); err != nil || len(retained) == 0 {
		t.Fatalf("store changed by removal: %s %v", retained, err)
	}
}

func TestSourceIdentityCannotCollideAcrossRegistries(t *testing.T) {
	entry := registry.Entry{Descriptor: registry.Descriptor{ID: "duplicate", Name: "Duplicate", Enabled: true, Implemented: true}}
	sources, _ := registry.New(entry)
	content, _ := registry.New(entry)
	routes := assembly.Routes{}
	s := NewServer(Config{DataSources: sources, ContentSources: content, DataSourceRoutes: &routes})
	defer s.Close()
	if s.StartupError() == nil {
		t.Fatal("ambiguous supplier catalog activated")
	}
}

type membersOnly struct{ calls int }

func (p *membersOnly) Members(_ context.Context, contract, date string) (foundation.MarketFuturesMembers, error) {
	p.calls++
	return foundation.MarketFuturesMembers{ContractCode: contract, TradeDate: date, Members: []foundation.MarketFuturesMemberRank{{LongName: "Fixture", LongPosition: 10}}, Meta: foundation.SourceMeta{FetchedAt: time.Now()}}, nil
}
func TestFuturesMembersCanBeReplacedWithoutOtherExchangeAbilities(t *testing.T) {
	provider := &membersOnly{}
	sources, err := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "members_only", Name: "Members Only", Enabled: true, Implemented: true, Capabilities: []string{"futures-members"}}, Capabilities: registry.Capabilities{FuturesMembers: provider}})
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := registry.New()
	routes := assembly.Routes{FuturesMembers: "members_only"}
	s := NewServer(Config{DataSources: sources, ContentSources: empty, DataSourceRoutes: &routes})
	defer s.Close()
	if err := s.StartupError(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/market/futures-members?contract=IF2610&trade_date=2026-10-01", nil))
	if response.Code != http.StatusOK || provider.calls != 1 {
		t.Fatalf("status=%d body=%s calls=%d", response.Code, response.Body.String(), provider.calls)
	}
	health := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "members_only")
	if health.Status != "available" || len(health.Capabilities) != 1 {
		t.Fatalf("wrong independent identity: %+v", health)
	}
}
func TestRegisteredNewsSourceFeedsAPIResearchAndIndependentProbe(t *testing.T) {
	provider := &replacementNews{}
	probes := 0
	sources, err := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "replacement", Name: "Replacement", Mode: "public", Kinds: []string{"information"}, Capabilities: []string{"news"}, ProbeScope: "headline", Enabled: true, Implemented: true}, Capabilities: registry.Capabilities{News: provider}, Probe: func(context.Context, time.Time) error { probes++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	routes := assembly.Routes{News: "replacement"}
	emptyContent, _ := registry.New()
	s := NewServer(Config{DataSources: sources, ContentSources: emptyContent, DataSourceRoutes: &routes})
	defer s.Close()
	if err := s.StartupError(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/market/news?source=replacement", nil))
	if response.Code != 200 || provider.calls != 1 {
		t.Fatalf("%d %s calls=%d", response.Code, response.Body.String(), provider.calls)
	}
	// Research uses exactly the same access service, without a page snapshot.
	if _, err := s.newsProvider.LatestNews(context.Background(), 120); err != nil || provider.calls != 2 {
		t.Fatalf("research provider calls=%d err=%v", provider.calls, err)
	}
	health := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "replacement")
	if health.Status != "available" || len(health.Capabilities) != 1 {
		t.Fatalf("unregistered observation: %+v", health)
	}
	if results, _, err := s.sourceProbes.check(context.Background()); err != nil || len(results) != 1 || probes != 1 || provider.calls != 2 {
		t.Fatalf("probe fetched through business route results=%+v err=%v calls=%d", results, err, provider.calls)
	}
	response = httptest.NewRecorder()
	s.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil))
	var body struct {
		Catalog []registry.Descriptor `json:"catalog"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Catalog) != 1 || body.Catalog[0].ID != "replacement" {
		t.Fatalf("catalog=%+v err=%v", body, err)
	}
}

func TestDisabledReportsDoNotObserveUnrelatedMarginSupplier(t *testing.T) {
	margin := &fakeMarketOverviewProvider{}
	sources, err := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "eastmoney", Name: "Margin", Enabled: true, Implemented: true}, Capabilities: registry.Capabilities{Margin: margin}})
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := registry.New()
	routes := assembly.Routes{Margin: "eastmoney"}
	s := NewServer(Config{DataSources: sources, ContentSources: empty, DataSourceRoutes: &routes})
	defer s.Close()
	if err := s.StartupError(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/research/institution-reports", "/api/v1/research/announcements", "/api/v1/market/billboard"} {
		response := httptest.NewRecorder()
		s.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusBadGateway {
			t.Fatalf("%s: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	health := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "eastmoney")
	if health.Status != "unknown" || health.CheckedAt != nil || len(health.Capabilities) != 0 {
		t.Fatalf("unsupported modules marked untouched margin source failed: %+v", health)
	}
}
