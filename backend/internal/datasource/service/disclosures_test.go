package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"errors"
	"strings"
	"testing"
	"time"
)

type disclosureFixture struct {
	items []foundation.MarketResearchItem
	meta  foundation.SourceMeta
	err   error
	calls int
	wait  bool
}

func (f *disclosureFixture) fetch(ctx context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	f.calls++
	if f.wait {
		<-ctx.Done()
		return f.items, f.meta, ctx.Err()
	}
	return cloneResearchItems(f.items), foundation.CloneSourceMeta(f.meta), f.err
}
func (f *disclosureFixture) MarketAnnouncements(ctx context.Context, _, _, _ string, _ int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return f.fetch(ctx)
}
func (f *disclosureFixture) MarketReports(ctx context.Context, _, _, _, _ string, _ int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return f.fetch(ctx)
}
func disclosureRow(source string) foundation.MarketResearchItem {
	return foundation.MarketResearchItem{ID: "r1", Kind: "stock", Title: "真实研报", URL: "https://source.example/r1", PublishedAt: time.Now().Add(-time.Hour), Meta: foundation.SourceMeta{Source: source, FieldsKnown: true, AvailableFields: []string{"title", "published_at"}}}
}
func newDisclosureMarket(p, b *disclosureFixture) *Market {
	return NewMarket(MarketConfig{Announcements: p, AnnouncementsFallback: b, Reports: p, ReportsFallback: b, AnnouncementsSourceID: "sina", ReportsSourceID: "sina", AnnouncementsFallbackSourceID: "eastmoney", ReportsFallbackSourceID: "eastmoney"})
}
func TestDisclosureCompleteAndValidatedEmptyDoNotRequestFallback(t *testing.T) {
	for _, empty := range []bool{false, true} {
		p := &disclosureFixture{items: []foundation.MarketResearchItem{disclosureRow("sina:reports")}, meta: foundation.SourceMeta{Source: "sina:reports", QueryCoverage: "complete"}}
		if empty {
			p.items = []foundation.MarketResearchItem{}
		}
		b := &disclosureFixture{}
		items, _, err := newDisclosureMarket(p, b).MarketReports(context.Background(), "stock", "", "600519.SH", "", 5)
		if err != nil || b.calls != 0 || (!empty && len(items) != 1) {
			t.Fatalf("primary not authoritative %+v %v", items, err)
		}
	}
}
func TestDisclosureUnsupportedOrBoundedQueryFallsBackWholeList(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		p := &disclosureFixture{items: []foundation.MarketResearchItem{disclosureRow("sina:reports")}, meta: foundation.SourceMeta{Source: "sina:reports", Partial: true, QueryCoverage: "bounded"}}
		if unsupported {
			p.err = &contracts.Error{Kind: contracts.Unsupported}
		}
		b := &disclosureFixture{items: []foundation.MarketResearchItem{disclosureRow("eastmoney:reports")}, meta: foundation.SourceMeta{Source: "eastmoney:reports"}}
		rows, meta, err := newDisclosureMarket(p, b).MarketReports(context.Background(), "industry", "", "", "old-native", 5)
		if err != nil || b.calls != 1 || len(rows) != 1 || rows[0].Meta.Source != "eastmoney:reports" || !strings.Contains(meta.FallbackReason, "整份") {
			t.Fatalf("fallback mixed %+v %+v %v", rows, meta, err)
		}
	}
}
func TestDisclosureBodyFailureIsNotQueryFailureAndPartialSurvivesBackupFailure(t *testing.T) {
	p := &disclosureFixture{items: []foundation.MarketResearchItem{disclosureRow("sina:announcements")}, meta: foundation.SourceMeta{Source: "sina:announcements", Partial: true, QueryCoverage: "complete"}}
	p.items[0].ContentStatus = "unavailable"
	b := &disclosureFixture{err: errors.New("offline")}
	rows, meta, err := newDisclosureMarket(p, b).MarketAnnouncements(context.Background(), "", "600519.SH", "all", 3)
	if err != nil || b.calls != 0 || rows[0].ContentStatus != "unavailable" || !meta.Partial {
		t.Fatalf("body failure invalidated list %+v %v", rows, err)
	}
	p.meta.QueryCoverage = "bounded"
	rows, meta, err = newDisclosureMarket(p, b).MarketAnnouncements(context.Background(), "filter", "600519.SH", "all", 3)
	if err != nil || rows[0].Meta.Source != "sina:announcements" || !meta.Partial || b.calls != 1 {
		t.Fatalf("partial results dropped %+v %v", rows, err)
	}
}
func TestDisclosureLaterPageFailurePreservesVerifiedPrimaryRowsWhenBackupFails(t *testing.T) {
	p := &disclosureFixture{items: []foundation.MarketResearchItem{disclosureRow("sina:reports")}, meta: foundation.SourceMeta{Source: "sina:reports", QueryCoverage: "bounded", Partial: true}, err: &contracts.Error{Kind: contracts.UpstreamFailure, Cause: errors.New("page2 EOF")}}
	b := &disclosureFixture{err: errors.New("backup offline")}
	rows, meta, err := newDisclosureMarket(p, b).MarketReports(context.Background(), "stock", "", "600519.SH", "", 50)
	if err != nil || len(rows) != 1 || rows[0].Meta.Source != "sina:reports" || !meta.Partial || b.calls != 1 {
		t.Fatalf("valid first page dropped %+v %+v %v", rows, meta, err)
	}
}
func TestDisclosureCancelAndMissingCapabilitiesNeverRestoreDefaults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, b := &disclosureFixture{}, &disclosureFixture{}
	_, _, err := newDisclosureMarket(p, b).MarketReports(ctx, "stock", "", "", "", 5)
	if !errors.Is(err, context.Canceled) || p.calls != 0 || b.calls != 0 {
		t.Fatalf("cancel fetched %v", err)
	}
	_, _, err = NewMarket(MarketConfig{}).MarketReports(context.Background(), "stock", "", "", "", 5)
	if contracts.Kind(err) != contracts.Unsupported {
		t.Fatal(err)
	}
}
func TestDisclosureUnknownRequiredIdentityNotPublished(t *testing.T) {
	p := &disclosureFixture{items: []foundation.MarketResearchItem{{Title: "missing date", Meta: foundation.SourceMeta{FieldsKnown: true}}}}
	_, _, err := NewMarket(MarketConfig{Reports: p}).MarketReports(context.Background(), "stock", "", "", "", 5)
	if err == nil {
		t.Fatal("invalid masked list accepted")
	}
}
