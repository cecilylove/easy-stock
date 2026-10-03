package stockanalysis

import (
	"easy-stock/backend/internal/foundation"
	"strings"
	"testing"
	"time"
)

func TestBusinessEvidenceRoleIsIndependentOfSupplierName(t *testing.T) {
	evidence := ThemeEvidence{Type: "fact", Relation: "own_business", EvidenceRole: "business-profile", Source: "new-vendor:company-data"}
	if !hasF10BusinessEvidence([]ThemeEvidence{evidence}) {
		t.Fatal("new supplier rejected for lacking f10 string")
	}
	evidence.EvidenceRole = "news"
	evidence.Source = "new-vendor:f10-business"
	if hasF10BusinessEvidence([]ThemeEvidence{evidence}) {
		t.Fatal("source name overrode explicit evidence role")
	}
	evidence.EvidenceRole = ""
	evidence.Source = "eastmoney:f10-business"
	if !hasF10BusinessEvidence([]ThemeEvidence{evidence}) {
		t.Fatal("historical role compatibility lost")
	}
}
func TestReportSourcesAndAnnouncementScopeAreActualEvidence(t *testing.T) {
	source := researchReportSources([]foundation.MarketResearchItem{{Meta: foundation.SourceMeta{Source: "new-vendor:reports"}}, {Meta: foundation.SourceMeta{Source: "other:reports"}}})
	if !strings.Contains(source, "new-vendor") || strings.Contains(source, "eastmoney") {
		t.Fatal(source)
	}
	item := ResearchItemSource(foundation.MarketResearchItem{Title: "公司公告", ContentStatus: "unavailable", ContentScope: "list-only", ContentIssue: "未取得正文"}, "announcement", time.Now())
	if !strings.Contains(item.Content, "list-only") || !strings.Contains(item.Content, "未取得正文") {
		t.Fatal(item)
	}
}
