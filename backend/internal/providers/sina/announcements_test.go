package sina

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func annFixture(t *testing.T, name string) string {
	t.Helper()
	b, e := os.ReadFile("testdata/announcement_" + name + ".html")
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func annTestSymbol(t *testing.T) foundation.Symbol {
	t.Helper()
	s, e := businessSymbol("600519.SH")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func annFixtureServer(t *testing.T, list, page2, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	count := &atomic.Int32{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=gb2312")
		switch r.URL.Path {
		case "/corp/go.php/vCB_AllBulletin/stockid/600519.phtml":
			io.WriteString(w, list)
		case "/corp/view/vCB_AllBulletin.php":
			if r.URL.Query().Get("Page") != "2" {
				t.Errorf("unexpected page %s", r.URL.String())
			}
			io.WriteString(w, page2)
		case "/corp/view/vCB_AllBulletinDetail.php":
			if r.URL.Query().Get("id") == "12496835" {
				io.WriteString(w, body)
			} else {
				http.Error(w, "body unavailable", http.StatusServiceUnavailable)
			}
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s, count
}

func TestAnnouncementOfficialFixtureAndFilters(t *testing.T) {
	list, p2, body := annFixture(t, "list"), annFixture(t, "page2"), annFixture(t, "detail")
	rows, next, e := annParseList(list, annTestSymbol(t), 1)
	if e != nil || len(rows) != 3 || !strings.Contains(next, "Page=2") {
		t.Fatalf("parse=%+v %s %v", rows, next, e)
	}
	if rows[0].Symbol != "600519.SH" || rows[0].StockName != "贵州茅台" || rows[0].ID != "12496835" || rows[2].Category != "" {
		t.Fatalf("identity/categories %+v", rows)
	}
	text, e := annParseDetail(body, rows[0], annTestSymbol(t))
	if e != nil || !strings.Contains(text, "未经审计") || strings.Contains(text, "untrusted") || strings.Contains(text, "下载公告") {
		t.Fatalf("body=%q %v", text, e)
	}
	s, count := annFixtureServer(t, list, p2, body)
	now := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	c := NewAnnouncementClient(AnnouncementConfig{BaseURL: s.URL, Now: func() time.Time { return now }})
	items, meta, e := c.MarketAnnouncements(context.Background(), "报告摘要", "600519.SH", "半年度报告", 10)
	if e != nil || len(items) != 1 || items[0].ContentStatus != "available" || meta.Partial {
		t.Fatalf("result %+v %+v %v", items, meta, e)
	}
	if count.Load() != 3 {
		t.Fatalf("expected 2 list pages+1 body, got %d", count.Load())
	}
	if meta.Source != "sina:announcements" || meta.Provider != "sina" || meta.Capability != "announcement" || meta.ExecutionState != "fetched" || !meta.FetchedAt.Equal(now) || items[0].PublishedAt.Format("2006-01-02") != "2026-08-15" || meta.NativeTimestamp != "" {
		t.Fatalf("provenance %+v", meta)
	}
	if !strings.HasPrefix(items[0].URL, annOrigin+"/corp/view/vCB_AllBulletinDetail.php?") {
		t.Fatalf("unsafe public URL %s", items[0].URL)
	}
	items, meta, e = c.MarketAnnouncements(context.Background(), "", "600519.SH", "临时公告", 10)
	if e != nil || len(items) != 0 || meta.Partial {
		t.Fatalf("unknown type must not match 临时公告: %+v %+v %v", items, meta, e)
	}
}

func TestAnnouncementPaginationDedupeQuality(t *testing.T) {
	s, _ := annFixtureServer(t, annFixture(t, "list"), annFixture(t, "page2"), annFixture(t, "detail"))
	items, meta, e := NewAnnouncementClient(AnnouncementConfig{BaseURL: s.URL}).MarketAnnouncements(context.Background(), "", "600519.SH", "all", 100)
	if e != nil || len(items) != 5 || !meta.Partial || len(meta.MissingIDs) != 4 {
		t.Fatalf("pagination/body %+v %+v %v", items, meta, e)
	}
	seen := map[string]bool{}
	for i, item := range items {
		if seen[item.ID] {
			t.Fatalf("duplicate %s", item.ID)
		}
		seen[item.ID] = true
		if i > 0 && item.PublishedAt.After(items[i-1].PublishedAt) {
			t.Fatal("not sorted")
		}
		if i > 0 && (item.ContentStatus != "unavailable" || item.ContentScope != "list-only" || item.ContentIssue == "") {
			t.Fatalf("failed body quality %+v", item)
		}
	}
}

func TestAnnouncementListValidation(t *testing.T) {
	list := annFixture(t, "list")
	cases := []struct {
		name, document string
		kind           contracts.ErrorKind
	}{
		{"empty", annFixture(t, "empty"), ""}, {"challenge", annFixture(t, "challenge"), contracts.InvalidResponse},
		{"text-only-challenge", strings.Replace(annFixture(t, "empty"), "<ul></ul>", "<ul>安全验证</ul>", 1), contracts.InvalidResponse},
		{"ambiguous-lists", strings.Replace(annFixture(t, "empty"), "</body>", `<div class="datelist"></div></body>`, 1), contracts.InvalidResponse},
		{"wrong-code", strings.ReplaceAll(list, "600519", "000001"), contracts.InvalidResponse},
		{"wrong-market", strings.ReplaceAll(list, "600519.SH", "600519.SZ"), contracts.InvalidResponse},
		{"wrong-date", strings.Replace(list, "2026-08-15", "2026-02-30", 1), contracts.InvalidResponse},
		{"unsafe-host", strings.Replace(list, "/corp/view/vCB_AllBulletinDetail.php?stockid=600519&amp;id=12496835", "https://evil.invalid/corp/view/vCB_AllBulletinDetail.php?stockid=600519&amp;id=12496835", 1), contracts.InvalidResponse},
		{"unsafe-next", strings.Replace(list, "Page=2", "Page=1", 1), contracts.InvalidResponse},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rows, _, e := annParseList(tt.document, annTestSymbol(t), 1)
			if contracts.Kind(e) != tt.kind {
				t.Fatalf("kind=%s err=%v", contracts.Kind(e), e)
			}
			if tt.name == "empty" && len(rows) != 0 {
				t.Fatal("empty not empty")
			}
		})
	}
}

func TestAnnouncementUnsupportedIdentityNoNetwork(t *testing.T) {
	calls := 0
	c := NewAnnouncementClient(AnnouncementConfig{HTTPClient: &http.Client{Transport: annRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("should not request") })}})
	for _, symbol := range []string{"", "600519.SZ", "000001.SH", "00700.HK"} {
		_, _, e := c.MarketAnnouncements(context.Background(), "", symbol, "", 10)
		if contracts.Kind(e) != contracts.Unsupported {
			t.Fatalf("%s: %v", symbol, e)
		}
	}
	if calls != 0 {
		t.Fatalf("unsupported made %d requests", calls)
	}
	for _, symbol := range []string{"000001.SZ", "920001.BJ", "600519.SH"} {
		if _, e := businessSymbol(symbol); e != nil {
			t.Fatal(e)
		}
	}
}

type annRoundTripFunc func(*http.Request) (*http.Response, error)

func (f annRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAnnouncementNoProgressAndCoverage(t *testing.T) {
	list := annFixture(t, "list")
	p2 := strings.Replace(list, "第1页", "第2页", 1)
	p2 = strings.Replace(p2, "Page=2", "Page=3", 1)
	s, _ := annFixtureServer(t, list, p2, annFixture(t, "detail"))
	c := NewAnnouncementClient(AnnouncementConfig{BaseURL: s.URL})
	items, meta, e := c.MarketAnnouncements(context.Background(), "不存在", "600519.SH", "", 10)
	if len(items) != 0 || !meta.Partial || contracts.Kind(e) != contracts.InvalidResponse || len(meta.MissingIDs) != 1 || meta.MissingIDs[0] != "listing:no-progress" {
		t.Fatalf("no-progress %+v %+v %v", items, meta, e)
	}
	page := atomic.Int32{}
	capServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := page.Add(1)
		doc := strings.ReplaceAll(list, "12496835", string(rune('0'+n))+"2496835")
		doc = strings.ReplaceAll(doc, "12496833", string(rune('0'+n))+"2496833")
		doc = strings.ReplaceAll(doc, "12453221", string(rune('0'+n))+"2453221")
		doc = strings.Replace(doc, "Page=2", "Page="+string(rune('1'+n)), 1)
		// Stable same-date rows preserve descending order while IDs progress.
		doc = annDateRE.ReplaceAllString(doc, "2026-04-17")
		io.WriteString(w, doc)
	}))
	defer capServer.Close()
	_, meta, e = NewAnnouncementClient(AnnouncementConfig{BaseURL: capServer.URL}).MarketAnnouncements(context.Background(), "不存在", "600519.SH", "", 100)
	if page.Load() != 4 || !meta.Partial || contracts.Kind(e) != contracts.InvalidResponse || meta.MissingIDs[0] != "listing:page-cap" {
		t.Fatalf("cap calls=%d %+v %v", page.Load(), meta, e)
	}
}

func TestAnnouncementBodyTruncationFailureAndIdentity(t *testing.T) {
	body := annFixture(t, "detail")
	body = strings.Replace(body, "本半年度报告未经审计。", strings.Repeat("文", 8100), 1)
	s, _ := annFixtureServer(t, annFixture(t, "list"), annFixture(t, "page2"), body)
	items, meta, e := NewAnnouncementClient(AnnouncementConfig{BaseURL: s.URL}).MarketAnnouncements(context.Background(), "", "600519.SH", "", 1)
	if e != nil || len(items) != 1 || len([]rune(items[0].Content)) != 8000 || items[0].ContentStatus != "truncated" || items[0].ContentScope != "truncated-text" || !meta.Partial {
		t.Fatalf("truncated %+v %+v %v", items, meta, e)
	}
	rows, _, _ := annParseList(annFixture(t, "list"), annTestSymbol(t), 1)
	for _, bad := range []string{strings.Replace(body, "sh600519.phtml", "sz600519.phtml", 1), strings.Replace(body, "公告日期:2026-08-15", "公告日期:2026-08-16", 1), strings.Replace(body, "id=\"content\"", "id=\"other\"", 1)} {
		if _, e := annParseDetail(bad, rows[0], annTestSymbol(t)); e == nil {
			t.Fatal("mismatched detail accepted")
		}
	}
}

func TestAnnouncementHTTPBudgetCancelOversizeGBK(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	input := &http.Client{Timeout: time.Minute, Jar: jar}
	c := NewAnnouncementClient(AnnouncementConfig{HTTPClient: input})
	if c.httpClient.Timeout != annBudget || c.httpClient.Jar != nil || input.Jar == nil {
		t.Fatal("unsafe client mutation or timeout")
	}
	for _, status := range []int{429, 401, 403, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := atomic.Int32{}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/login" {
					t.Fatal("followed login")
				}
				w.Header().Set("Location", "/login")
				w.WriteHeader(status)
			}))
			defer s.Close()
			_, _, e := NewAnnouncementClient(AnnouncementConfig{BaseURL: s.URL}).MarketAnnouncements(context.Background(), "", "600519.SH", "", 1)
			kind := contracts.Unauthorized
			if status == 429 {
				kind = contracts.RateLimited
			}
			if contracts.Kind(e) != kind || calls.Load() != 1 {
				t.Fatalf("status %d: %v calls %d", status, e, calls.Load())
			}
		})
	}
	for _, chunked := range []bool{false, true} {
		t.Run("oversize", func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if chunked {
					w.(http.Flusher).Flush()
				}
				io.WriteString(w, strings.Repeat("x", annMaxHTML+1))
			}))
			defer s.Close()
			_, _, e := NewAnnouncementClient(AnnouncementConfig{BaseURL: s.URL}).MarketAnnouncements(context.Background(), "", "600519.SH", "", 1)
			if contracts.Kind(e) != contracts.InvalidResponse {
				t.Fatal(e)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, e := c.MarketAnnouncements(ctx, "", "600519.SH", "", 1)
	if !errors.Is(e, context.Canceled) || contracts.Kind(e) != contracts.Canceled {
		t.Fatal(e)
	}
	deadlines := make(chan time.Time, 1)
	c = NewAnnouncementClient(AnnouncementConfig{HTTPClient: &http.Client{Transport: annRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Error("no total budget")
		}
		deadlines <- deadline
		return nil, context.DeadlineExceeded
	})}})
	before := time.Now()
	_, _, e = c.MarketAnnouncements(context.Background(), "", "600519.SH", "", 1)
	if contracts.Kind(e) != contracts.TimedOut || (<-deadlines).Sub(before) > annBudget+time.Second {
		t.Fatal(e)
	}
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	c = NewAnnouncementClient(AnnouncementConfig{HTTPClient: &http.Client{Transport: annRoundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}})
	_, _, e = c.MarketAnnouncements(short, "", "600519.SH", "", 1)
	if contracts.Kind(e) != contracts.TimedOut {
		t.Fatal(e)
	}
	gbk, e := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(annFixture(t, "empty")))
	if e != nil {
		t.Fatal(e)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=gb2312")
		w.Write(gbk)
	}))
	defer s.Close()
	items, meta, e := NewAnnouncementClient(AnnouncementConfig{BaseURL: s.URL}).MarketAnnouncements(context.Background(), "", "600519.SH", "", 1)
	if e != nil || len(items) != 0 || meta.Partial {
		t.Fatalf("valid GBK empty %+v %v", meta, e)
	}
}

func TestAnnouncementDOMComplexityBound(t *testing.T) {
	for _, document := range []string{strings.Repeat("<div>", 129) + strings.Repeat("</div>", 129), strings.Repeat("<br>", 100001), strings.Repeat("x", annMaxHTML*2+1)} {
		if _, err := annParseDOM(document); err == nil {
			t.Fatal("unbounded announcement DOM accepted")
		}
	}
}

func TestAnnouncementLimitsCategoriesAndConflictingDuplicates(t *testing.T) {
	for _, title := range []string{"贵州茅台：关于召开年度报告说明会的公告", "贵州茅台：关于年度报告更正的公告", "贵州茅台：风险评估报告"} {
		if annKnownCategory(title) != "" {
			t.Fatalf("unknown category inferred for %q", title)
		}
	}
	if annKnownCategory("贵州茅台：2025年度报告") != "年度报告" {
		t.Fatal("known annual report category missing")
	}
	var pages, bodies atomic.Int32
	c := NewAnnouncementClient(AnnouncementConfig{HTTPClient: &http.Client{Transport: annRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		doc := ""
		if r.URL.Path == "/corp/view/vCB_AllBulletinDetail.php" {
			bodies.Add(1)
			doc = annFixture(t, "challenge")
		} else {
			n := int(pages.Add(1))
			var rows strings.Builder
			for i := 0; i < 30; i++ {
				fmt.Fprintf(&rows, `2026-04-17&nbsp;<a href="/corp/view/vCB_AllBulletinDetail.php?stockid=600519&amp;id=%d">贵州茅台：重大事项公告</a><br>`, n*100+i)
			}
			doc = `<h1>贵州茅台(600519.SH)</h1><div class="datelist"><ul>` + rows.String() + `</ul></div>`
			if n < 4 {
				doc += fmt.Sprintf(`<a href="/corp/view/vCB_AllBulletin.php?stockid=600519&amp;Page=%d">下一页</a>`, n+1)
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(doc))}, nil
	})}})
	items, _, err := c.MarketAnnouncements(context.Background(), "", "600519.SH", "", 1000)
	if err != nil || len(items) != 100 || pages.Load() != 4 || bodies.Load() != 100 {
		t.Fatalf("bounded pages=%d bodies=%d items=%d %v", pages.Load(), bodies.Load(), len(items), err)
	}
	list := annFixture(t, "list")
	bad := strings.Replace(list, "贵州茅台：2026年半年度报告摘要", "贵州茅台：不同标题", 1)
	bad = strings.Replace(bad, "Page=2", "Page=3", 1)
	s, _ := annFixtureServer(t, list, bad, annFixture(t, "detail"))
	_, meta, err := NewAnnouncementClient(AnnouncementConfig{BaseURL: s.URL}).MarketAnnouncements(context.Background(), "不存在", "600519.SH", "", 30)
	if contracts.Kind(err) != contracts.InvalidResponse || !meta.Partial || meta.MissingIDs[0] != "listing:conflicting-duplicate" {
		t.Fatalf("conflicting duplicate %+v %v", meta, err)
	}
	rows := strings.Repeat(`2026-04-17&nbsp;<a href="/corp/view/vCB_AllBulletinDetail.php?stockid=600519&amp;id=111">贵州茅台：公告</a><br>`, 31)
	_, _, err = annParseList(`<h1>贵州茅台(600519.SH)</h1><div class="datelist">`+rows+`</div>`, annTestSymbol(t), 1)
	if contracts.Kind(err) != contracts.InvalidResponse {
		t.Fatal("31 rows accepted")
	}
}

func TestAnnouncementBodyTimeoutIsQualityNotSourceFailure(t *testing.T) {
	c := NewAnnouncementClient(AnnouncementConfig{HTTPClient: &http.Client{Timeout: 20 * time.Millisecond, Transport: annRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/corp/view/vCB_AllBulletinDetail.php" {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(annFixture(t, "list")))}, nil
	})}})
	items, meta, err := c.MarketAnnouncements(context.Background(), "", "600519.SH", "", 1)
	if err != nil || len(items) != 1 || !meta.Partial || items[0].ContentStatus != "unavailable" || meta.MissingIDs[0] != "12496835:body" {
		t.Fatalf("body timeout %+v %+v %v", items, meta, err)
	}
}

func TestAnnouncementBodyConcurrencyAndSharedBudget(t *testing.T) {
	var active, maxActive atomic.Int32
	deadlines := make(chan time.Time, 10)
	c := NewAnnouncementClient(AnnouncementConfig{HTTPClient: &http.Client{Transport: annRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		d, _ := r.Context().Deadline()
		deadlines <- d
		doc := annFixture(t, "list")
		if r.URL.Path == "/corp/view/vCB_AllBulletinDetail.php" {
			n := active.Add(1)
			for {
				m := maxActive.Load()
				if n <= m || maxActive.CompareAndSwap(m, n) {
					break
				}
			}
			defer active.Add(-1)
			select {
			case <-r.Context().Done():
				return nil, r.Context().Err()
			case <-time.After(5 * time.Millisecond):
			}
			doc = annFixture(t, "detail")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(doc))}, nil
	})}})
	_, _, e := c.MarketAnnouncements(context.Background(), "", "600519.SH", "", 3)
	if e != nil {
		t.Fatal(e)
	}
	close(deadlines)
	var first time.Time
	for d := range deadlines {
		if first.IsZero() {
			first = d
		}
		if !d.Equal(first) {
			t.Fatal("budget reset between requests")
		}
	}
	if maxActive.Load() > 3 || maxActive.Load() < 2 {
		t.Fatalf("concurrency %d", maxActive.Load())
	}
}
