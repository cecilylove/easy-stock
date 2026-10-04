package httpapi

import (
	"context"
	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type disclosureRouteFixture struct {
	id    string
	err   error
	calls atomic.Int32
}

func (p *disclosureRouteFixture) row(kind string) foundation.MarketResearchItem {
	return foundation.MarketResearchItem{Kind: kind, ID: "sample", Title: "明确公司公告", Symbol: "600519.SH", PublishedAt: time.Now().Add(-time.Hour), URL: "https://source.example/sample", Content: "官方披露内容", ContentStatus: "available", ContentScope: "readable-text", Meta: foundation.SourceMeta{Source: p.id, FieldsKnown: true, AvailableFields: []string{"id", "title", "symbol", "published_at", "content"}, ExecutionState: "fetched"}}
}
func (p *disclosureRouteFixture) MarketAnnouncements(context.Context, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	p.calls.Add(1)
	return []foundation.MarketResearchItem{p.row("announcement")}, foundation.SourceMeta{Source: p.id, ExecutionState: "fetched", QueryCoverage: "complete"}, p.err
}
func (p *disclosureRouteFixture) MarketReports(context.Context, string, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	p.calls.Add(1)
	return []foundation.MarketResearchItem{p.row("stock")}, foundation.SourceMeta{Source: p.id, ExecutionState: "fetched", QueryCoverage: "complete"}, p.err
}
func TestDisclosureRegisteredSlotsReachHTTPAndFallbackSourceNotSina(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "primary", true: "fallback"}[fallback], func(t *testing.T) {
			p, b := &disclosureRouteFixture{id: "sina:reports"}, &disclosureRouteFixture{id: "eastmoney:reports"}
			if fallback {
				p.err = &contracts.Error{Kind: contracts.Unsupported}
			}
			sources, _ := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "sina", Name: "Sina", Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Announcements: p, Reports: p, KLine: companyRoutePrice{}}}, registry.Entry{Descriptor: registry.Descriptor{ID: "eastmoney", Name: "EM", Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Announcements: b, Reports: b}})
			routes := assembly.Routes{Announcements: "sina", Reports: "sina", AnnouncementsFallback: "eastmoney", ReportsFallback: "eastmoney", KLine: []string{"sina"}}
			s := NewServer(Config{DataSources: sources, DataSourceRoutes: &routes, ReviewDBPath: ":memory:", Realtime: stockAnalysisRealtime{}, KLinePrimary: companyRoutePrice{}, StockBusiness: stockAnalysisBusiness{}, StockConcept: stockAnalysisCatalog{}, StockDirectory: stockAnalysisCatalog{}, LimitUp: stockAnalysisLimitUps{}, ThemeOverview: stockAnalysisThemes{}, News: stockAnalysisNews{}})
			defer s.Close()
			if err := s.StartupError(); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/api/v1/research/announcements?symbol=600519.SH", "/api/v1/research/institution-reports?symbol=600519.SH"} {
				rec := httptest.NewRecorder()
				s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				if rec.Code != 200 {
					t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
				}
				var value struct {
					Data []foundation.MarketResearchItem `json:"data"`
					Meta foundation.SourceMeta           `json:"meta"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
					t.Fatal(err)
				}
				expected := "sina:reports"
				if fallback {
					expected = "eastmoney:reports"
				}
				if len(value.Data) != 1 || value.Meta.Source != expected || value.Data[0].Meta.Source != expected {
					t.Fatalf("source lost %+v", value)
				}
				if fallback && value.Meta.FallbackReason == "" {
					t.Fatal("fallback invisible")
				}
			}
			_, snapshot, err := s.collectStockResearch(context.Background(), "600519.SH")
			if err != nil {
				t.Fatal(err)
			}
			evidenceSeen := false
			expectedSource := "sina:reports"
			if fallback {
				expectedSource = "eastmoney:reports"
			}
			for _, source := range snapshot.Sources {
				if source.Kind == "opinion" {
					evidenceSeen = source.Provider == expectedSource
				}
			}
			if !evidenceSeen {
				t.Fatalf("registered disclosure not consumed by stock research %+v", snapshot.Sources)
			}
			if !fallback && b.calls.Load() != 0 {
				t.Fatal("complete primary requested backup")
			}
		})
	}
}
