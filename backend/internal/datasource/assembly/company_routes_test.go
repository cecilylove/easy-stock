package assembly_test

import (
	"context"
	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
	"testing"
)

type companySlots struct{}

func (companySlots) StockBusinessProfile(context.Context, string) (foundation.StockBusinessProfile, error) {
	return foundation.StockBusinessProfile{}, nil
}
func (companySlots) StockFundamentals(context.Context, string) (foundation.StockFundamentals, error) {
	return foundation.StockFundamentals{}, nil
}
func TestCompanyPrimaryAndFallbackAreExplicitIndependentSlots(t *testing.T) {
	sources := assembly.Default("")
	routes := assembly.DefaultRoutes()
	if routes.Business != "sina" || routes.Fundamentals != "sina" || routes.BusinessFallback != "eastmoney" || routes.FundamentalsFallback != "eastmoney" {
		t.Fatalf("wrong company defaults %+v", routes)
	}
	if err := routes.Validate(sources); err != nil {
		t.Fatal(err)
	}
	only, _ := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "company", Name: "Company", Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Business: companySlots{}, Fundamentals: companySlots{}}})
	if err := (assembly.Routes{Business: "company", Fundamentals: "company"}).Validate(only); err != nil {
		t.Fatal("empty fallback should disable it", err)
	}
	if err := (assembly.Routes{Business: "company", FundamentalsFallback: "missing"}).Validate(only); err == nil {
		t.Fatal("dangling fallback accepted")
	}
	if err := (assembly.Routes{Fundamentals: "company", BusinessFallback: "missing"}).Validate(only); err == nil {
		t.Fatal("dangling business fallback accepted")
	}
}
