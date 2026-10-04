package sina

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func repFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/report_" + name + ".html")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func repNow() time.Time {
	return time.Date(2026, 10, 4, 9, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
}
func repEmpty() string {
	return `<title>研究报告</title><table class="tb_01"><tr><th>序号</th><th>标题</th><th>报告类型</th><th>发布日期</th><th>机构</th><th>研究员</th></tr><tr><td colspan="6" class="td10"></td></tr></table>`
}
func repNoNext(body string) string {
	at := strings.Index(body, `<div id="_function_code_page">`)
	if at < 0 {
		at = strings.Index(body, `<div class="pagebox"`)
	}
	if at >= 0 {
		return body[:at] + `<div id="_function_code_page"><span class="pagebox_next_nolink">下一页</span></div>`
	}
	return body
}
func repKind(t *testing.T, err error, want contracts.ErrorKind) {
	t.Helper()
	if contracts.Kind(err) != want {
		t.Fatalf("kind %q want %q: %v", contracts.Kind(err), want, err)
	}
}
func repClient(s *httptest.Server) *ReportClient {
	return NewReportClient(ReportConfig{BaseURL: s.URL, HTTPClient: s.Client(), Now: repNow})
}
func TestReportOfficialFixtures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		page, want int
	}{{"company", 1, 2}, {"stock", 1, 2}, {"stock_p2", 2, 1}, {"industry", 1, 2}, {"industry_filter", 1, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := repParseList(repFixture(t, tc.name), repNow(), tc.page)
			if err != nil || len(p.items) != tc.want {
				t.Fatalf("%d %v", len(p.items), err)
			}
			for _, i := range p.items {
				if strings.Contains(i.URL, "kind/search") {
					t.Fatal(i.URL)
				}
			}
			if tc.name == "industry_filter" && p.items[0].Kind != "stock" {
				t.Fatal("classification is not industry research")
			}
		})
	}
}
func TestReportStockFilterPaginationAndPlatformText(t *testing.T) {
	stock := repFixture(t, "stock")
	p2 := repFixture(t, "stock_p2")
	detail := repFixture(t, "detail")
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "vReport_List") {
			calls.Add(1)
			if r.URL.Query().Get("symbol") != "600519" || r.URL.Query().Get("t1") != "all" {
				t.Error("filter lost")
			}
			switch r.URL.Query().Get("p") {
			case "":
				fmt.Fprint(w, stock)
			case "2":
				fmt.Fprint(w, p2)
			default:
				t.Error("unexpected page")
				fmt.Fprint(w, repEmpty())
			}
			return
		}
		if strings.Contains(r.URL.Path, "843401040115") {
			fmt.Fprint(w, detail)
		} else {
			http.Error(w, "unavailable", 503)
		}
	}))
	defer s.Close()
	c := NewReportClient(ReportConfig{BaseURL: s.URL, HTTPClient: s.Client(), Now: func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, repNow().Location()) }})
	items, meta, err := c.MarketReports(context.Background(), "stock", "", "600519.SH", "", 8)
	if err != nil || len(items) != 2 || calls.Load() != 2 {
		t.Fatalf("%d %d %v", len(items), calls.Load(), err)
	}
	i := items[0]
	if i.Symbol != "600519.SH" || i.Meta.NativeCode != "600519" || i.Meta.FieldSources["symbol"] != "sina:reports:title" || i.ContentStatus != "available" || i.ContentScope != "platform-readable" || !strings.Contains(i.Content, "EPS") {
		t.Fatalf("%+v", i)
	}
	for _, f := range []string{"rating", "target_low", "target_high", "eps", "pe"} {
		if foundation.FieldAvailable(i.Meta, f) {
			t.Fatal("invented " + f)
		}
	}
	if !i.Meta.FieldsKnown || !foundation.FieldAvailable(i.Meta, "title") || !foundation.FieldAvailable(i.Meta, "content") {
		t.Fatal("masks")
	}
	if !meta.Partial || meta.QueryCoverage != "complete" || meta.Source != "sina:reports" || meta.Provider != "sina" || meta.Capability != "report" || meta.ExecutionState != "fetched" {
		t.Fatalf("%+v", meta)
	}
	if items[1].ContentStatus != "unavailable" || items[1].Title == "" {
		t.Fatal("lost list")
	}
}
func TestReportWindowAndQueryCoverage(t *testing.T) {
	stock := repNoNext(repFixture(t, "stock"))
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "vReport_List") {
			fmt.Fprint(w, stock)
		} else {
			http.Error(w, "none", 503)
		}
	}))
	defer s.Close()
	items, meta, err := repClient(s).MarketReports(context.Background(), "stock", "陈文倩", "600519", "", 8)
	if err != nil || len(items) != 1 || !meta.Partial || meta.QueryCoverage != "bounded" {
		t.Fatalf("%d %+v %v", len(items), meta, err)
	}
	items, meta, err = repClient(s).MarketReports(context.Background(), "stock", "never matched", "600519", "", 8)
	repKind(t, err, contracts.Unsupported)
	if len(items) != 0 || !meta.Partial {
		t.Fatal("query miss is not known empty")
	}
}
func TestReportUnsupportedDoesNotRequest(t *testing.T) {
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer s.Close()
	for _, tc := range []struct{ kind, symbol, industry string }{{"macro", "", ""}, {"industry", "600519", ""}, {"industry", "", "半导体"}, {"stock", "", "sw2_270100"}, {"stock", "000001.SH", ""}, {"stock", "SPY", ""}} {
		_, m, e := repClient(s).MarketReports(context.Background(), tc.kind, "", tc.symbol, tc.industry, 8)
		repKind(t, e, contracts.Unsupported)
		if !m.Partial || m.ExecutionState != "skipped" || m.QueryCoverage != "unsupported" {
			t.Fatalf("%+v", m)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("unsupported network")
	}
	c := NewReportClient(ReportConfig{BaseURL: "https://stock.finance.sina.com.cn.evil.test", Now: repNow})
	_, _, e := c.MarketReports(context.Background(), "stock", "", "", "", 8)
	repKind(t, e, contracts.Unsupported)
}
func TestReportIndustryNeverMixesCompany(t *testing.T) {
	industry := repNoNext(repFixture(t, "industry"))
	company := repNoNext(repFixture(t, "company"))
	mixed := strings.Replace(industry, "</table>", company[strings.Index(company, "<tr><td>1"):strings.Index(company, "</table>")]+"</table>", 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "vReport_List") {
			fmt.Fprint(w, mixed)
		} else {
			http.Error(w, "none", 503)
		}
	}))
	defer s.Close()
	items, m, e := repClient(s).MarketReports(context.Background(), "industry", "", "", "", 8)
	if e != nil || len(items) != 2 || m.QueryCoverage != "bounded" {
		t.Fatalf("%d %+v %v", len(items), m, e)
	}
	for _, i := range items {
		if i.Kind != "industry" || i.Symbol != "" {
			t.Fatal("mixed kind")
		}
	}
}
func TestReportStockFilterMismatchOrEmptyIsUnsupported(t *testing.T) {
	for _, body := range []string{repNoNext(repFixture(t, "company")), repEmpty()} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		items, m, e := repClient(s).MarketReports(context.Background(), "stock", "", "600519", "", 8)
		s.Close()
		repKind(t, e, contracts.Unsupported)
		if len(items) != 0 || !m.Partial || m.QueryCoverage != "unsupported" {
			t.Fatalf("%+v", m)
		}
	}
}
func TestReportRejectMalformedRowsAndUnsafePagination(t *testing.T) {
	stock := repNoNext(repFixture(t, "stock"))
	cases := map[string]string{
		"bad-date": strings.ReplaceAll(stock, "2026-09-22", "2026-02-30"), "future": strings.ReplaceAll(stock, "2026-09-22", "2099-09-22"),
		"host": strings.ReplaceAll(stock, "stock.finance.sina.com.cn/stock/go.php/vReport_Show", "evil.test/stock/go.php/vReport_Show"), "js": strings.ReplaceAll(stock, "//stock.finance.sina.com.cn/stock/go.php/vReport_Show", "javascript:alert(1)//"),
		"id": strings.ReplaceAll(stock, "843401040115", "not-id"), "title-truncated": strings.ReplaceAll(stock, "贵州茅台(600519)公司跟踪报告：以动销定投放 缓解市场压力 低速稳态发展", "贵州茅台(600519)...")}
	for n, b := range cases {
		t.Run(n, func(t *testing.T) {
			p, e := repParseList(b, repNow(), 1)
			if n == "host" || n == "js" {
				repKind(t, e, contracts.InvalidResponse)
			} else if p.invalid < 1 {
				t.Fatalf("not rejected %+v %v", p, e)
			}
		})
	}
	_, e := repParseList(strings.ReplaceAll(repFixture(t, "stock"), "set_page_num('2')", "set_page_num('2');alert(1)"), repNow(), 1)
	repKind(t, e, contracts.InvalidResponse)
}
func TestReportValidEmptyVersusChallenge(t *testing.T) {
	for _, tc := range []struct {
		body string
		want contracts.ErrorKind
	}{{repEmpty(), ""}, {`<title>安全验证</title>` + repEmpty(), contracts.InvalidResponse}, {`<title>研究报告</title><script>var rows=[];</script>`, contracts.InvalidResponse}, {`<form>请先登录 验证码</form>` + repEmpty(), contracts.InvalidResponse}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
		items, _, e := repClient(s).MarketReports(context.Background(), "stock", "", "", "", 8)
		s.Close()
		if len(items) != 0 {
			t.Fatal("not empty")
		}
		repKind(t, e, tc.want)
	}
	p, e := repParseList(repNoNext(repFixture(t, "stock"))+`<script>alert('验证码')</script><div id="loginLayer">请先登录</div>`, repNow(), 1)
	if e != nil || len(p.items) != 2 {
		t.Fatalf("%d %v", len(p.items), e)
	}
}
func TestReportDetailsEmptyMismatchTruncationAndMask(t *testing.T) {
	list, e := repParseList(repFixture(t, "stock"), repNow(), 1)
	if e != nil {
		t.Fatal(e)
	}
	detail := repFixture(t, "detail")
	for _, tc := range []struct{ name, body, want string }{{"normal", detail, "available"}, {"unavailable", strings.Replace(detail, `class="blk_container"`, `class="other"`, 1), "unavailable"}, {"empty", strings.Replace(detail, detail[strings.Index(detail, "<p>"):strings.Index(detail, "</p>")+4], "<p>暂无资料</p>", 1), "unavailable"}, {"identity", strings.ReplaceAll(detail, "日期：2026-09-22", "日期：2026-09-23"), "unavailable"}, {"truncated", strings.Replace(detail, "<p>", "<p>"+strings.Repeat("平台文字", 7000), 1), "truncated"}} {
		t.Run(tc.name, func(t *testing.T) {
			i := list.items[0]
			e := repParseDetail(tc.body, &i)
			if tc.want == "unavailable" {
				if e == nil {
					t.Fatal("invalid detail accepted")
				}
				return
			}
			if e != nil || i.ContentStatus != tc.want {
				t.Fatalf("%s %v", i.ContentStatus, e)
			}
			if tc.want == "truncated" && len([]rune(i.Content)) != repMaxContent {
				t.Fatal("text bound")
			}
		})
	}
	i := foundation.MarketResearchItem{Kind: "stock", ID: "1", Title: "sample", Content: "EPS 0 PE 0 目标价0 买入"}
	i.Meta = repMeta(repNow())
	i.Meta.AvailableFields = repFields(i)
	for _, f := range []string{"eps", "pe", "target_low", "target_high", "rating"} {
		if foundation.FieldAvailable(i.Meta, f) {
			t.Fatal("placeholder known " + f)
		}
	}
	i.Meta.AvailableFields = append(i.Meta.AvailableFields, "eps")
	if !foundation.FieldAvailable(i.Meta, "eps") {
		t.Fatal("explicit true zero not known")
	}
}
func TestReportDedupNoProgressAndPageBound(t *testing.T) {
	original := repFixture(t, "company")
	for _, mode := range []string{"duplicate", "bound"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, "vReport_List") {
					http.Error(w, "none", 503)
					return
				}
				calls.Add(1)
				page := 1
				if raw := r.URL.Query().Get("p"); raw != "" {
					fmt.Sscan(raw, &page)
				}
				body := strings.ReplaceAll(original, "set_page_num('2')", fmt.Sprintf("set_page_num('%d')", page+1))
				if mode == "bound" {
					body = strings.ReplaceAll(body, "844345564978", fmt.Sprintf("8443455649%02d", page))
					body = strings.ReplaceAll(body, "844345320117", fmt.Sprintf("8443453201%02d", page))
				}
				fmt.Fprint(w, body)
			}))
			defer s.Close()
			items, m, e := repClient(s).MarketReports(context.Background(), "stock", "", "", "", 1000)
			if e != nil || m.QueryCoverage != "bounded" || !m.Partial {
				t.Fatalf("%+v %v", m, e)
			}
			if mode == "duplicate" && (len(items) != 2 || calls.Load() != 2 || !strings.Contains(m.FallbackReason, "no progress")) {
				t.Fatalf("%d %d %+v", len(items), calls.Load(), m)
			}
			if mode == "bound" && (len(items) != 10 || calls.Load() != repMaxPages || !strings.Contains(m.FallbackReason, "five-page")) {
				t.Fatalf("%d %d %+v", len(items), calls.Load(), m)
			}
		})
	}
}
func TestReportEncodingOversizeRedirectAndCancel(t *testing.T) {
	body := repNoNext(repFixture(t, "stock"))
	gbk, e := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(body))
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"gbk", "utf8-gbk-header", "oversize", "oversize-length", "redirect", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "vReport_Show") {
					http.Error(w, "none", 503)
					return
				}
				switch mode {
				case "gbk":
					w.Header().Set("Content-Type", "text/html; charset=gbk")
					w.Write(gbk)
				case "utf8-gbk-header":
					w.Header().Set("Content-Type", "text/html; charset=gbk")
					fmt.Fprint(w, body)
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", repMaxBody+1))
				case "oversize-length":
					w.Header().Set("Content-Length", fmt.Sprint(repMaxBody+1))
					w.WriteHeader(200)
				case "redirect":
					http.Redirect(w, r, "https://evil.test/login", 302)
				case "cancel":
					<-r.Context().Done()
				}
			}))
			defer s.Close()
			ctx := context.Background()
			var cancel context.CancelFunc
			if mode == "cancel" {
				ctx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
				defer cancel()
			}
			items, _, e := repClient(s).MarketReports(ctx, "stock", "", "600519", "", 8)
			if mode == "gbk" || mode == "utf8-gbk-header" {
				if e != nil || len(items) != 2 {
					t.Fatalf("%d %v", len(items), e)
				}
			} else if mode == "cancel" {
				if !errors.Is(e, context.DeadlineExceeded) {
					t.Fatal(e)
				}
			} else {
				repKind(t, e, contracts.InvalidResponse)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, e = NewReportClient(ReportConfig{Now: repNow}).MarketReports(ctx, "stock", "", "", "", 8)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestReportDetailsBoundConcurrencyCookiesAndSharedDeadline(t *testing.T) {
	company := repNoNext(repFixture(t, "company"))
	rows := company[strings.Index(company, "<tr><td>1"):strings.Index(company, "</table>")]
	var expanded strings.Builder
	for i := 0; i < 6; i++ {
		r := strings.ReplaceAll(rows, "844345564978", fmt.Sprintf("8443455649%02d", i))
		r = strings.ReplaceAll(r, "844345320117", fmt.Sprintf("8443453201%02d", i))
		expanded.WriteString(r)
	}
	company = company[:strings.Index(company, "<tr><td>1")] + expanded.String() + `</table><div id="_function_code_page"><span class="pagebox_next_nolink">下一页</span></div>`
	var active, maximum, details atomic.Int32
	var mu sync.Mutex
	var deadlines []time.Time
	transport := repRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		d, ok := r.Context().Deadline()
		if !ok {
			t.Error("no shared deadline")
		}
		mu.Lock()
		deadlines = append(deadlines, d)
		mu.Unlock()
		return http.DefaultTransport.RoundTrip(r)
	})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("cookie leaked")
		}
		if strings.Contains(r.URL.Path, "vReport_List") {
			fmt.Fprint(w, company)
			return
		}
		details.Add(1)
		a := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if a <= old || maximum.CompareAndSwap(old, a) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		http.Error(w, "none", 503)
	}))
	defer s.Close()
	jar, _ := cookiejar.New(nil)
	u, _ := http.NewRequest("GET", s.URL, nil)
	jar.SetCookies(u.URL, []*http.Cookie{{Name: "secret", Value: "never"}})
	c := NewReportClient(ReportConfig{BaseURL: s.URL, HTTPClient: &http.Client{Transport: transport, Jar: jar}, Now: repNow})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	want, _ := ctx.Deadline()
	items, m, e := c.MarketReports(ctx, "stock", "", "", "", 1000)
	if e != nil || len(items) != 12 || !m.Partial || details.Load() != repMaxDetails || maximum.Load() > 3 {
		t.Fatalf("%d %d %d %+v %v", len(items), details.Load(), maximum.Load(), m, e)
	}
	for _, d := range deadlines {
		if !d.Equal(want) {
			t.Fatalf("deadline extended %s %s", d, want)
		}
	}
	if !strings.Contains(items[8].ContentIssue, "bounded") {
		t.Fatal("unattempted detail not explicit")
	}
	// A small output limit must not cause more than the requested enrichments.
	details.Store(0)
	items, _, e = c.MarketReports(ctx, "stock", "", "", "", 1)
	if e != nil || len(items) != 1 || details.Load() != 1 {
		t.Fatalf("limit %d %d %v", len(items), details.Load(), e)
	}
}

type repRoundTripFunc func(*http.Request) (*http.Response, error)

func (f repRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
