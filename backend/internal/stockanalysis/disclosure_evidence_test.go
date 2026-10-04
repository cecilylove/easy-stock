package stockanalysis

import (
	"easy-stock/backend/internal/foundation"
	"strings"
	"testing"
	"time"
)

func TestDisclosureUnknownRatingsAndBoundedCoverageAreNotFullInstitutionConsensus(t *testing.T) {
	row := foundation.MarketResearchItem{Title: "研报", Organization: "测试机构", Meta: foundation.SourceMeta{Source: "sina:reports", QueryCoverage: "bounded", FieldsKnown: true, AvailableFields: []string{"title", "organization"}}}
	analysis := analyzeResearch([]foundation.MarketResearchItem{row, row, row, row, row})
	if analysis.Coverage != "有界样本" || !strings.Contains(analysis.Summary, "结构化评级未取得") {
		t.Fatalf("unknown rating/full coverage asserted %+v", analysis)
	}
	row.Rating = "买入" // Present unmasked placeholder must not become rating evidence.
	masked := analyzeResearch([]foundation.MarketResearchItem{row})
	if masked.Score > 50 {
		t.Fatalf("unavailable rating scored %+v", masked)
	}
}
func TestDisclosureScopeAndActualFallbackReachResearchEvidence(t *testing.T) {
	item := foundation.MarketResearchItem{ID: "r1", Title: "第三方研报", Content: "观点与预测内容", PublishedAt: time.Now(), URL: "https://stock.finance.sina.com.cn/report", ContentStatus: "truncated", ContentScope: "platform-readable", ContentIssue: "不等于PDF全文", Meta: foundation.SourceMeta{Source: "sina:reports", FieldsKnown: true, FallbackReason: "有界列表，可能遗漏"}}
	value := ResearchItemSource(item, "opinion", time.Now())
	if value.Provider != "sina:reports" || !strings.Contains(value.Content, "platform-readable") || !strings.Contains(value.Content, "有界列表") || !strings.Contains(value.Content, "available_fields") {
		t.Fatalf("evidence loses boundaries %+v", value)
	}
}
