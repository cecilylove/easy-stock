package httpapi

import (
	"context"
	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
	"sync/atomic"
	"testing"
	"time"
)

type companyRoutePrice struct{}

func (companyRoutePrice) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	lines, err := (stockAnalysisKLines{}).KLine(ctx, symbol, period, limit)
	for i := range lines {
		lines[i].Meta.Source = "sina"
		lines[i].Meta.Provider = "sina"
	}
	return lines, err
}

type companyRouteFixture struct {
	source string
	calls  atomic.Int32
}

func (p *companyRouteFixture) StockBusinessProfile(context.Context, string) (foundation.StockBusinessProfile, error) {
	p.calls.Add(1)
	return foundation.StockBusinessProfile{Symbol: "600519.SH", Name: "样本", MainBusiness: "白酒生产与销售", Description: "公司从事白酒生产", Meta: foundation.SourceMeta{Source: p.source + ":business", ExecutionState: "fetched", FetchedAt: time.Now()}}, nil
}
func (p *companyRouteFixture) StockFundamentals(context.Context, string) (foundation.StockFundamentals, error) {
	p.calls.Add(1)
	return foundation.StockFundamentals{Symbol: "600519.SH", ReportDate: "2026-06-30", PublishedAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), Revenue: 100, NetProfit: 10, Meta: foundation.SourceMeta{Source: p.source + ":financials", ExecutionState: "fetched", FetchedAt: time.Now(), FieldsKnown: true, AvailableFields: []string{"revenue", "revenue_yoy", "net_profit", "net_profit_yoy", "deducted_net_profit", "deducted_net_profit_yoy", "eps", "roe", "gross_margin", "debt_ratio", "operating_cash_flow_per_share"}}}, nil
}
func TestCompanyDefaultMigrationAndInjectedConsumerEvidence(t *testing.T) {
	defaults := assembly.DefaultRoutes()
	if defaults.Business != "sina" || defaults.Fundamentals != "sina" || defaults.BusinessFallback != "eastmoney" || defaults.FundamentalsFallback != "eastmoney" {
		t.Fatalf("not migrated %+v", defaults)
	}
	primary, backup := &companyRouteFixture{source: "sina"}, &companyRouteFixture{source: "eastmoney"}
	sources, err := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "sina", Name: "Sina", Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Business: primary, Fundamentals: primary, KLine: companyRoutePrice{}, Realtime: stockAnalysisRealtime{}, Theme: nil, News: stockAnalysisNews{}}}, registry.Entry{Descriptor: registry.Descriptor{ID: "eastmoney", Name: "EM", Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Business: backup, Fundamentals: backup}})
	if err != nil {
		t.Fatal(err)
	}
	routes := assembly.Routes{Business: "sina", Fundamentals: "sina", BusinessFallback: "eastmoney", FundamentalsFallback: "eastmoney", KLine: []string{"sina"}}
	s := NewServer(Config{DataSources: sources, DataSourceRoutes: &routes, Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{}, LimitUp: stockAnalysisLimitUps{}, StockConcept: stockAnalysisCatalog{}, MarketOverview: &fakeMarketOverviewProvider{}, ReviewDBPath: ":memory:"})
	defer s.Close()
	if err := s.StartupError(); err != nil {
		t.Fatal(err)
	}
	analysis, snapshot, runErr := s.collectStockResearch(context.Background(), "600519.SH")
	if runErr != nil {
		t.Fatal(runErr)
	}
	if analysis.Fundamental == nil || analysis.Fundamental.Source != "sina:financials" || backup.calls.Load() != 0 {
		t.Fatalf("consumer reverted to EM %+v backup=%d", analysis.Fundamental, backup.calls.Load())
	}
	found := false
	for _, source := range snapshot.Sources {
		if source.ID == "f-financial" {
			found = source.Provider == "sina:financials" && source.TimeStatus == "dated" && !source.PublishedAt.IsZero()
		}
	}
	if !found {
		t.Fatalf("disclosure evidence missing %+v", snapshot.Sources)
	}
}
