package assembly_test

import (
	"context"
	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
	"testing"
)

type reportsOnly struct{}

func (reportsOnly) MarketReports(context.Context, string, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func TestRemovedSourceAndMissingCapabilityFailBeforeActivation(t *testing.T) {
	r, err := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "replacement", Name: "Replacement", Implemented: true, Enabled: true}, Capabilities: registry.Capabilities{Reports: reportsOnly{}}})
	if err != nil {
		t.Fatal(err)
	}
	route := assembly.Routes{Reports: "replacement"}
	if err := route.Validate(r); err != nil {
		t.Fatal(err)
	}
	route.Announcements = "replacement"
	if err := route.Validate(r); err == nil {
		t.Fatal("interface implementation silently claimed another ability")
	}
	route.Announcements = ""
	removed, _ := registry.New()
	if err := route.Validate(removed); err == nil {
		t.Fatal("removed supplier still enabled in route")
	}
	if err := assembly.DefaultRoutes().Validate(assembly.Default("")); err != nil {
		t.Fatal(err)
	}
}
