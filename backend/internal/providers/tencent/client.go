package tencent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type Client struct {
	quoteBaseURL          string
	klineBaseURL          string
	industryBaseURL       string
	industryStocksBaseURL string
	httpClient            *http.Client
}

type Option func(*Client)

func WithQuoteBaseURL(value string) Option {
	return func(c *Client) { c.quoteBaseURL = strings.TrimRight(value, "/") }
}

func WithKLineBaseURL(value string) Option {
	return func(c *Client) { c.klineBaseURL = strings.TrimRight(value, "/") }
}

func WithIndustryBaseURL(value string) Option {
	return func(c *Client) { c.industryBaseURL = strings.TrimRight(value, "/") }
}

func WithIndustryStocksBaseURL(value string) Option {
	return func(c *Client) { c.industryStocksBaseURL = strings.TrimRight(value, "/") }
}

func WithHTTPClient(value *http.Client) Option {
	return func(c *Client) {
		if value != nil {
			c.httpClient = value
		}
	}
}

func NewClient(options ...Option) *Client {
	client := &Client{
		quoteBaseURL:          "https://qt.gtimg.cn",
		klineBaseURL:          "https://web.ifzq.gtimg.cn/appstock/app/fqkline/get",
		industryBaseURL:       "https://proxy.finance.qq.com/ifzqgtimg/appstock/app/mktHs/rank",
		industryStocksBaseURL: "https://proxy.finance.qq.com/cgi/cgi-bin/rank/hs/getBoardRankList",
		httpClient:            &http.Client{Timeout: 12 * time.Second},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

type indexDefinition struct {
	ID       string
	QuoteKey string
	KLineKey string
	Code     string
	Name     string
	Region   string
	Market   string
	Currency string
	Core     bool
}

var indexCatalog = []indexDefinition{
	{ID: "sse", QuoteKey: "sh000001", KLineKey: "sh000001", Code: "000001", Name: "上证指数", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "szse", QuoteKey: "sz399001", KLineKey: "sz399001", Code: "399001", Name: "深证成指", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "chinext", QuoteKey: "sz399006", KLineKey: "sz399006", Code: "399006", Name: "创业板指", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "csi300", QuoteKey: "sh000300", KLineKey: "sh000300", Code: "000300", Name: "沪深300", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "sse50", QuoteKey: "sh000016", KLineKey: "sh000016", Code: "000016", Name: "上证50", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "csi1000", QuoteKey: "sh000852", KLineKey: "sh000852", Code: "000852", Name: "中证1000", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "star50", QuoteKey: "sh000688", KLineKey: "sh000688", Code: "000688", Name: "科创50", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "hsi", QuoteKey: "r_hkHSI", KLineKey: "hkHSI", Code: "HSI", Name: "恒生指数", Region: "中国香港", Market: "HK", Currency: "HKD", Core: true},
	{ID: "dow", QuoteKey: "usDJI", KLineKey: "usDJI", Code: ".DJI", Name: "道琼斯", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "sp500", QuoteKey: "usINX", KLineKey: "usINX", Code: ".INX", Name: "标普500", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	// Both identities were verified directly; legacy nasdaq always aliases NDX.
	{ID: "nasdaq100", QuoteKey: "usNDX", KLineKey: "usNDX", Code: ".NDX", Name: "纳斯达克100", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "nasdaq_composite", QuoteKey: "usIXIC", KLineKey: "usIXIC", Code: ".IXIC", Name: "纳斯达克综合", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "ftse", QuoteKey: "ukUKX", KLineKey: "ukUKX", Code: "UKX", Name: "英国富时100", Region: "欧洲", Market: "UK", Currency: "GBP"},
}

func (c *Client) MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	definitions := make([]indexDefinition, 0, len(indexCatalog))
	keys := make([]string, 0, len(indexCatalog))
	for _, definition := range indexCatalog {
		if scope == "core" && !definition.Core {
			continue
		}
		definitions = append(definitions, definition)
		keys = append(keys, definition.QuoteKey)
	}
	values := url.Values{}
	values.Set("q", strings.Join(keys, ","))
	requestURL := c.quoteBaseURL + "?" + values.Encode()
	start := time.Now()
	body, err := c.get(ctx, requestURL)
	if err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	meta := foundation.SourceMeta{Source: "tencent:index", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	byKey := parseTencentQuoteLines(body)
	items := make([]foundation.MarketIndexSnapshot, 0, len(definitions))
	for _, definition := range definitions {
		fields := byKey[definition.QuoteKey]
		if len(fields) < 33 || !validIndexNumber(fieldAt(fields, 3), true) || fieldAt(fields, 2) != definition.Code {
			continue
		}
		itemMeta := meta
		itemMeta.Provider, itemMeta.NativeCode, itemMeta.InstrumentID = "tencent", definition.QuoteKey, definition.ID
		itemMeta.TimeZone = "unknown"
		tradeTime := time.Time{}
		if definition.Market == "CN" || definition.Market == "HK" {
			tradeTime = parseTencentTradeTime(fieldAt(fields, 30))
			itemMeta.TimeZone = "Asia/Shanghai"
		}
		// Foreign wall clocks have no offset in the feed. Do not assume New York
		// (or the host timezone); preserve their raw timestamp for inspection.
		itemMeta.NativeTimestamp = fieldAt(fields, 30)
		change, changePercent := tencentChange(fields, false)
		items = append(items, foundation.MarketIndexSnapshot{
			ID: definition.ID, SecID: definition.QuoteKey, Code: firstString(fieldAt(fields, 2), definition.Code), Name: firstString(fieldAt(fields, 1), definition.Name),
			Region: definition.Region, Market: definition.Market, Currency: definition.Currency, Price: parseFloat(fieldAt(fields, 3)), Change: change, ChangePercent: changePercent,
			TradeTime: tradeTime, Status: tencentMarketStatus(definition.Market, tradeTime, time.Now()), Meta: itemMeta,
		})
	}
	if len(items) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("tencent returned no index snapshots")
	}
	return items, meta, nil
}

// SupportsIndexSeries separates unsupported input from an attempted upstream request.
func SupportsIndexSeries(id, period string) bool {
	_, ok := findIndex(id)
	period = strings.ToLower(strings.TrimSpace(period))
	return ok && (period == "" || period == "daily" || period == "day" || period == "week" || period == "month")
}

func (c *Client) MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error) {
	definition, ok := findIndex(id)
	if !ok {
		return foundation.MarketIndexSeries{}, fmt.Errorf("unsupported index %q", id)
	}
	period = strings.ToLower(strings.TrimSpace(period))
	if period == "" || period == "daily" {
		period = "day"
	}
	if period != "day" && period != "week" && period != "month" {
		return foundation.MarketIndexSeries{}, fmt.Errorf("unsupported period %q", period)
	}
	if limit <= 0 {
		limit = 120
	}
	values := url.Values{}
	values.Set("param", fmt.Sprintf("%s,%s,,,%d,", definition.KLineKey, period, min(limit, 500)))
	requestURL := c.klineBaseURL + "?" + values.Encode()
	start := time.Now()
	var payload struct {
		Code int `json:"code"`
		Data map[string]struct {
			Day    [][]any `json:"day"`
			Week   [][]any `json:"week"`
			Month  [][]any `json:"month"`
			QFQDay [][]any `json:"qfqday"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, requestURL, &payload); err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	if payload.Code != 0 {
		return foundation.MarketIndexSeries{}, fmt.Errorf("%w: tencent index kline code=%d", foundation.ErrInvalidPriceData, payload.Code)
	}
	raw := payload.Data[definition.KLineKey]
	rawLines := raw.Day
	if period == "week" {
		rawLines = raw.Week
	} else if period == "month" {
		rawLines = raw.Month
	}
	if len(rawLines) == 0 {
		return foundation.MarketIndexSeries{}, fmt.Errorf("%w: requested Tencent index series is empty", foundation.ErrPriceNoData)
	}
	meta := foundation.SourceMeta{Source: "tencent:index-kline", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(), Provider: "tencent", NativeCode: definition.KLineKey, InstrumentID: definition.ID, Period: period, TimeZone: "UTC", EffectiveAdjustment: "none", VolumeUnit: "provider_index_volume", FieldsKnown: true, AvailableFields: []string{"open", "close", "high", "low", "volume"}}
	// Index volume is provider-native aggregate volume, not a stock lot count.
	// Date-only bars are trading-session labels (UTC midnight), not instants.
	lines := make([]foundation.KLine, 0, len(rawLines))
	for _, values := range rawLines {
		if len(values) < 6 {
			continue
		}
		valid := true
		for i := 1; i <= 5; i++ {
			if !validIndexNumber(anyString(values[i]), i < 5) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		day, parseErr := time.ParseInLocation("2006-01-02", anyString(values[0]), time.UTC)
		if parseErr != nil {
			continue
		}
		closePrice := parseFloat(anyString(values[2]))
		open, high, low := parseFloat(anyString(values[1])), parseFloat(anyString(values[3])), parseFloat(anyString(values[4]))
		if high < math.Max(open, closePrice) || low > math.Min(open, closePrice) || low > high {
			continue
		}
		lines = append(lines, foundation.KLine{Symbol: definition.ID, Time: day, Open: open, Close: closePrice, High: high, Low: low, Volume: parseFloat(anyString(values[5])), Meta: meta})
	}
	if len(lines) == 0 {
		return foundation.MarketIndexSeries{}, fmt.Errorf("%w: tencent returned no valid index bars", foundation.ErrInvalidPriceData)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Time.Before(lines[j].Time) })
	for i := range lines {
		lines[i].ChangePercent = 0
		if i > 0 {
			if !lines[i].Time.After(lines[i-1].Time) {
				return foundation.MarketIndexSeries{}, fmt.Errorf("%w: tencent duplicate index bar", foundation.ErrInvalidPriceData)
			}
			lines[i].ChangePercent = (lines[i].Close/lines[i-1].Close - 1) * 100
			lines[i].Meta.AvailableFields = append(append([]string(nil), meta.AvailableFields...), "change_percent")
		}
	}
	latest := lines[len(lines)-1]
	index := foundation.MarketIndexSnapshot{ID: definition.ID, SecID: definition.QuoteKey, Code: definition.Code, Name: definition.Name, Region: definition.Region, Market: definition.Market, Currency: definition.Currency, Price: latest.Close, ChangePercent: latest.ChangePercent, TradeTime: latest.Time, Status: "closed", Meta: latest.Meta}
	return foundation.MarketIndexSeries{Index: index, Lines: lines, Meta: meta}, nil
}

func (c *Client) get(ctx context.Context, requestURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Referer", "https://stockapp.finance.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tencent http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	decoded, decodeErr := simplifiedchinese.GB18030.NewDecoder().Bytes(body)
	if decodeErr == nil {
		body = decoded
	}
	return string(body), nil
}

func (c *Client) getJSON(ctx context.Context, requestURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "https://gu.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &foundation.PriceHTTPStatusError{Provider: "tencent", StatusCode: resp.StatusCode}
	}
	err = json.NewDecoder(resp.Body).Decode(target)
	if err == io.EOF {
		return fmt.Errorf("%w: Tencent price response has no JSON payload", foundation.ErrPriceNoData)
	}
	return err
}

func parseTencentQuoteLines(body string) map[string][]string {
	result := map[string][]string{}
	for _, line := range strings.Split(body, ";") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		left, value, ok := strings.Cut(line, "=\"")
		if !ok {
			continue
		}
		key := strings.TrimPrefix(left, "v_")
		result[key] = strings.Split(strings.TrimSuffix(value, "\""), "~")
	}
	return result
}

func tencentChange(fields []string, simple bool) (float64, float64) {
	if simple {
		return parseFloat(fieldAt(fields, 4)), parseFloat(fieldAt(fields, 5))
	}
	return parseFloat(fieldAt(fields, 31)), parseFloat(fieldAt(fields, 32))
}

func tencentMarketStatus(_ string, tradeTime time.Time, now time.Time) string {
	if tradeTime.IsZero() {
		return "unknown"
	}
	if !tradeTime.IsZero() && now.Sub(tradeTime) >= 0 && now.Sub(tradeTime) <= 20*time.Minute {
		return "open"
	}
	return "closed"
}

func findIndex(id string) (indexDefinition, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	switch id {
	case "nasdaq", "ndx":
		id = "nasdaq100"
	case "ixic":
		id = "nasdaq_composite"
	}
	for _, item := range indexCatalog {
		if item.ID == strings.ToLower(strings.TrimSpace(id)) {
			return item, true
		}
	}
	return indexDefinition{}, false
}

func fieldAt(fields []string, index int) string {
	if index < 0 || index >= len(fields) {
		return ""
	}
	return strings.TrimSpace(fields[index])
}

func parseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return parsed
}

func parseTencentTradeTime(value string) time.Time {
	for _, layout := range []string{"2006/01/02 15:04:05", "2006-01-02 15:04:05", "20060102150405"} {
		if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(value), time.FixedZone("Asia/Shanghai", 8*60*60)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func validIndexNumber(value string, positive bool) bool {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && ((!positive && n >= 0) || (positive && n > 0))
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
