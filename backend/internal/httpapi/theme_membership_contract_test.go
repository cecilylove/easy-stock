package httpapi

import (
	"easy-stock/backend/internal/foundation"
	"testing"
)

func TestThemeScreenPhaseAndMembershipCompletenessAreIndependent(t *testing.T) {
	ref := foundation.BoardRef{Provider: "tencent", NativeCode: "pt01801152", Dimension: "industry"}
	makeMap := func(nodes ...foundation.SectorMapNode) foundation.SectorMap {
		return foundation.SectorMap{Groups: []foundation.SectorMapGroup{{Nodes: nodes}}}
	}
	native := foundation.SectorMapNode{MemberSet: &foundation.MemberSetMeta{Kind: "native", Complete: true, BoardRef: ref}}
	if complete, scope := themeMembershipCoverage(makeMap(native)); !complete || scope != "native_complete" {
		t.Fatalf("native coverage wrong %v %s", complete, scope)
	}
	candidate := foundation.SectorMapNode{MemberSet: &foundation.MemberSetMeta{Kind: "candidate", Returned: 10, BoardRef: ref}}
	if complete, scope := themeMembershipCoverage(makeMap(candidate)); complete || scope != "candidate" {
		t.Fatalf("candidate called complete %v %s", complete, scope)
	}
	if complete, scope := themeMembershipCoverage(makeMap(native, candidate)); complete || scope != "mixed" {
		t.Fatalf("mixed called complete %v %s", complete, scope)
	}
	if complete, scope := themeMembershipCoverage(makeMap(foundation.SectorMapNode{})); complete || scope != "unknown" {
		t.Fatalf("unknown called complete %v %s", complete, scope)
	}
	if complete, scope := themeMembershipCoverage(makeMap()); complete || scope != "unknown" {
		t.Fatalf("empty called complete %v %s", complete, scope)
	}
}
