package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
)

// Explicit live check: real public Sina company data, deterministic unrelated
// market fixtures, in-memory stores, no model/key/settings/login dependencies.
func TestLiveSinaCompanyMigrationReachesHTTPResearchAndHolding(t *testing.T) {
	if os.Getenv("A_STOCK_LIVE_COMPANY_TEST") != "1" {
		t.Skip("explicit public company-source verification only")
	}
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	var mu sync.Mutex
	var next time.Time
	calls := []string{}
	http.DefaultTransport = companyLiveTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Host != "quotes.sina.cn" && r.URL.Host != "vip.stock.finance.sina.com.cn" {
			return nil, fmt.Errorf("unexpected external dependency: %s", r.URL.Host)
		}
		if len(calls) >= 12 {
			return nil, fmt.Errorf("live company sample budget exhausted")
		}
		if delay := time.Until(next); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-r.Context().Done():
				timer.Stop()
				return nil, r.Context().Err()
			case <-timer.C:
			}
		}
		response, err := original.RoundTrip(r)
		next = time.Now().Add(1200 * time.Millisecond)
		calls = append(calls, r.URL.Host+r.URL.Path)
		return response, err
	})
	sources := assembly.Default("")
	entries := sources.Entries()
	for i := range entries {
		if entries[i].Descriptor.ID == "sina" {
			entries[i].Capabilities.KLine = companyRoutePrice{}
		}
	}
	sources, err := registry.New(entries...)
	if err != nil {
		t.Fatal(err)
	}
	routes := assembly.DefaultRoutes()
	routes.KLine = []string{"sina"}
	s := NewServer(Config{DataSources: sources, DataSourceRoutes: &routes, Realtime: stockAnalysisRealtime{}, KLinePrimary: companyRoutePrice{}, LimitUp: stockAnalysisLimitUps{}, StockConcept: stockAnalysisCatalog{}, StockDirectory: stockAnalysisCatalog{}, SectorMap: fakeSectorMapProvider{}, ThemeOverview: stockAnalysisThemes{}, News: stockAnalysisNews{}, MarketOverview: &fakeMarketOverviewProvider{}, ReviewDBPath: ":memory:", PortfolioDBPath: ":memory:", StockResearchDBPath: ":memory:"})
	defer s.Close()
	if err := s.StartupError(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/ai-analysis", strings.NewReader(`{"symbol":"600519.SH","mode":"quick"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("HTTP=%d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data stockanalysis.Analysis `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	assertCompanyLiveAnalysis(t, payload.Data)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	analysis, snapshot, err := s.collectStockResearch(ctx, "600519.SH")
	if err != nil {
		t.Fatal(err)
	}
	assertCompanyLiveAnalysis(t, analysis)
	foundFinancial, foundBusiness := false, false
	for _, source := range snapshot.Sources {
		if source.ID == "f-financial" {
			foundFinancial = source.Provider == "sina:financials" && source.TimeStatus == "dated" && !source.PublishedAt.IsZero()
		}
		if source.ID == "f-business" {
			foundBusiness = source.Provider == "sina:business" && source.URL != ""
		}
	}
	if !foundFinancial || !foundBusiness {
		t.Fatalf("source evidence not connected %+v", snapshot.Sources)
	}
	holding, err := s.analyzeHoldingResearch(ctx, portfolioinspection.Holding{Symbol: "600519.SH"})
	if err != nil {
		t.Fatal(err)
	}
	assertCompanyLiveAnalysis(t, holding)
	for _, source := range []string{payload.Data.Fundamental.Source, analysis.Fundamental.Source, holding.Fundamental.Source} {
		if source != "sina:financials" {
			t.Fatal(source)
		}
	}
	mu.Lock()
	count := len(calls)
	mu.Unlock()
	t.Logf("official company requests=%d; HTTP quick, research snapshot and holding shared analyzer connected; synthetic price fixtures are not a market-data validation", count)
	if dir := os.Getenv("A_STOCK_COMPANY_EVIDENCE_DIR"); dir != "" {
		record := map[string]any{"at": time.Now(), "scope": "real Sina company sources; unrelated price/news/theme fixtures; in-memory stores; no AI generation or personal data", "http_fundamental": payload.Data.Fundamental, "research_financial": analysis.Fundamental, "holding_financial": holding.Fundamental, "snapshot_sources": snapshot.Sources, "requests": calls}
		data, _ := json.MarshalIndent(record, "", "  ")
		if err := os.WriteFile(dir+"/http-holding.json", data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

type companyLiveTransport func(*http.Request) (*http.Response, error)

func (f companyLiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func assertCompanyLiveAnalysis(t *testing.T, value stockanalysis.Analysis) {
	t.Helper()
	if value.Fundamental == nil || value.Fundamental.Source != "sina:financials" || value.Fundamental.PublishedAt.IsZero() || !value.Fundamental.FieldsKnown || !value.Fundamental.ScoreAvailable {
		t.Fatalf("actual finance not preserved %+v", value.Fundamental)
	}
	if value.Fundamental.FallbackReason != "" {
		t.Fatalf("unexpected financial fallback %s", value.Fundamental.FallbackReason)
	}
}
