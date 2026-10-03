package registry

import "testing"

func TestLegacyDeclarationsMapOnlyToImplementedCanonicalSlots(t *testing.T) {
	cases := []struct {
		alias string
		slots []string
	}{
		{"leaders", []string{"theme"}},
		{"concept", []string{"theme"}}, {"concept", []string{"boards"}}, {"concept", []string{"stock-directory"}},
		{"sector", []string{"industry"}}, {"sector-stocks", []string{"board-members"}},
		{"money-flow", []string{"fund-flow"}}, {"announcement", []string{"announcements"}}, {"report", []string{"reports"}},
		{"futures", []string{"futures-history"}}, {"futures", []string{"futures-snapshot"}},
		{"limit-up", []string{"theme"}}, {"limit-up", []string{"limit-up"}},
	}
	for _, test := range cases {
		if !supportsDeclaration(test.alias, test.slots) {
			t.Fatalf("legacy declaration %s does not map to %v", test.alias, test.slots)
		}
		if supportsDeclaration(test.alias, []string{"news"}) {
			t.Fatalf("legacy declaration %s claims unrelated slot", test.alias)
		}
	}
	if supportsDeclaration("futures-history", []string{"futures-snapshot"}) {
		t.Fatal("single-day snapshot implied history")
	}
	if supportsDeclaration("billboard", []string{"billboard-labels"}) {
		t.Fatal("labels implied raw detail")
	}
}
