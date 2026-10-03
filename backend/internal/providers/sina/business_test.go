package sina

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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

func businessFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile("testdata/business_" + name + ".html")
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func businessPage(code, market, name, rows string) string {
	return fmt.Sprintf(`<!doctype html><html><head><title>%s(%s)公司资料_新浪财经_新浪网</title></head><body><h1>%s(%s.%s)</h1><table id="comInfo1"><tr><td>公司名称：</td><td>%s股份有限公司</td></tr><tr><td>上市市场：</td><td>%s证券交易所</td></tr>%s</table></body></html>`, name, code, name, code, market, name, map[string]string{"SH": "上海", "SZ": "深圳", "BJ": "北京"}[market], rows)
}

const businessCompleteRows = `<tr><td>公司简介：</td><td>公司成立于2000年，从事白酒产品生产与销售。</td></tr><tr><td>主营业务：</td><td>白酒系列生产销售</td></tr>`

func TestBusinessOfficialMinimalFixtures(t *testing.T) {
	for _, test := range []struct {
		fixture, symbol, name, main string
	}{
		{"sh", "600519.SH", "贵州茅台酒股份有限公司", "贵州茅台酒系列产品的生产与销售,饮料、食品、包装材料的生产与销售,防伪技术开发;信息产业相关产品的研制和开发等。"},
		{"sz", "sz000001", "平安银行股份有限公司", "人民币、外币存贷款;国际、国内结算;票据贴现;外汇买卖;提供担保及信用证服务;提供保管箱服务等。"},
		{"bj", "920002.BJ", "江苏万达特种轴承股份有限公司", "叉车轴承及回转支承的研发、生产和销售"},
	} {
		t.Run(test.fixture, func(t *testing.T) {
			body := businessFixture(t, test.fixture)
			var requestedPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestedPath = r.URL.Path
				if r.Method != http.MethodGet || r.Header.Get("Referer") != "https://finance.sina.com.cn/" {
					t.Errorf("unexpected public request: %s %#v", r.Method, r.Header)
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				io.WriteString(w, body)
			}))
			defer server.Close()
			fixedNow := time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)
			client := NewBusinessClient(BusinessConfig{BaseURL: server.URL + "/corp/go.php/vCI_CorpInfo/stockid", Now: func() time.Time { return fixedNow }})
			profile, err := client.StockBusinessProfile(context.Background(), test.symbol)
			if err != nil {
				t.Fatal(err)
			}
			normalized, _ := foundation.NormalizeSymbol(test.symbol)
			if requestedPath != "/corp/go.php/vCI_CorpInfo/stockid/"+normalized.RawCode+".phtml" {
				t.Fatalf("path = %q", requestedPath)
			}
			if profile.Symbol != normalized.Canonical || profile.Name != test.name || profile.MainBusiness != test.main || profile.Description == "" {
				t.Fatalf("profile = %#v", profile)
			}
			if profile.Industry != "" || profile.BusinessScope != "" {
				t.Fatalf("unreported fields were invented: %#v", profile)
			}
			meta := profile.Meta
			if meta.Source != "sina:business" || meta.Provider != "sina" || meta.Capability != "business" || meta.ExecutionState != "fetched" || meta.Partial || !meta.FieldsKnown {
				t.Fatalf("meta = %#v", meta)
			}
			_, offset := meta.FetchedAt.Zone()
			if !meta.FetchedAt.Equal(fixedNow) || offset != 8*60*60 || meta.TimeZone != "Asia/Shanghai" || meta.AsOf != "" || meta.NativeTimestamp != "" || meta.TradeDate != "" {
				t.Fatalf("capture time is not Shanghai or was mistaken for publication: %#v", meta)
			}
			if meta.SourceURL != server.URL+requestedPath || meta.NativeCode != normalized.Sina || meta.InstrumentID != normalized.Canonical || !foundation.FieldAvailable(meta, "main_business") || foundation.FieldAvailable(meta, "industry") {
				t.Fatalf("bad provenance/presence: %#v", meta)
			}
		})
	}
}

func TestBusinessCharsetsEntitiesAndParagraphs(t *testing.T) {
	rows := `<tr><th>公司简介：</th><td><p>&nbsp;甲公司 &amp; 合作方</p><p>第二段<br/>第三行<script>恶意JS主营业务</script><style>假简介</style></p></td></tr><tr><td>主营业务：</td><td><span>白酒</span>系列生产销售 &amp; 服务</td></tr><tr><td>所属行业：</td><td>食品制造业</td><td>经营范围：</td><td>许可项目：食品生产。</td></tr>`
	page := businessPage("600519", "SH", "贵州茅台", rows)
	for _, test := range []struct {
		name, contentType string
		gbk               bool
	}{
		{"UTF8", "text/html; charset=UTF-8", false},
		{"GBK", "text/html; charset=GBK", true},
		{"GBKNoHeader", "text/html", true},
		{"UTF8StaleGBKHeader", "text/html; charset=gb2312", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(page)
			if test.gbk {
				var err error
				body, err = simplifiedchinese.GBK.NewEncoder().Bytes(body)
				if err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/local/600519.phtml" {
					t.Errorf("template path = %q", r.URL.Path)
				}
				w.Header().Set("Content-Type", test.contentType)
				w.Write(body)
			}))
			defer server.Close()
			profile, err := NewBusinessClient(BusinessConfig{BaseURL: server.URL + "/local/{code}.phtml"}).StockBusinessProfile(context.Background(), "600519")
			if err != nil {
				t.Fatal(err)
			}
			if profile.Description != "甲公司 & 合作方\n第二段\n第三行" || profile.MainBusiness != "白酒系列生产销售 & 服务" || profile.Industry != "食品制造业" || profile.BusinessScope != "许可项目：食品生产。" {
				t.Fatalf("decoded profile = %#v", profile)
			}
			if !foundation.FieldAvailable(profile.Meta, "business_scope") || !foundation.FieldAvailable(profile.Meta, "industry") {
				t.Fatalf("reported fields not tracked: %#v", profile.Meta)
			}
		})
	}
}

func TestBusinessMissingRequiredFieldsNeedFallback(t *testing.T) {
	for _, test := range []struct {
		name, rows, missing string
	}{
		{"missing-main", `<tr><td>公司简介：</td><td>公司以技术研发为核心。</td></tr><tr><td>所属行业：</td><td>白酒行业</td></tr><tr><td>经营范围：</td><td>白酒生产销售。</td></tr>`, "main_business"},
		{"missing-description", `<tr><td>主营业务：</td><td>白酒系列生产销售</td></tr><tr><td>经营范围：</td><td>公司从事白酒生产销售。</td></tr>`, "description"},
		{"empty-main", `<tr><td>主营业务：</td><td>&nbsp;--</td></tr><tr><td>公司简介：</td><td>公司从事白酒生产销售。</td></tr>`, "main_business"},
		{"template-main", `<tr><td>主营业务：</td><td>@main_business@</td></tr><tr><td>公司简介：</td><td>公司从事白酒生产销售。</td></tr>`, "main_business"},
		{"template-description", `<tr><td>主营业务：</td><td>白酒系列生产销售</td></tr><tr><td>公司简介：</td><td>{{description}}</td></tr>`, "description"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, businessPage("600519", "SH", "贵州茅台", test.rows))
			}))
			defer server.Close()
			profile, err := NewBusinessClient(BusinessConfig{BaseURL: server.URL}).StockBusinessProfile(context.Background(), "600519.SH")
			assertBusinessKind(t, err, contracts.NoData)
			if !profile.Meta.Partial || profile.Meta.ExecutionState != "fetched" || foundation.FieldAvailable(profile.Meta, test.missing) {
				t.Fatalf("missing field falsely succeeded: %#v", profile)
			}
			if test.missing == "main_business" && profile.MainBusiness != "" || test.missing == "description" && profile.Description != "" {
				t.Fatalf("field inferred from industry/scope: %#v", profile)
			}
		})
	}
}

func TestBusinessRejectsWrongIdentityChallengesAndTemplates(t *testing.T) {
	valid := businessPage("600519", "SH", "贵州茅台", businessCompleteRows)
	for _, test := range []struct {
		name, body, symbol string
		kind               contracts.ErrorKind
	}{
		{"wrong-code", businessPage("600520", "SH", "贵州茅台", businessCompleteRows), "600519.SH", contracts.InvalidResponse},
		{"wrong-title-only", strings.Replace(valid, "贵州茅台(600519)公司资料", "贵州茅台(600520)公司资料", 1), "600519.SH", contracts.InvalidResponse},
		{"wrong-company", strings.Replace(valid, "贵州茅台股份有限公司", "完全无关股份有限公司", 1), "600519.SH", contracts.InvalidResponse},
		{"wrong-company-history-cannot-bypass", strings.Replace(strings.Replace(valid, "贵州茅台股份有限公司", "完全无关股份有限公司", 1), "</table>", `<tr><td>证券简称更名历史：</td><td>贵州茅台</td></tr></table>`, 1), "600519.SH", contracts.InvalidResponse},
		{"wrong-market", businessPage("600519", "SZ", "贵州茅台", businessCompleteRows), "600519.SH", contracts.InvalidResponse},
		{"wrong-table-code", strings.Replace(valid, "</table>", `<tr><td>股票代码：</td><td>600520</td></tr></table>`, 1), "600519.SH", contracts.InvalidResponse},
		{"no-code", strings.ReplaceAll(valid, "600519", "------"), "600519.SH", contracts.InvalidResponse},
		{"captcha", `<html><title>安全验证</title><body>请输入验证码</body></html>`, "600519.SH", contracts.InvalidResponse},
		{"login", `<html><title>用户登录</title><body>请先登录后查看公司资料</body></html>`, "600519.SH", contracts.InvalidResponse},
		{"login-cell", strings.Replace(valid, "白酒系列生产销售", "登录后查看", 1), "600519.SH", contracts.InvalidResponse},
		{"captcha-title-with-table", strings.Replace(valid, "贵州茅台(600519)公司资料", "验证码", 1), "600519.SH", contracts.InvalidResponse},
		{"captcha-form-with-table", strings.Replace(valid, "</body>", `<form><label>验证码</label><input name="captcha"></form></body>`, 1), "600519.SH", contracts.InvalidResponse},
		{"error-heading-with-table", strings.Replace(valid, "</body>", `<h2>系统繁忙</h2></body>`, 1), "600519.SH", contracts.InvalidResponse},
		{"invalid-encoding", valid + "\uFFFD", "600519.SH", contracts.InvalidResponse},
		{"invalid-html", `<html><body><table><tr><td>损坏网页`, "600519.SH", contracts.InvalidResponse},
		{"plain-json", `{"name":"贵州茅台","main_business":"白酒"}`, "600519.SH", contracts.InvalidResponse},
		{"script-only", `<script>var profile = "公司名称：贵州茅台 主营业务：白酒"</script>`, "600519.SH", contracts.InvalidResponse},
		{"empty-name", strings.Replace(valid, "贵州茅台股份有限公司", "--", 1), "600519.SH", contracts.NoData},
		{"empty-profile", `<html><title>公司资料</title><table id="comInfo1"><tr><td>公司名称：</td><td></td></tr></table></html>`, "600519.SH", contracts.NoData},
		{"bj-wrong-sh", businessPage("920002", "SH", "万达轴承", businessCompleteRows), "920002.BJ", contracts.InvalidResponse},
		{"bj-numeric-page-sh", strings.Replace(businessPage("920002", "BJ", "万达轴承", businessCompleteRows), "北京证券交易所", "上海证券交易所", 1), "920002.BJ", contracts.Unsupported},
		{"bj-no-market", strings.Replace(businessPage("920002", "BJ", "万达轴承", businessCompleteRows), "北京证券交易所", "", 1), "920002.BJ", contracts.Unsupported},
		{"bj-no-data", `<html><title>公司资料</title><body>暂无资料</body></html>`, "920002.BJ", contracts.NoData},
		{"conflicting-label", strings.Replace(valid, "</table>", `<tr><td>主营业务：</td><td>轴承制造</td></tr></table>`, 1), "600519.SH", contracts.InvalidResponse},
		{"duplicate-table", strings.Replace(valid, "</body>", `<table><tr><td>公司名称：</td><td>贵州茅台股份有限公司</td></tr></table></body>`, 1), "600519.SH", contracts.InvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := foundation.NormalizeSymbol(test.symbol)
			if err != nil {
				t.Fatal(err)
			}
			_, err = parseBusinessHTML(test.body, normalized)
			assertBusinessKind(t, err, test.kind)
		})
	}
}

func TestBusinessNormalPageLoginAndQuoteTemplatesAreNotProfileFields(t *testing.T) {
	body := businessFixture(t, "sh")
	body = strings.Replace(body, "</body>", `<div>@now@ @open@ @company@</div><p>查看自选股请先 <a>登录</a></p><div id="loginLayer"><input type="password"><p>用户登录：请先登录</p></div><script>window.fake="验证码"</script></body>`, 1)
	symbol, _ := foundation.NormalizeSymbol("600519")
	profile, err := parseBusinessHTML(body, symbol)
	if err != nil || profile.MainBusiness == "" {
		t.Fatalf("normal site's optional login/quote shell rejected: %#v %v", profile, err)
	}
}

func TestBusinessOnlySameRowCellsSupplyFields(t *testing.T) {
	symbol, _ := foundation.NormalizeSymbol("600519")
	page := businessPage("600519", "SH", "贵州茅台", `<tr><td>公司简介：</td><td>公司从事白酒生产。</td></tr><tr><td>主营业务：</td></tr><tr><td>行业：</td><td>白酒行业</td></tr>`)
	profile, err := parseBusinessHTML(page, symbol)
	assertBusinessKind(t, err, contracts.NoData)
	if profile.MainBusiness != "" || profile.Industry != "白酒行业" {
		t.Fatalf("unrelated cells filled missing business: %#v", profile)
	}
	page = businessPage("600519", "SH", "贵州茅台", `<tr><td>公司简介：</td><td>公司从事白酒生产。</td></tr><tr><td>附加信息：</td><td><table><tr><td>主营业务：</td><td>假业务</td></tr></table></td></tr>`)
	profile, err = parseBusinessHTML(page, symbol)
	assertBusinessKind(t, err, contracts.NoData)
	if profile.MainBusiness != "" {
		t.Fatalf("nested table was used as main-business evidence: %#v", profile)
	}
}

func TestBusinessRejectsUnsupportedSymbolsWithoutRequests(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := NewBusinessClient(BusinessConfig{BaseURL: server.URL})
	for _, symbol := range []string{"", "bad", "12345", "00700.HK", "600519.US", "600519.SZ", "000001.SH", "920002.SH", "999999", "111111.SZ", "200001.SZ", "900001.SH", "399001.SZ", "510300.SH", "660001.SH", "777777.BJ"} {
		_, err := client.StockBusinessProfile(context.Background(), symbol)
		assertBusinessKind(t, err, contracts.Unsupported)
	}
	if calls.Load() != 0 {
		t.Fatalf("unsupported symbols made %d requests", calls.Load())
	}
}

type businessRoundTripFunc func(*http.Request) (*http.Response, error)

func (f businessRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBusinessDeadlineBudgetAndCancellation(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(fmt.Sprint(short), func(t *testing.T) {
			ctx := context.Background()
			var cancel context.CancelFunc
			var callerDeadline time.Time
			if short {
				ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				callerDeadline, _ = ctx.Deadline()
			}
			httpClient := &http.Client{Transport: businessRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 6*time.Second {
					t.Errorf("missing or extended deadline: %v", deadline)
				}
				if short && !deadline.Equal(callerDeadline) {
					t.Errorf("short caller deadline was changed: %v vs %v", deadline, callerDeadline)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(businessPage("600519", "SH", "贵州茅台", businessCompleteRows)))}, nil
			})}
			_, err := NewBusinessClient(BusinessConfig{HTTPClient: httpClient}).StockBusinessProfile(ctx, "600519.SH")
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, canceled := range []bool{false, true} {
		t.Run("propagates-"+fmt.Sprint(canceled), func(t *testing.T) {
			var calls atomic.Int32
			httpClient := &http.Client{Transport: businessRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			if canceled {
				cancel()
			} else {
				defer cancel()
			}
			start := time.Now()
			_, err := NewBusinessClient(BusinessConfig{HTTPClient: httpClient}).StockBusinessProfile(ctx, "600519.SH")
			kind, sentinel := contracts.TimedOut, context.DeadlineExceeded
			if canceled {
				kind, sentinel = contracts.Canceled, context.Canceled
			}
			assertBusinessKind(t, err, kind)
			if !errors.Is(err, sentinel) || time.Since(start) > time.Second || canceled && calls.Load() != 0 {
				t.Fatalf("context not propagated promptly: %v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestBusinessCancellationDuringBodyRead(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-started; cancel() }()
	_, err := NewBusinessClient(BusinessConfig{BaseURL: server.URL}).StockBusinessProfile(ctx, "600519.SH")
	assertBusinessKind(t, err, contracts.Canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("body read lost context cancellation: %v", err)
	}
}

func TestBusinessOversizeStatusNoRetryAndNoRedirect(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		kind   contracts.ErrorKind
	}{
		{"known-oversize", 200, contracts.InvalidResponse},
		{"chunked-oversize", 200, contracts.InvalidResponse},
		{"404", 404, contracts.NoData},
		{"403", 403, contracts.Unauthorized},
		{"429", 429, contracts.RateLimited},
		{"503", 503, contracts.UpstreamFailure},
		{"redirect-login", 302, contracts.InvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if test.name == "redirect-login" {
					w.Header().Set("Location", "/login")
				}
				if test.name == "known-oversize" {
					w.Header().Set("Content-Length", fmt.Sprint(businessMaxBody+1))
				}
				w.WriteHeader(test.status)
				if test.name == "chunked-oversize" {
					w.(http.Flusher).Flush()
				}
				if test.status == 200 {
					io.WriteString(w, strings.Repeat("x", businessMaxBody+1))
				}
			}))
			defer server.Close()
			_, err := NewBusinessClient(BusinessConfig{BaseURL: server.URL}).StockBusinessProfile(context.Background(), "600519.SH")
			assertBusinessKind(t, err, test.kind)
			if calls.Load() != 1 {
				t.Fatalf("automatic retries/redirects happened: %d", calls.Load())
			}
		})
	}
}

func TestBusinessNoCacheAndDefaultURL(t *testing.T) {
	var calls int
	httpClient := &http.Client{Transport: businessRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://vip.stock.finance.sina.com.cn/corp/go.php/vCI_CorpInfo/stockid/600519.phtml" || r.Header.Get("Cookie") != "" {
			t.Errorf("unexpected endpoint/credentials: %v", r)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(businessPage("600519", "SH", "贵州茅台", businessCompleteRows)))}, nil
	})}
	client := NewBusinessClient(BusinessConfig{HTTPClient: httpClient})
	for range 2 {
		if _, err := client.StockBusinessProfile(context.Background(), "600519.SH"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("requests were cached: %d", calls)
	}
}

func assertBusinessKind(t *testing.T, err error, kind contracts.ErrorKind) {
	t.Helper()
	var typed *contracts.Error
	if err == nil || !errors.As(err, &typed) || contracts.Kind(err) != kind || typed.SourceID != "sina" || typed.Capability != "business" {
		t.Fatalf("error = %v; want typed sina/business %s", err, kind)
	}
}
