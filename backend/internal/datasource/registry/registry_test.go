package registry_test

import (
	"easy-stock/backend/internal/datasource/registry"
	"testing"
)

func TestRegistryRejectsAmbiguousIdentityAndOwnsDescriptorSnapshot(t *testing.T) {
	d := registry.Descriptor{ID: "new_source", Name: "New supplier", Implemented: true, Enabled: true, Capabilities: []string{"news"}}
	r, err := registry.New(registry.Entry{Descriptor: d})
	if err != nil {
		t.Fatal(err)
	}
	d.Capabilities[0] = "fake-price"
	entry, _ := r.Lookup("new_source")
	entry.Descriptor.Capabilities[0] = "fake-quote"
	if actual, _ := r.Lookup("new_source"); actual.Descriptor.Capabilities[0] != "news" {
		t.Fatal("caller changed live registry")
	}
	if r.SourceID(" new_source:headline ") != "new_source" || r.SourceID("removed") != "" {
		t.Fatal("registered identity not resolved")
	}
	if _, err := registry.New(entry, entry); err == nil {
		t.Fatal("duplicate identities accepted")
	}
	entry.Descriptor.ID = "supplier:news"
	if _, err := registry.New(entry); err == nil {
		t.Fatal("ambiguous identity accepted")
	}
}
