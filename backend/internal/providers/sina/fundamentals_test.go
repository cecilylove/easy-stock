package sina

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

var fundamentalsTestNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func financialTestItem(field, title string, value, yoy any) map[string]any {
	return map[string]any{"item_field": field, "item_title": title, "item_value": value, "item_tongbi": yoy}
}

func financialTestItems() []map[string]any {
	return []map[string]any{
		financialTestItem("PARENETP", "归母净利润", "44", -0.01952),
		financialTestItem("BIZTOTINCO", "营业总收入", "92", 0.013),
		financialTestItem("NETPROFIT", "净利润", "46", -0.02029),
		financialTestItem("NPCUT", "扣非净利润", "43", -0.0204),
		financialTestItem("EPSBASIC", "基本每股收益", "35.57", ""),
		financialTestItem("ROEWEIGHTED", "净资产收益率(ROE)", "16.75", ""),
		financialTestItem("SGPMARGIN", "毛利率", "89.55", ""),
		financialTestItem("ASSLIABRT", "资产负债率", "15.19", ""),
		financialTestItem("OPNCFPS", "每股现金流", "56.54", 4.41476),
		financialTestItem("OPNCFPS", "每股经营现金流", "56.5400", 4.41476),
		financialTestItem("NCFPS", "每股现金流量净额", "46.70", 3.1662),
	}
}

func financialTestReport(items []map[string]any) map[string]any {
	return map[string]any{"rType": "合并期末", "rCurrency": "CNY", "publish_date": "20260815", "data_source": "其他", "data": items}
}

func financialTestEnvelope(dates []map[string]any, reports map[string]any) map[string]any {
	return map[string]any{"result": map[string]any{"status": map[string]any{"code": 0}, "data": map[string]any{"report_date": dates, "report_list": reports}}}
}

func financialTestDate(date, name string) map[string]any {
	periodType := map[string]int{"0331": 1, "0630": 2, "0930": 3, "1231": 4}[date[4:]]
	return map[string]any{"date_value": date, "date_description": name, "date_type": periodType}
}

func financialTestBody(items []map[string]any) string {
	body, _ := json.Marshal(financialTestEnvelope([]map[string]any{financialTestDate("20260630", "2026半年报")}, map[string]any{"20260630": financialTestReport(items)}))
	return string(body)
}

func financialTestClient(t *testing.T, body string) (*FundamentalsClient, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("source") != "gjzb" || r.URL.Query().Get("type") != "0" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("num") != "4" {
			t.Errorf("unexpected query %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return NewFundamentalsClient(FundamentalsConfig{BaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return fundamentalsTestNow }}), calls
}

func TestFundamentalsNeverBorrowsCookiesOrFollowsLoginRedirect(t *testing.T) {
	var loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("public financial request borrowed cookies")
		}
		if r.URL.Path == "/login" {
			loginCalls.Add(1)
			fmt.Fprint(w, financialTestBody(financialTestItems()))
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	endpoint, _ := url.Parse(server.URL)
	jar.SetCookies(endpoint, []*http.Cookie{{Name: "auth", Value: "test-only"}})
	caller := server.Client()
	caller.Jar = jar
	client := NewFundamentalsClient(FundamentalsConfig{BaseURL: server.URL, HTTPClient: caller})
	if _, err := client.StockFundamentals(context.Background(), "600519.SH"); err == nil {
		t.Fatal("redirect accepted")
	}
	if loginCalls.Load() != 0 || caller.Jar != jar {
		t.Fatal("login followed or caller client mutated")
	}
}

func TestLatestDisclosureUnknownDoesNotSilentlyUseOlderQuarter(t *testing.T) {
	latest := financialTestReport(financialTestItems())
	latest["publish_date"] = ""
	older := financialTestReport(financialTestItems())
	older["publish_date"] = "20260425"
	body, _ := json.Marshal(financialTestEnvelope([]map[string]any{financialTestDate("20260630", "2026半年报"), financialTestDate("20260331", "2026一季报")}, map[string]any{"20260630": latest, "20260331": older}))
	client, _ := financialTestClient(t, string(body))
	if item, err := client.StockFundamentals(context.Background(), "600519.SH"); err == nil {
		t.Fatalf("unknown latest disclosure downgraded silently %+v", item)
	}
}

func TestFundamentalsOrdinaryAndAliases(t *testing.T) {
	for _, symbol := range []string{"600519", "sh600519", "600519.SH", "SH600519", "000001.SZ", "sz300001", "688001.SH", "689009.SH", "bj430047", "830799.BJ", "920001"} {
		t.Run(symbol, func(t *testing.T) {
			client, calls := financialTestClient(t, financialTestBody(financialTestItems()))
			item, err := client.StockFundamentals(context.Background(), symbol)
			if err != nil {
				t.Fatal(err)
			}
			normalized, _ := foundation.NormalizeSymbol(symbol)
			if item.Symbol != normalized.Canonical || item.Meta.NativeCode != normalized.Sina || !strings.Contains(item.Meta.SourceURL, "paperCode="+normalized.Sina) {
				t.Fatalf("bad symbol/source %#v", item)
			}
			if item.ReportDate != "2026-06-30" || item.ReportName != "2026半年报" || item.Revenue != 92 || item.NetProfit != 44 || item.RevenueYearOverYear != 1.3 || item.NetProfitYearOverYear != -1.952 || item.DeductedNetProfitYearOverYear != -2.04 || item.OperatingCashFlowPerShare != 56.54 {
				t.Fatalf("bad metrics %#v", item)
			}
			if !item.DeductedNetProfitAvailable || !item.DeductedNetProfitYearOverYearAvailable || item.DeductedNetProfitReportDate != item.ReportDate {
				t.Fatalf("bad deducted metadata %#v", item)
			}
			if item.Meta.Source != "sina:financials" || item.Meta.Provider != "sina" || item.Meta.Capability != "fundamentals" || item.Meta.ExecutionState != "fetched" || item.Meta.Stale || item.Meta.Partial || !item.Meta.FieldsKnown || len(item.Meta.AvailableFields) != 11 || item.Meta.AmountCurrency != "CNY" || item.Meta.Period != "cumulative" || calls.Load() != 1 {
				t.Fatalf("bad metadata %#v", item.Meta)
			}
			if item.PublishedAt.Format(time.RFC3339) != "2026-08-15T00:00:00+08:00" {
				t.Fatalf("publication %v", item.PublishedAt)
			}
		})
	}
}

func TestFundamentalsBankNotApplicable(t *testing.T) {
	items := financialTestItems()
	items[0] = financialTestItem("NETPARECOMPPROF", "归母净利润", "12", 0.11)
	items[1] = financialTestItem("BIZINCO", "营业收入", "45", 0.03)
	client, _ := financialTestClient(t, financialTestBody(items))
	item, err := client.StockFundamentals(context.Background(), "601398")
	if err != nil {
		t.Fatal(err)
	}
	if item.NetProfit != 12 || item.Revenue != 45 || item.NetProfitYearOverYear != 11 || item.RevenueYearOverYear != 3 || item.FieldAvailable("gross_margin") || item.GrossMargin != 0 || len(item.NotApplicableFields) != 1 || item.NotApplicableFields[0] != "gross_margin" || item.Meta.Partial {
		t.Fatalf("bad bank %#v", item)
	}
}

func TestFundamentalsMissingNumbersAndValidZero(t *testing.T) {
	items := []map[string]any{
		financialTestItem("PARENETP", "归母净利润", "0", "0"),
		financialTestItem("BIZTOTINCO", "营业总收入", nil, 0.05),
		financialTestItem("NPCUT", "扣非净利润", "--", 0),
		financialTestItem("EPSBASIC", "基本每股收益", "", nil),
		financialTestItem("ROEWEIGHTED", "ROE", "NaN", "Inf"),
		financialTestItem("ASSLIABRT", "资产负债率", "-Inf", 0),
		financialTestItem("OPNCFPS", "每股现金流", "8", nil),
	}
	client, _ := financialTestClient(t, financialTestBody(items))
	item, err := client.StockFundamentals(context.Background(), "600519")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"net_profit", "net_profit_yoy", "revenue_yoy", "deducted_net_profit_yoy"} {
		if !item.FieldAvailable(field) {
			t.Errorf("valid zero/growth missing %s", field)
		}
	}
	for _, field := range []string{"revenue", "deducted_net_profit", "eps", "roe", "debt_ratio", "operating_cash_flow_per_share"} {
		if item.FieldAvailable(field) {
			t.Errorf("missing/nonfinite field present %s", field)
		}
	}
	if !item.Meta.Partial || item.DeductedNetProfitAvailable || !item.DeductedNetProfitYearOverYearAvailable || item.DeductedNetProfitReportDate != "" {
		t.Fatalf("bad partial %#v", item)
	}
}

func TestFundamentalsLatestActualDateAndPublishedAsOf(t *testing.T) {
	newest := financialTestReport(financialTestItems())
	newest["publish_date"] = "20261030"
	latest := financialTestReport(financialTestItems())
	older := financialTestReport([]map[string]any{financialTestItem("PARENETP", "归母净利润", "1", 0)})
	older["publish_date"] = "20260430"
	body, _ := json.Marshal(financialTestEnvelope([]map[string]any{
		financialTestDate("20260331", "2026一季报"), financialTestDate("20260930", "2026三季报"), financialTestDate("20260630", "2026半年报"),
	}, map[string]any{"20260331": older, "20260930": newest, "20260630": latest}))
	client, _ := financialTestClient(t, string(body))
	item, err := client.StockFundamentals(context.Background(), "600519")
	if err != nil || item.ReportDate != "2026-06-30" || item.NetProfit != 44 {
		t.Fatalf("bad selection %#v %v", item, err)
	}
}

func TestFundamentalsRejectInvalidProtocol(t *testing.T) {
	good := financialTestBody(financialTestItems())
	cases := map[string]string{
		"error status":              strings.Replace(good, `"code":0`, `"code":1`, 1),
		"missing status":            `{"result":{"data":{}}}`,
		"missing code":              `{"result":{"status":{},"data":{}}}`,
		"null data":                 `{"result":{"status":{"code":0},"data":null}}`,
		"invalid data shape":        `{"result":{"status":{"code":0},"data":[]}}`,
		"invalid dates shape":       strings.Replace(good, `"report_date":[`, `"report_date":{`, 1),
		"no core":                   financialTestBody([]map[string]any{financialTestItem("NETPROFIT", "净利润", 10, 0), financialTestItem("EPSBASIC", "EPS", 1, 0)}),
		"no value core":             financialTestBody([]map[string]any{financialTestItem("PARENETP", "归母净利润", nil, 0.1)}),
		"invalid date":              strings.ReplaceAll(good, "20260630", "20260230"),
		"invalid publication":       strings.Replace(good, "20260815", "20260832", 1),
		"unknown publication":       strings.Replace(good, `"publish_date":`, `"update_date":`, 1),
		"publication before report": strings.Replace(good, "20260815", "20260515", 1),
		"future publication":        strings.Replace(good, "20260815", "20261030", 1),
		"parent report":             strings.Replace(good, "合并期末", "母公司期末", 1),
		"single quarter":            strings.Replace(good, "2026半年报", "2026单季报", 1),
		"quarter basis":             strings.Replace(good, "合并期末", "合并单季度", 1),
		"currency":                  strings.Replace(good, "CNY", "USD", 1),
		"duplicate JSON key":        strings.Replace(good, `"code":0`, `"code":0,"code":1`, 1),
		"case duplicate JSON key":   strings.Replace(good, `"code":0`, `"code":0,"Code":1`, 1),
		"unknown period type":       strings.Replace(good, `"date_type":2`, `"date_type":0`, 1),
		"conflicting period type":   strings.Replace(good, `"date_type":2`, `"date_type":3`, 1),
		"javascript":                "callback(" + good + ");",
		"trailing JSON":             good + `{}`,
		"infinite JSON":             strings.Replace(good, `"44"`, `1e999`, 1),
	}
	// An overflowing numeric core is unavailable, not silently normalized to zero.
	cases["infinite JSON"] = financialTestBody([]map[string]any{financialTestItem("PARENETP", "归母净利润", "1e999", 0)})
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			client, calls := financialTestClient(t, body)
			if item, err := client.StockFundamentals(context.Background(), "600519"); err == nil {
				t.Fatalf("accepted invalid response %#v", item)
			}
			if calls.Load() != 1 {
				t.Fatalf("unexpected retries %d", calls.Load())
			}
		})
	}
}

func TestFundamentalsConflictingMetricKeys(t *testing.T) {
	for _, kind := range []string{"value", "yoy", "alias", "date", "orphan"} {
		t.Run(kind, func(t *testing.T) {
			items := financialTestItems()
			switch kind {
			case "value":
				items = append(items, financialTestItem("PARENETP", "归母净利润", 100, -0.01952))
			case "yoy":
				items = append(items, financialTestItem("PARENETP", "归母净利润", 44, 0.2))
			case "alias":
				items = append(items, financialTestItem("NETPARECOMPPROF", "归母净利润", 100, 0.2))
			}
			envelope := financialTestEnvelope([]map[string]any{financialTestDate("20260630", "2026半年报")}, map[string]any{"20260630": financialTestReport(items)})
			if kind == "date" {
				envelope = financialTestEnvelope([]map[string]any{financialTestDate("20260630", "2026半年报"), financialTestDate("20260630", "2026三季报")}, map[string]any{"20260630": financialTestReport(items)})
			}
			if kind == "orphan" {
				envelope = financialTestEnvelope([]map[string]any{financialTestDate("20260331", "2026一季报")}, map[string]any{"20260630": financialTestReport(items)})
			}
			body, _ := json.Marshal(envelope)
			client, _ := financialTestClient(t, string(body))
			if _, err := client.StockFundamentals(context.Background(), "600519"); err == nil {
				t.Fatal("accepted conflict")
			}
		})
	}
}

func TestFundamentalsUnsupportedSymbolsNeverFetch(t *testing.T) {
	client, calls := financialTestClient(t, financialTestBody(financialTestItems()))
	for _, symbol := range []string{"", "00700", "60051.SH", "sh000001", "399001.SZ", "510300.SH", "600519.SZ", "000001.SH", "123456", "hk00700", "600519.XX", "670001.SH", "430047.SZ"} {
		if SupportsFundamentalsSymbol(symbol) {
			t.Errorf("claimed support %q", symbol)
		}
		if _, err := client.StockFundamentals(context.Background(), symbol); err == nil {
			t.Errorf("accepted symbol %q", symbol)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("requested unsupported symbols %d", calls.Load())
	}
	if NewFundamentalsClient(FundamentalsConfig{}).baseURL != fundamentalsDefaultURL {
		t.Fatal("bad default endpoint")
	}
}

func TestFundamentalsCancellationDeadlineAndBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client := NewFundamentalsClient(FundamentalsConfig{BaseURL: server.URL, HTTPClient: server.Client()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.StockFundamentals(ctx, "600519"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := client.StockFundamentals(ctx, "600519"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("parent deadline was enlarged")
	}
	budgetClient := NewFundamentalsClient(FundamentalsConfig{HTTPClient: &http.Client{Transport: financialTestTransport(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > fundamentalsBudget {
			t.Errorf("request budget missing/too long")
		}
		return nil, context.DeadlineExceeded
	})}})
	if _, err := budgetClient.StockFundamentals(context.Background(), "600519"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

type financialTestTransport func(*http.Request) (*http.Response, error)

func (f financialTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestFundamentalsResponseSizeHTTPAndEncoding(t *testing.T) {
	good := financialTestBody(financialTestItems())
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, contentType, body string
		status                  int
		chunked, success        bool
	}{
		{name: "BOM", body: "\xef\xbb\xbf" + good, success: true},
		{name: "GB18030", contentType: "application/json; charset=gb18030", body: string(encoded), success: true},
		{name: "invalid UTF8", contentType: "application/json; charset=utf-8", body: "\xff" + good},
		{name: "unknown charset", contentType: "application/json; charset=utf-16", body: good},
		{name: "too big declared", body: strings.Repeat(" ", fundamentalsMaxBody+1)},
		{name: "too big streamed", body: strings.Repeat(" ", fundamentalsMaxBody+1), chunked: true},
		{name: "http failure", body: good, status: 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				} else {
					w.Header().Set("Content-Type", "application/json")
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				if tc.chunked {
					w.(http.Flusher).Flush()
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := NewFundamentalsClient(FundamentalsConfig{BaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return fundamentalsTestNow }})
			_, err := client.StockFundamentals(context.Background(), "600519")
			if (err == nil) != tc.success {
				t.Fatalf("success=%v error=%v", tc.success, err)
			}
		})
	}
}

// This trimmed fixture preserves the exact consumed values of the anonymous
// sh601398 response sampled on 2026-10-03 (type=0, source=gjzb, num=4).
func TestFundamentalsRealBankFixture(t *testing.T) {
	body, err := os.ReadFile("fundamentals_fixture_bank.json")
	if err != nil {
		t.Fatal(err)
	}
	client, _ := financialTestClient(t, string(body))
	item, err := client.StockFundamentals(context.Background(), "601398")
	if err != nil {
		t.Fatal(err)
	}
	if item.NetProfit != 173682000000 || item.Revenue != 465859000000 || item.DeductedNetProfit != 173082000000 || item.OperatingCashFlowPerShare != 3.84115 || item.GrossMargin != 0 || item.FieldAvailable("gross_margin") || item.Meta.Partial || len(item.NotApplicableFields) != 1 {
		t.Fatalf("bad actual bank report %#v", item)
	}
}

func TestFundamentalsRealMaotaiAuditSample(t *testing.T) {
	// Optional local real-response regression; default tests never request the network.
	body, err := os.ReadFile("../../../../.runtime/em-replacement-audit-20261003/sina-finance-maotai.json")
	if os.IsNotExist(err) {
		t.Skip("local audit sample not present")
	}
	if err != nil {
		t.Fatal(err)
	}
	client, _ := financialTestClient(t, string(body))
	item, err := client.StockFundamentals(context.Background(), "600519")
	if err != nil {
		t.Fatal(err)
	}
	if item.NetProfit != 44516880421.86 || item.Revenue != 92278072083.21 || item.OperatingCashFlowPerShare != 56.548908 || !item.FieldAvailable("operating_cash_flow_per_share") || item.Meta.Partial {
		t.Fatalf("unexpected actual report %#v", item)
	}
}
