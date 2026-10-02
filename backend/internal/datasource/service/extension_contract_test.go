package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/datasource/service"
	"easy-stock/backend/internal/foundation"
)

// This provider intentionally implements only reports, not a market facade.
type independentReports struct{ calls int }

func (p *independentReports) MarketReports(context.Context, string, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	p.calls++
	return []foundation.MarketResearchItem{{Title: "第三方独立研报"}}, foundation.SourceMeta{Source: "new_vendor", FetchedAt: time.Now()}, nil
}

func TestIndependentCapabilityAndRemovedRoutes(t *testing.T) {
	reports := &independentReports{}
	entry := registry.Entry{Descriptor: registry.Descriptor{ID: "new_vendor", Name: "独立研报来源", Mode: "public", Kinds: []string{"information"}, Capabilities: []string{"report"}, Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Reports: reports}}
	sources, err := registry.New(entry)
	if err != nil {
		t.Fatal(err)
	}
	routes := assembly.Routes{Reports: "new_vendor"}
	if err := routes.Validate(sources); err != nil {
		t.Fatal(err)
	}
	market := service.NewMarket(service.MarketConfig{Reports: assembly.Capabilities(sources, routes.Reports).Reports})
	items, meta, err := market.MarketReports(context.Background(), "stock", "", "", "", 1)
	if err != nil || len(items) != 1 || meta.Source != "new_vendor" || reports.calls != 1 {
		t.Fatalf("independent reports were not used: %+v %+v %v", items, meta, err)
	}
	empty, _ := registry.New()
	if err := routes.Validate(empty); err == nil {
		t.Fatal("removing a supplier left its route valid")
	}
	if err := (assembly.Routes{}).Validate(empty); err != nil {
		t.Fatal(err)
	}
	if err := (assembly.Routes{Index: "new_vendor"}).Validate(sources); err == nil {
		t.Fatal("a report adapter was accepted as an index adapter")
	}
	entry.Descriptor.Enabled = false
	disabled, _ := registry.New(entry)
	if err := routes.Validate(disabled); err == nil {
		t.Fatal("disabled source remained active")
	}

	checks := map[string]func() error{
		"reports": func() error {
			_, _, err := service.NewMarket(service.MarketConfig{}).MarketReports(context.Background(), "", "", "", "", 1)
			return err
		},
		"us-sector":        func() error { _, _, err := market.USSectorMomentum(context.Background(), 1); return err },
		"margin":           func() error { _, _, err := market.MarketMarginSeries(context.Background(), 1); return err },
		"announcements":    func() error { _, _, err := market.MarketAnnouncements(context.Background(), "", "", "", 1); return err },
		"billboard":        func() error { _, _, err := market.MarketBillboard(context.Background(), "", 1); return err },
		"billboard-detail": func() error { _, _, err := market.MarketBillboardDetail(context.Background(), "", "", ""); return err },
		"industry":         func() error { _, _, err := market.IndustryMomentum(context.Background(), 1); return err },
		"fund-flow":        func() error { _, _, err := market.MarketFundFlows(context.Background(), "stock", "", 1); return err },
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if value := recover(); value != nil {
					t.Errorf("removed capability panicked: %v", value)
				}
			}()
			var typed *contracts.Error
			if err := check(); !errors.As(err, &typed) || typed.Kind != contracts.Unsupported {
				t.Fatalf("missing capability error = %v", err)
			}
		})
	}
}

type independentIndex struct {
	calls  int
	failed bool
}

func (p *independentIndex) MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	p.calls++
	if p.failed {
		return nil, foundation.SourceMeta{}, errors.New("new index supplier failed")
	}
	return []foundation.MarketIndexSnapshot{{ID: "sse", Price: 3000}}, foundation.SourceMeta{Source: "new_index:index", FetchedAt: time.Now()}, nil
}

func (p *independentIndex) MarketIndexSeries(context.Context, string, string, int) (foundation.MarketIndexSeries, error) {
	p.calls++
	return foundation.MarketIndexSeries{}, errors.New("new index history failed")
}

func TestIndexReplacementKeepsSourceIdentityAndDoesNotFallback(t *testing.T) {
	provider := &independentIndex{}
	market := service.NewMarket(service.MarketConfig{Index: provider, IndexSourceID: "new_index"})
	_, meta, err := market.MarketIndexes(context.Background(), "core")
	if err != nil || meta.Source != "new_index:index" || len(meta.Observations) != 1 || meta.Observations[0].SourceID != "new_index" {
		t.Fatalf("index replacement %+v: %v", meta, err)
	}
	provider.failed = true
	_, meta, err = market.MarketIndexes(context.Background(), "core")
	if err == nil || provider.calls != 2 {
		t.Fatalf("failed named source used an unexpected chain: %d %v", provider.calls, err)
	}
	if meta.Source != "new_index:index" {
		t.Errorf("failure attributed to a retired/default source: %+v", meta)
	}
	if len(meta.Observations) != 1 || meta.Observations[0].SourceID != "new_index" || !meta.Observations[0].Failed {
		t.Errorf("missing failed attempt of the actual adapter: %+v", meta.Observations)
	}
	_, err = market.MarketIndexSeries(context.Background(), "sse", "day", 1)
	if err == nil || provider.calls != 3 {
		t.Fatalf("index history switched suppliers or lost failure: %d %v", provider.calls, err)
	}
	_, err = market.MarketIndexSeries(context.Background(), "sse", "5", 1)
	if !errors.Is(err, service.ErrUnsupportedIndexSeries) || provider.calls != 3 {
		t.Fatalf("unsupported period fetched from a supplier: %d %v", provider.calls, err)
	}
}

type independentFuturesHistory struct {
	calls int
	fail  bool
	empty bool
	err   error
}

func (p *independentFuturesHistory) Trend(context.Context, string, int) (foundation.MarketFuturesPositionSeries, error) {
	p.calls++
	if p.err != nil {
		return foundation.MarketFuturesPositionSeries{}, p.err
	}
	if p.fail {
		return foundation.MarketFuturesPositionSeries{}, errors.New("history supplier failed")
	}
	series := foundation.MarketFuturesPositionSeries{Variety: "IF", Meta: foundation.SourceMeta{Source: "new_history", FetchedAt: time.Now()}}
	if !p.empty {
		price := 4000.
		series.Rows = []foundation.MarketFuturesPositionRow{{TradeDate: "2026-10-02", SettlePrice: &price}}
	}
	return series, nil
}

type independentFuturesExchange struct {
	calls int
	empty bool
}

func (p *independentFuturesExchange) LatestMembers(context.Context, string, string) (foundation.MarketFuturesMembers, error) {
	p.calls++
	members := foundation.MarketFuturesMembers{ContractCode: "IF2610", TradeDate: "2026-10-02", Meta: foundation.SourceMeta{Source: "new_exchange", FetchedAt: time.Now()}}
	if !p.empty {
		members.Members = []foundation.MarketFuturesMemberRank{{LongPosition: 10, ShortPosition: 8}}
	}
	return members, nil
}

func TestFuturesPrimaryAndExchangeAreIndependentAndEmptyIsNotSuccess(t *testing.T) {
	history, exchange := &independentFuturesHistory{}, &independentFuturesExchange{}
	futures := service.NewFuturesWithSources("new_history", "new_exchange", history, exchange, nil, nil)
	series, err := futures.Trend(context.Background(), "IF", 30)
	if err != nil || series.Meta.Source != "new_history" || exchange.calls != 0 {
		t.Fatalf("successful history invoked exchange: %+v %v", series, err)
	}
	history.fail = true
	series, err = futures.Trend(context.Background(), "IF", 30)
	if err != nil || series.Meta.Source != "new_exchange" || exchange.calls != 1 || len(series.Rows) != 1 {
		t.Fatalf("independent exchange downgrade: %+v %v", series, err)
	}
	if len(series.Meta.Observations) != 2 || series.Meta.Observations[0].SourceID != "new_history" || !series.Meta.Observations[0].Failed || series.Meta.Observations[1].SourceID != "new_exchange" || series.Meta.Observations[1].Failed {
		t.Errorf("lost actual primary and exchange attempts: %+v", series.Meta.Observations)
	}
	if !strings.Contains(series.Meta.FallbackReason, "new_history") || !strings.Contains(series.Meta.FallbackReason, "new_exchange") {
		t.Errorf("replacement source labels were hardcoded: %s", series.Meta.FallbackReason)
	}
	if series.Rows[0].SettlePrice != nil || series.Rows[0].Basis != nil || series.Rows[0].IndexClose != nil || series.Rows[0].LongChange != nil || series.Rows[0].ShortChange != nil {
		t.Fatalf("single-day positions fabricated missing fields: %+v", series.Rows[0])
	}
	history.fail, history.empty = false, true
	series, err = futures.Trend(context.Background(), "IF", 30)
	if err != nil || series.Meta.Source != "new_exchange" || exchange.calls != 2 {
		t.Errorf("empty history falsely claimed success: %+v %v", series, err)
	}
	history.fail, exchange.empty = true, true
	series, err = futures.Trend(context.Background(), "IF", 30)
	if err == nil {
		t.Errorf("empty exchange fabricated a valid zero-position point: %+v", series)
	}
	history.err = context.Canceled
	before := exchange.calls
	_, err = futures.Trend(context.Background(), "IF", 30)
	if !errors.Is(err, context.Canceled) || exchange.calls != before {
		t.Fatalf("canceled history requested a backup: %v, calls %d -> %d", err, before, exchange.calls)
	}
}

type customHistoryIndex struct{ calls int }

func (p *customHistoryIndex) SupportsIndexSeries(id, period string) bool {
	return id == "custom_index" && period == "quarter"
}
func (p *customHistoryIndex) MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, errors.New("not used")
}
func (p *customHistoryIndex) MarketIndexSeries(_ context.Context, id, period string, _ int) (foundation.MarketIndexSeries, error) {
	p.calls++
	return foundation.MarketIndexSeries{Index: foundation.MarketIndexSnapshot{ID: id}, Lines: []foundation.KLine{{Time: time.Now(), Close: 123}}, Meta: foundation.SourceMeta{Source: "new_index", FetchedAt: time.Now()}}, nil
}
func TestIndexSupportComesFromReplacementAdapter(t *testing.T) {
	provider := &customHistoryIndex{}
	market := service.NewMarket(service.MarketConfig{Index: provider, IndexSourceID: "new_index"})
	series, err := market.MarketIndexSeries(context.Background(), "custom_index", "quarter", 1)
	if err != nil || len(series.Lines) != 1 || provider.calls != 1 {
		t.Fatalf("adapter support ignored: %+v %v calls=%d", series, err, provider.calls)
	}
	_, err = market.MarketIndexSeries(context.Background(), "sse", "day", 1)
	if !errors.Is(err, service.ErrUnsupportedIndexSeries) || provider.calls != 1 {
		t.Fatalf("unsupported request reached adapter: %v calls=%d", err, provider.calls)
	}
}

type isolatedIndustry struct {
	calls int
	err   error
	empty bool
}

func (p *isolatedIndustry) IndustryMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	p.calls++
	if p.err != nil || p.empty {
		return nil, foundation.SourceMeta{}, p.err
	}
	return []foundation.MarketIndustryMomentum{{Code: "native-board", Name: "测试行业"}}, foundation.SourceMeta{FetchedAt: time.Now()}, nil
}
func (p *isolatedIndustry) MarketFundFlows(context.Context, string, string, int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	p.calls++
	if p.err != nil || p.empty {
		return nil, foundation.SourceMeta{}, p.err
	}
	return []foundation.MarketFundFlow{{Symbol: "000001.SZ"}}, foundation.SourceMeta{FetchedAt: time.Now()}, nil
}

func TestStandaloneMarketRoutesKeepErrorsAndCancellation(t *testing.T) {
	sentinel := errors.New("independent supplier failed")
	primary, backup := &isolatedIndustry{err: sentinel}, &isolatedIndustry{}
	market := service.NewMarket(service.MarketConfig{Industry: primary, IndustrySourceID: "new_industry", FundFlow: primary, FundFlowSourceID: "new_flow"})
	if _, _, err := market.IndustryMomentum(context.Background(), 1); !errors.Is(err, sentinel) {
		t.Fatalf("standalone industry lost primary error: %v", err)
	}
	if _, _, err := market.MarketFundFlows(context.Background(), "stock", "", 1); !errors.Is(err, sentinel) {
		t.Fatalf("standalone flow lost primary error: %v", err)
	}
	market = service.NewMarket(service.MarketConfig{Industry: primary, IndustrySourceID: "new_industry", IndustryFallback: backup, IndustryFallbackSourceID: "backup_industry", FundFlow: primary, FundFlowSourceID: "new_flow", FundFlowFallback: backup, FundFlowFallbackSourceID: "backup_flow"})
	_, meta, err := market.IndustryMomentum(context.Background(), 1)
	if err != nil || meta.Source != "backup_industry" || len(meta.Observations) != 2 || meta.Observations[1].SourceID != "backup_industry" {
		t.Fatalf("backup industry identity lost: %+v %v", meta, err)
	}
	_, meta, err = market.MarketFundFlows(context.Background(), "stock", "", 1)
	if err != nil || meta.Source != "backup_flow" || len(meta.Observations) != 2 || meta.Observations[1].SourceID != "backup_flow" {
		t.Fatalf("backup flow identity lost: %+v %v", meta, err)
	}
	primary.err = context.Canceled
	before := backup.calls
	if _, meta, err := market.IndustryMomentum(context.Background(), 1); !errors.Is(err, context.Canceled) || len(meta.Observations) != 0 {
		t.Fatalf("cancellation observed as failure: %+v %v", meta, err)
	}
	if _, meta, err := market.MarketFundFlows(context.Background(), "stock", "", 1); !errors.Is(err, context.Canceled) || len(meta.Observations) != 0 {
		t.Fatalf("flow cancellation observed as failure: %+v %v", meta, err)
	}
	if backup.calls != before {
		t.Fatalf("cancellation invoked backup: %d -> %d", before, backup.calls)
	}
}

type cancelingIndex struct{ err error }

func (p cancelingIndex) MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, p.err
}
func (p cancelingIndex) MarketIndexSeries(context.Context, string, string, int) (foundation.MarketIndexSeries, error) {
	return foundation.MarketIndexSeries{}, p.err
}

func TestMarketAndFuturesTypedCancellationStopsFallbackAndObservation(t *testing.T) {
	for _, canceled := range []error{context.Canceled, &contracts.Error{Kind: contracts.Canceled}} {
		primary, backup := &isolatedIndustry{err: canceled}, &isolatedIndustry{}
		market := service.NewMarket(service.MarketConfig{
			Industry: primary, IndustrySourceID: "primary", IndustryFallback: backup, IndustryFallbackSourceID: "backup",
			FundFlow: primary, FundFlowSourceID: "primary", FundFlowFallback: backup, FundFlowFallbackSourceID: "backup",
			Index: cancelingIndex{err: canceled}, IndexSourceID: "primary",
		})
		if _, meta, err := market.IndustryMomentum(context.Background(), 1); !errors.Is(err, context.Canceled) || len(meta.Observations) != 0 {
			t.Fatalf("industry cancellation recorded as failure: %+v %v", meta, err)
		}
		if _, meta, err := market.MarketFundFlows(context.Background(), "stock", "", 1); !errors.Is(err, context.Canceled) || len(meta.Observations) != 0 {
			t.Fatalf("fund-flow cancellation recorded as failure: %+v %v", meta, err)
		}
		if backup.calls != 0 {
			t.Fatalf("supplier cancellation invoked a market backup: %d", backup.calls)
		}
		if _, meta, err := market.MarketIndexes(context.Background(), "core"); !errors.Is(err, context.Canceled) || len(meta.Observations) != 0 {
			t.Fatalf("index snapshot cancellation recorded as failure: %+v %v", meta, err)
		}
		if series, err := market.MarketIndexSeries(context.Background(), "sse", "day", 1); !errors.Is(err, context.Canceled) || len(series.Meta.Observations) != 0 {
			t.Fatalf("index history cancellation recorded as failure: %+v %v", series.Meta, err)
		}
		history, exchange := &independentFuturesHistory{err: canceled}, &independentFuturesExchange{}
		futures := service.NewFuturesWithSources("primary", "backup", history, exchange, nil, nil)
		if series, err := futures.Trend(context.Background(), "IF", 30); !errors.Is(err, context.Canceled) || exchange.calls != 0 || len(series.Meta.Observations) != 0 {
			t.Fatalf("futures cancellation requested a backup or recorded failure: %+v %v calls=%d", series.Meta, err, exchange.calls)
		}
	}
}
