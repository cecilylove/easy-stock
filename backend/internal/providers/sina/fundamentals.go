package sina

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const fundamentalsDefaultURL = "https://quotes.sina.cn/cn/api/openapi.php/CompanyFinanceService.getFinanceReport2022"
const fundamentalsMaxBody = 4 << 20
const fundamentalsBudget = 6 * time.Second

// FundamentalsConfig is independent of the legacy quote Client configuration.
type FundamentalsConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

type FundamentalsClient struct {
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
}

func NewFundamentalsClient(config FundamentalsConfig) *FundamentalsClient {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = fundamentalsDefaultURL
	}
	client := &http.Client{Timeout: fundamentalsBudget}
	if config.HTTPClient != nil {
		copy := *config.HTTPClient
		client = &copy
	}
	// Public API acquisition never borrows a caller's login cookies or follows
	// an upstream redirect to an authorization/challenge endpoint.
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &FundamentalsClient{baseURL: baseURL, httpClient: client, now: now}
}

// SupportsFundamentalsSymbol is deliberately narrower than NormalizeSymbol:
// only six-digit A shares, not indices, funds, HK symbols or guessed markets.
func SupportsFundamentalsSymbol(symbol string) bool {
	_, err := fundamentalsSymbol(symbol)
	return err == nil
}

func fundamentalsSymbol(input string) (foundation.Symbol, error) {
	symbol, err := foundation.NormalizeSymbol(input)
	if err != nil {
		return symbol, err
	}
	code := symbol.RawCode
	supported := len(code) == 6
	if supported {
		switch symbol.Market {
		case "SH":
			supported = strings.HasPrefix(code, "60") || strings.HasPrefix(code, "688") || strings.HasPrefix(code, "689")
		case "SZ":
			supported = strings.HasPrefix(code, "00") || strings.HasPrefix(code, "30")
		case "BJ":
			supported = strings.Contains("489", code[:1])
		default:
			supported = false
		}
	}
	if !supported {
		return foundation.Symbol{}, fmt.Errorf("sina fundamentals unsupported A-share symbol %q", input)
	}
	return symbol, nil
}

type fundamentalsReportDate struct {
	Date        string `json:"date_value"`
	Description string `json:"date_description"`
	Type        int    `json:"date_type"`
}

type fundamentalsItem struct {
	Field string          `json:"item_field"`
	Title string          `json:"item_title"`
	Value json.RawMessage `json:"item_value"`
	YoY   json.RawMessage `json:"item_tongbi"`
}

type fundamentalsReport struct {
	Type      string             `json:"rType"`
	Currency  string             `json:"rCurrency"`
	Published string             `json:"publish_date"`
	Items     []fundamentalsItem `json:"data"`
}

type fundamentalsNumber struct {
	value float64
	valid bool
}

func (c *FundamentalsClient) StockFundamentals(ctx context.Context, input string) (foundation.StockFundamentals, error) {
	symbol, err := fundamentalsSymbol(input)
	if err != nil {
		return foundation.StockFundamentals{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, fundamentalsBudget)
	defer cancel()
	start := time.Now()
	endpoint, err := url.Parse(c.baseURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals invalid endpoint")
	}
	query := endpoint.Query()
	query.Set("paperCode", symbol.Sina)
	query.Set("source", "gjzb")
	// This upstream mode is consolidated period-to-date, not the single-quarter mode.
	query.Set("type", "0")
	query.Set("page", "1")
	query.Set("num", "4")
	endpoint.RawQuery = query.Encode()
	requestURL := endpoint.String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return foundation.StockFundamentals{}, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals HTTP status %d", resp.StatusCode)
	}
	if resp.ContentLength > fundamentalsMaxBody {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals response too large")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, fundamentalsMaxBody+1))
	if err != nil {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals read: %w", err)
	}
	if len(body) > fundamentalsMaxBody {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals response too large")
	}
	body, err = fundamentalsDecode(body, resp.Header.Get("Content-Type"))
	if err != nil {
		return foundation.StockFundamentals{}, err
	}
	// Standard json.Unmarshal silently accepts contradictory duplicate object keys.
	if err = fundamentalsUniqueJSON(body); err != nil {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals JSON: %w", err)
	}
	var payload struct {
		Result *struct {
			Status *struct {
				Code *int `json:"code"`
			} `json:"status"`
			Data *struct {
				Dates   []fundamentalsReportDate      `json:"report_date"`
				Reports map[string]fundamentalsReport `json:"report_list"`
			} `json:"data"`
		} `json:"result"`
	}
	if err = json.Unmarshal(body, &payload); err != nil {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals decode: %w", err)
	}
	if payload.Result == nil || payload.Result.Status == nil || payload.Result.Status.Code == nil || *payload.Result.Status.Code != 0 || payload.Result.Data == nil {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals invalid status/data")
	}
	data := payload.Result.Data
	if len(data.Dates) == 0 || len(data.Reports) == 0 {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals empty reports")
	}
	now := c.now()
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	descriptions := make(map[string]fundamentalsReportDate, len(data.Dates))
	for _, date := range data.Dates {
		parsed, err := fundamentalsDate(date.Date, location)
		if err != nil {
			return foundation.StockFundamentals{}, err
		}
		// Calendar report ends and the upstream period type must agree. Unknown
		// report periods are not silently represented as consolidated cumulative.
		expectedEnds := map[int]string{1: "0331", 2: "0630", 3: "0930", 4: "1231"}
		if expectedEnds[date.Type] != parsed.Format("0102") {
			return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals invalid report period type")
		}
		if previous, exists := descriptions[date.Date]; exists && previous != date {
			return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals conflicting report dates")
		}
		descriptions[date.Date] = date
	}
	dates := make([]string, 0, len(data.Reports))
	for date := range data.Reports {
		if _, exists := descriptions[date]; !exists {
			return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals report key missing actual report_date")
		}
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	for _, key := range dates {
		reportDate, err := fundamentalsDate(key, location)
		if err != nil {
			return foundation.StockFundamentals{}, err
		}
		report := data.Reports[key]
		published, err := fundamentalsDate(report.Published, location)
		// Never infer publication from report end, data_source, or an unknown key.
		if err != nil {
			// An announced latest report with unknown disclosure timing is not
			// permission to silently label an older quarter as the latest snapshot.
			return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals latest disclosure date unavailable: %w", err)
		}
		if published.Before(reportDate) {
			return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals publication precedes report date")
		}
		if published.After(now) || reportDate.After(now) {
			continue
		}
		description := descriptions[key].Description
		if report.Type != "合并期末" || report.Currency != "CNY" || strings.Contains(description, "单季") || strings.Contains(description, "单季度") {
			return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals unsupported report basis/currency")
		}
		item, err := fundamentalsSnapshot(symbol.Canonical, reportDate.Format("2006-01-02"), description, published, report.Items)
		if err != nil {
			return foundation.StockFundamentals{}, err
		}
		if err = ctx.Err(); err != nil {
			return foundation.StockFundamentals{}, err
		}
		item.Meta = foundation.SourceMeta{
			Source: "sina:financials", SourceURL: requestURL, Provider: "sina", Capability: "fundamentals", ExecutionState: "fetched",
			FieldsKnown: true, AvailableFields: item.Meta.AvailableFields, Partial: item.Meta.Partial,
			FetchedAt: now, LatencyMS: time.Since(start).Milliseconds(), NativeCode: symbol.Sina,
			AmountCurrency: "CNY", TimeZone: "Asia/Shanghai", Period: "cumulative", BasisID: "consolidated:cumulative",
			AsOf: reportDate.Format("2006-01-02"),
		}
		return item, nil
	}
	return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals no published report as of now")
}

func fundamentalsDate(value string, location *time.Location) (time.Time, error) {
	if len(value) != 8 {
		return time.Time{}, fmt.Errorf("sina fundamentals invalid date %q", value)
	}
	date, err := time.ParseInLocation("20060102", value, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("sina fundamentals invalid date %q", value)
	}
	return date, nil
}

func fundamentalsNumeric(raw json.RawMessage, multiplier float64) fundamentalsNumber {
	text := strings.TrimSpace(string(raw))
	if len(text) > 0 && text[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return fundamentalsNumber{}
		}
		text = strings.TrimSpace(text)
	}
	value, err := strconv.ParseFloat(text, 64)
	value *= multiplier
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return fundamentalsNumber{}
	}
	return fundamentalsNumber{value, true}
}

func fundamentalsSnapshot(symbol, date, name string, published time.Time, items []fundamentalsItem) (foundation.StockFundamentals, error) {
	type metric struct {
		value, yoy fundamentalsNumber
		cashTitle  bool
	}
	metrics := make(map[string]metric)
	for _, raw := range items {
		if raw.Field == "" {
			continue
		}
		next := metric{value: fundamentalsNumeric(raw.Value, 1), yoy: fundamentalsNumeric(raw.YoY, 100), cashTitle: strings.Contains(raw.Title, "每股经营现金流") || strings.Contains(raw.Title, "每股经营活动现金流")}
		if previous, exists := metrics[raw.Field]; exists {
			if previous.value != next.value || previous.yoy != next.yoy {
				return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals contradictory metric %s", raw.Field)
			}
			next.cashTitle = next.cashTitle || previous.cashTitle
		}
		metrics[raw.Field] = next
	}
	revenue, profit := metrics["BIZTOTINCO"], metrics["PARENETP"]
	bankRevenue, bankProfit := metrics["BIZINCO"], metrics["NETPARECOMPPROF"]
	_, bankProfitKey := metrics["NETPARECOMPPROF"]
	_, bankRevenueKey := metrics["BIZINCO"]
	bank := bankProfitKey || bankRevenueKey
	if bank {
		if (revenue.value.valid && bankRevenue.value.valid && revenue.value != bankRevenue.value) || (revenue.yoy.valid && bankRevenue.yoy.valid && revenue.yoy != bankRevenue.yoy) {
			return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals contradictory revenue aliases")
		}
		if (profit.value.valid && bankProfit.value.valid && profit.value != bankProfit.value) || (profit.yoy.valid && bankProfit.yoy.valid && profit.yoy != bankProfit.yoy) {
			return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals contradictory profit aliases")
		}
		revenue, profit = bankRevenue, bankProfit
	}
	deducted := metrics["NPCUT"]
	if !revenue.value.valid && !profit.value.valid && !deducted.value.valid {
		return foundation.StockFundamentals{}, fmt.Errorf("sina fundamentals missing core values")
	}
	item := foundation.StockFundamentals{Symbol: symbol, ReportDate: date, ReportName: name, PublishedAt: published}
	fields := []struct {
		name   string
		number fundamentalsNumber
		target *float64
	}{
		{"revenue", revenue.value, &item.Revenue}, {"revenue_yoy", revenue.yoy, &item.RevenueYearOverYear},
		{"net_profit", profit.value, &item.NetProfit}, {"net_profit_yoy", profit.yoy, &item.NetProfitYearOverYear},
		{"deducted_net_profit", deducted.value, &item.DeductedNetProfit}, {"deducted_net_profit_yoy", deducted.yoy, &item.DeductedNetProfitYearOverYear},
		{"eps", metrics["EPSBASIC"].value, &item.EPS}, {"roe", metrics["ROEWEIGHTED"].value, &item.ROE},
		{"gross_margin", metrics["SGPMARGIN"].value, &item.GrossMargin}, {"debt_ratio", metrics["ASSLIABRT"].value, &item.DebtRatio},
		{"operating_cash_flow_per_share", metrics["OPNCFPS"].value, &item.OperatingCashFlowPerShare},
	}
	cash := metrics["OPNCFPS"]
	for _, field := range fields {
		if field.name == "operating_cash_flow_per_share" && !cash.cashTitle {
			continue
		}
		if field.name == "gross_margin" && bank {
			continue
		}
		if field.number.valid {
			*field.target = field.number.value
			item.Meta.AvailableFields = append(item.Meta.AvailableFields, field.name)
		}
	}
	if bank {
		item.NotApplicableFields = []string{"gross_margin"}
	}
	item.DeductedNetProfitAvailable = deducted.value.valid
	item.DeductedNetProfitYearOverYearAvailable = deducted.yoy.valid
	if deducted.value.valid {
		item.DeductedNetProfitReportDate = date
	}
	item.Meta.FieldsKnown = true
	item.Meta.Partial = len(item.Meta.AvailableFields)+len(item.NotApplicableFields) < len(fields)
	return item, nil
}

func fundamentalsDecode(body []byte, contentType string) ([]byte, error) {
	charset := ""
	if contentType != "" {
		_, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			return nil, fmt.Errorf("sina fundamentals invalid content type")
		}
		charset = strings.ToLower(params["charset"])
	}
	switch charset {
	case "", "utf-8", "utf8":
		body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
		if !utf8.Valid(body) {
			return nil, fmt.Errorf("sina fundamentals invalid UTF-8")
		}
	case "gbk", "gb18030", "gb2312":
		var err error
		body, err = simplifiedchinese.GB18030.NewDecoder().Bytes(body)
		if err != nil || bytes.Contains(body, []byte("\ufffd")) {
			return nil, fmt.Errorf("sina fundamentals invalid GB encoding")
		}
	default:
		return nil, fmt.Errorf("sina fundamentals unsupported charset %q", charset)
	}
	return body, nil
}

func fundamentalsUniqueJSON(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("JSON nesting too deep")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]bool)
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				// encoding/json matches typed struct keys case-insensitively too.
				key = strings.ToLower(key)
				if !ok || seen[key] {
					return fmt.Errorf("duplicate/invalid object key %v", token)
				}
				seen[key] = true
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON content")
	}
	return nil
}
