package service

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"errors"
	"strings"
	"testing"
	"time"
)

type companyFixture struct {
	business  foundation.StockBusinessProfile
	financial foundation.StockFundamentals
	err       error
	calls     int
	wait      bool
}

func (p *companyFixture) StockBusinessProfile(ctx context.Context, symbol string) (foundation.StockBusinessProfile, error) {
	p.calls++
	if p.wait {
		<-ctx.Done()
		return p.business, ctx.Err()
	}
	return p.business, p.err
}
func (p *companyFixture) StockFundamentals(ctx context.Context, symbol string) (foundation.StockFundamentals, error) {
	p.calls++
	if p.wait {
		<-ctx.Done()
		return p.financial, ctx.Err()
	}
	return p.financial, p.err
}
func financialFixture(source string) foundation.StockFundamentals {
	return foundation.StockFundamentals{Symbol: "600519.SH", ReportDate: "2026-06-30", ReportName: "中报", PublishedAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), Meta: foundation.SourceMeta{Source: source, FieldsKnown: true, AvailableFields: append([]string(nil), requiredFundamentalFields...), ExecutionState: "fetched", FetchedAt: time.Now()}}
}
func companyWith(p, b *companyFixture) *Company {
	return NewCompany(CompanyConfig{Business: p, BusinessFallback: b, Fundamentals: p, FundamentalsFallback: b, BusinessID: "sina", BusinessFallbackID: "eastmoney", FundamentalsID: "sina", FundamentalsFallbackID: "eastmoney", Now: func() time.Time { return time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) }})
}
func TestCompanyCompletePrimarySkipsFallbackIncludingTrueZero(t *testing.T) {
	p := &companyFixture{financial: financialFixture("sina:financials"), business: foundation.StockBusinessProfile{Symbol: "600519.SH", MainBusiness: "生产白酒", Description: "公司从事白酒生产", Meta: foundation.SourceMeta{Source: "sina:business"}}}
	b := &companyFixture{financial: financialFixture("eastmoney:f10-financials")}
	s := companyWith(p, b)
	value, err := s.StockFundamentals(context.Background(), "600519.SH")
	if err != nil || value.Meta.Source != "sina:financials" || b.calls != 0 || !value.FieldAvailable("revenue") {
		t.Fatalf("zero primary not used %+v %v", value, err)
	}
	profile, err := s.StockBusinessProfile(context.Background(), "600519.SH")
	if err != nil || profile.Meta.Source != "sina:business" || b.calls != 0 {
		t.Fatalf("business fallback used %+v %v", profile, err)
	}
}
func TestCompanyFallbackReplacesWholeSnapshotAndIndependentSources(t *testing.T) {
	p := &companyFixture{financial: financialFixture("sina:financials")}
	p.financial.Meta.AvailableFields = []string{"revenue", "net_profit"}
	p.financial.Revenue = 10
	b := &companyFixture{financial: financialFixture("eastmoney:f10-financials"), business: foundation.StockBusinessProfile{MainBusiness: "生产白酒", Description: "有效简介", Meta: foundation.SourceMeta{Source: "eastmoney:f10-business"}}}
	b.financial.Revenue = 20
	s := companyWith(p, b)
	value, err := s.StockFundamentals(context.Background(), "600519.SH")
	if err != nil || value.Revenue != 20 || value.Meta.Source != "eastmoney:f10-financials" || !strings.Contains(value.Meta.FallbackReason, "整份") {
		t.Fatalf("mixed/fallback identity %+v %v", value, err)
	}
	profile, err := s.StockBusinessProfile(context.Background(), "600519.SH")
	if err != nil || !strings.Contains(profile.Meta.FallbackReason, "回退") {
		t.Fatalf("incomplete business not downgraded %+v %v", profile, err)
	}
}
func TestCompanyPartialPrimaryPreservedWhenFallbackFailsOrOlder(t *testing.T) {
	for _, failure := range []bool{false, true} {
		p := &companyFixture{financial: financialFixture("sina:financials")}
		p.financial.Meta.AvailableFields = []string{"revenue", "net_profit"}
		b := &companyFixture{financial: financialFixture("eastmoney:f10-financials")}
		b.financial.ReportDate = "2026-03-31"
		if failure {
			b.err = errors.New("offline")
		}
		value, err := companyWith(p, b).StockFundamentals(context.Background(), "600519.SH")
		if err != nil || !value.Meta.Partial || value.Meta.Source != "sina:financials" || value.FieldAvailable("roe") {
			t.Fatalf("partial/older fallback mixed %+v %v", value, err)
		}
	}
}
func TestCompanyFinancialNotApplicableAndFutureOrUnsupported(t *testing.T) {
	p := &companyFixture{financial: financialFixture("sina:financials")}
	p.financial.Meta.AvailableFields = p.financial.Meta.AvailableFields[:8]
	p.financial.Meta.AvailableFields = append(p.financial.Meta.AvailableFields, "debt_ratio", "operating_cash_flow_per_share")
	p.financial.NotApplicableFields = []string{"gross_margin"}
	b := &companyFixture{financial: financialFixture("eastmoney")}
	if _, err := companyWith(p, b).StockFundamentals(context.Background(), "600519.SH"); err != nil || b.calls != 0 {
		t.Fatalf("not applicable triggered fallback %v", err)
	}
	p.financial.PublishedAt = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	value, err := companyWith(p, b).StockFundamentals(context.Background(), "600519.SH")
	if err != nil || value.Meta.Source != "eastmoney" {
		t.Fatalf("future publication accepted %+v %v", value, err)
	}
	empty := NewCompany(CompanyConfig{})
	if _, err := empty.StockFundamentals(context.Background(), "600519.SH"); err == nil {
		t.Fatal("disabled revived default")
	}
}
func TestCompanyCancelDoesNotTriggerFallbackAndReserveBudget(t *testing.T) {
	p := &companyFixture{wait: true}
	b := &companyFixture{financial: financialFixture("eastmoney"), business: foundation.StockBusinessProfile{MainBusiness: "有效", Description: "有效"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := companyWith(p, b).StockFundamentals(ctx, "600519.SH"); !errors.Is(err, context.Canceled) || b.calls != 0 || p.calls != 0 {
		t.Fatalf("cancel fetch %v", err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer stop()
	value, err := companyWith(p, b).StockFundamentals(ctx, "600519.SH")
	if err != nil || value.Meta.Source != "eastmoney" || b.calls != 1 {
		t.Fatalf("fallback budget unavailable %+v %v", value, err)
	}
}
func TestCompanyDisabledFallbackAndIndependentBusinessFinancialRoutes(t *testing.T) {
	p := &companyFixture{err: errors.New("primary failed")}
	backup := &companyFixture{financial: financialFixture("backup:financials")}
	s := NewCompany(CompanyConfig{Business: p, Fundamentals: backup, BusinessID: "primary", FundamentalsID: "backup"})
	if _, err := s.StockBusinessProfile(context.Background(), "600519.SH"); err == nil {
		t.Fatal("disabled business fallback revived")
	}
	value, err := s.StockFundamentals(context.Background(), "600519.SH")
	if err != nil || value.Meta.Source != "backup:financials" {
		t.Fatalf("financial route affected by business route %v", err)
	}
}

func TestCompanyObservationsPreservePrimaryFailureAndFallbackSuccess(t *testing.T) {
	p := &companyFixture{err: errors.New("fail")}
	b := &companyFixture{financial: financialFixture("eastmoney:financials")}
	s := companyWith(p, b)
	var seen []foundation.SourceObservation
	s.SetObserver(func(e foundation.SourceObservation) { seen = append(seen, e) })
	_, err := s.StockFundamentals(context.Background(), "600519.SH")
	if err != nil || len(seen) != 2 || seen[0].SourceID != "sina" || !seen[0].Failed || seen[1].SourceID != "eastmoney" || seen[1].Failed {
		t.Fatalf("observations %+v %v", seen, err)
	}
}
