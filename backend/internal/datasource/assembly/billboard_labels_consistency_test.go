package assembly_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
)

type labelsOnly struct{}

func (labelsOnly) Fetch(context.Context, string, string) (map[string]string, foundation.SourceMeta, error) {
	return map[string]string{}, foundation.SourceMeta{}, nil
}

func TestBillboardLabelRouteIndependentAndCustomEmptyDisables(t *testing.T) {
	defaults := assembly.Default("")
	routes := assembly.DefaultRoutes()
	if routes.BillboardLabels != "ths" || routes.Billboard != "eastmoney" {
		t.Fatalf("default labels are not independent: %#v", routes)
	}
	if err := routes.Validate(defaults); err != nil {
		t.Fatal(err)
	}
	ths, ok := defaults.Lookup("ths")
	if !ok || ths.Capabilities.BillboardLabels == nil || ths.Capabilities.Billboard != nil {
		t.Fatal("THS did not register labels independently")
	}
	custom, err := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "custom", Name: "Custom", Enabled: true, Implemented: true}, Capabilities: registry.Capabilities{BillboardLabels: labelsOnly{}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := (assembly.Routes{BillboardLabels: "custom"}).Validate(custom); err != nil {
		t.Fatal(err)
	}
	if err := (assembly.Routes{BillboardLabels: "custom", Billboard: "custom"}).Validate(custom); err == nil {
		t.Fatal("labels implied raw billboard capability")
	}
	var nilLabels *labelsOnly
	nilSource, err := registry.New(registry.Entry{Descriptor: registry.Descriptor{ID: "nil_source", Name: "Nil source", Enabled: true, Implemented: true}, Capabilities: registry.Capabilities{BillboardLabels: nilLabels}})
	if err != nil {
		t.Fatal(err)
	}
	if err := (assembly.Routes{BillboardLabels: "nil_source"}).Validate(nilSource); err == nil {
		t.Fatal("typed nil labels activated a route")
	}
	empty, _ := registry.New()
	if err := (assembly.Routes{}).Validate(empty); err != nil {
		t.Fatal("empty custom route restored defaults", err)
	}
	if err := (assembly.Routes{BillboardLabels: "ths"}).Validate(empty); err == nil {
		t.Fatal("removed label source accepted")
	}
	ths.Descriptor.Enabled = false
	disabled, err := registry.New(ths)
	if err != nil {
		t.Fatal(err)
	}
	if err := (assembly.Routes{BillboardLabels: "ths"}).Validate(disabled); err == nil {
		t.Fatal("disabled label source accepted")
	}
}

func TestDefaultAndContentCatalogExactlyMatchRegisteredSlots(t *testing.T) {
	for _, sources := range []*registry.Registry{assembly.Default(""), assembly.Content(nil, "", "")} {
		for _, entry := range sources.Entries() {
			actual := append([]string(nil), entry.Descriptor.Capabilities...)
			sort.Strings(actual)
			if !reflect.DeepEqual(actual, registry.CanonicalCapabilities(entry.Capabilities)) {
				t.Fatalf("%s catalog does not match slots: %v", entry.Descriptor.ID, actual)
			}
		}
	}
	content := assembly.Content(nil, "", "")
	xueqiu, _ := content.Lookup("xueqiu")
	if xueqiu.Capabilities.AuthorLinks != nil {
		t.Fatal("unsupported Xueqiu author discovery registered")
	}
	taoguba, _ := content.Lookup("taoguba")
	if taoguba.Capabilities.AuthorLinks == nil {
		t.Fatal("Taoguba author discovery lost")
	}
	wechat, _ := content.Lookup("wechat")
	if wechat.Capabilities.AuthorizedArticle == nil || wechat.Capabilities.AuthorLinks != nil {
		t.Fatal("Wechat actual slots changed")
	}
	cffex, _ := assembly.Default("").Lookup("cffex")
	if cffex.Capabilities.FuturesTrend != nil || cffex.Capabilities.FuturesSnapshot == nil {
		t.Fatal("single-day exchange claimed historical trend")
	}
}
