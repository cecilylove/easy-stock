package registry_test

import (
	"context"
	"reflect"
	"testing"

	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
)

type independentLabels struct{}

func (*independentLabels) Fetch(context.Context, string, string) (map[string]string, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}

type independentReports struct{}

func (independentReports) MarketReports(context.Context, string, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}

func TestImplementedCatalogGeneratedFromSlots(t *testing.T) {
	for _, declared := range [][]string{nil, {}, {"report"}, {"reports"}} {
		r, err := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "custom", Name: "Custom", Enabled: true, Implemented: true, Capabilities: declared}, Capabilities: registry.Capabilities{Reports: independentReports{}, BillboardLabels: &independentLabels{}}})
		if err != nil {
			t.Fatal(err)
		}
		actual := r.Catalog()[0].Capabilities
		if !reflect.DeepEqual(actual, []string{"billboard-labels", "reports"}) {
			t.Fatalf("noncanonical catalog %v", actual)
		}
	}
}

func TestImplementedCatalogRejectsFalseDeclarations(t *testing.T) {
	for _, name := range []string{"news", "billboard", "money-flow", "futures", "sector", "unknown"} {
		_, err := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "custom", Name: "Custom", Enabled: true, Implemented: true, Capabilities: []string{name}}, Capabilities: registry.Capabilities{Reports: independentReports{}}})
		if err == nil {
			t.Fatalf("claimed unsupported capability %s", name)
		}
	}
}

func TestDescriptorOnlyCatalogRemainsCompatible(t *testing.T) {
	r := registry.Default()
	if r == nil || len(r.Catalog()) == 0 {
		t.Fatal("descriptor-only historical catalog blocked")
	}
	for _, entry := range r.Entries() {
		if len(registry.CanonicalCapabilities(entry.Capabilities)) != 0 {
			t.Fatal("descriptor-only unexpectedly gained adapters")
		}
	}
	var typedNil *independentLabels
	if got := registry.CanonicalCapabilities(registry.Capabilities{BillboardLabels: typedNil}); len(got) != 0 {
		t.Fatalf("typed nil advertised: %v", got)
	}
}
