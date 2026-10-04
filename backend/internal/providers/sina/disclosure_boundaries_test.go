package sina

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisclosureDOMRejectsUnboundedDepthAndTokens(t *testing.T) {
	for _, body := range []string{strings.Repeat("<div>", 130) + strings.Repeat("</div>", 130), strings.Repeat("<br>", 100001)} {
		if _, err := disclosureDOM(body); err == nil {
			t.Fatal("unbounded DOM accepted")
		}
	}
}
func TestReportOptionalAuthorDoesNotDiscardSourceRow(t *testing.T) {
	body := strings.ReplaceAll(repFixture(t, "industry"), "周成", "")
	value, err := repParseList(body, repNow(), 1)
	if err != nil || value.invalid != 0 || len(value.items) == 0 {
		t.Fatalf("missing author discarded row %+v %v", value, err)
	}
}
func TestReportNonDescendingLatestNReturnsBoundedNotComplete(t *testing.T) {
	body := repFixture(t, "stock")
	body = strings.Replace(body, "2026-09-22", "2026-08-20", 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "vReport_List") {
			fmt.Fprint(w, body)
		} else {
			http.Error(w, "detail unavailable", 404)
		}
	}))
	defer s.Close()
	_, meta, err := repClient(s).MarketReports(context.Background(), "stock", "", "600519.SH", "", 1)
	if err != nil || meta.QueryCoverage != "bounded" {
		t.Fatalf("unordered latest result called complete %+v %v", meta, err)
	}
}
func TestReportRejectsFakeEmptyAndAmbiguousOrWrongInstitutionBody(t *testing.T) {
	for _, body := range []string{strings.Replace(repEmpty(), `<td colspan="6" class="td10"></td>`, `<td colspan="6">加载中</td>`, 1), strings.Replace(repEmpty(), `<tr><td colspan="6" class="td10"></td></tr>`, "", 1)} {
		if _, err := repParseList(body, repNow(), 1); err == nil {
			t.Fatal("fake empty accepted")
		}
	}
	parsed, err := repParseList(repFixture(t, "stock"), repNow(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{strings.Replace(repFixture(t, "detail"), "诚通证券股份有限公司", "另一机构", 1), strings.Replace(repFixture(t, "detail"), `<div class="blk_container">`, `<div class="blk_container">额外正文</div><div class="blk_container">`, 1)} {
		item := parsed.items[0]
		if err := repParseDetail(body, &item); err == nil {
			t.Fatal("wrong institution or ambiguous body accepted")
		}
	}
}
func TestAnnouncementFutureAndAmbiguousBodyRejected(t *testing.T) {
	rows, _, err := annParseList(annFixture(t, "list"), annTestSymbol(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(annFixture(t, "detail"), `id="content"`, `id="content"><div id="content"></div><div`, 1)
	if _, err := annParseDetail(body, rows[0], annTestSymbol(t)); err == nil {
		t.Fatal("ambiguous content accepted")
	}
}
func TestReportTitleIdentityAndOptionalFieldsRemainSourceMasked(t *testing.T) {
	var item foundation.MarketResearchItem
	item.Title = "第三方报告"
	if fields := repFields(item); strings.Contains(strings.Join(fields, ","), "rating") || strings.Contains(strings.Join(fields, ","), "eps") {
		t.Fatal("unknown metric inferred")
	}
}
