package sina

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type Client struct {
	baseURL                string
	kLineBaseURL           string
	moneyFlowBaseURL       string
	sectorMoneyFlowBaseURL string
	stockCatalogBaseURL    string
	httpClient             *http.Client
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func WithKLineBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.kLineBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithMoneyFlowBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.moneyFlowBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithSectorMoneyFlowBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.sectorMoneyFlowBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithStockCatalogBaseURL(baseURL string) Option {
	return func(c *Client) { c.stockCatalogBaseURL = strings.TrimRight(baseURL, "/") }
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:                "https://hq.sinajs.cn",
		kLineBaseURL:           "https://quotes.sina.cn/cn/api/jsonp_v2.php/callback/CN_MarketDataService.getKLineData",
		moneyFlowBaseURL:       "https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/MoneyFlow.ssl_bkzj_ssggzj",
		sectorMoneyFlowBaseURL: "https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/MoneyFlow.ssl_bkzj_bk",
		stockCatalogBaseURL:    "https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/Market_Center.getHQNodeData",
		httpClient:             &http.Client{Timeout: 10 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 120
	}
	scale := sinaKLineScale(period)
	if scale == "" {
		return nil, fmt.Errorf("unsupported period %q", period)
	}
	values := url.Values{}
	values.Set("symbol", normalized.Sina)
	values.Set("scale", scale)
	values.Set("ma", "no")
	values.Set("datalen", strconv.Itoa(limit))
	requestURL := c.kLineBaseURL + "?" + values.Encode()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &foundation.PriceHTTPStatusError{Provider: "sina", StatusCode: resp.StatusCode}
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	meta := foundation.SourceMeta{
		Source:    "sina",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	return parseKLineJSONP(decodeSinaBody(bodyBytes, resp.Header.Get("Content-Type")), normalized.Canonical, meta)
}

func (c *Client) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("symbols is required")
	}
	normalized := make([]foundation.Symbol, 0, len(symbols))
	sinaCodes := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		n, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, n)
		sinaCodes = append(sinaCodes, n.Sina)
	}

	values := url.Values{}
	values.Set("rn", strconv.FormatInt(time.Now().UnixMilli(), 10))
	values.Set("list", strings.Join(sinaCodes, ","))
	requestURL := c.requestURL(values)

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Host", "hq.sinajs.cn")
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sina http status %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	body := decodeSinaBody(bodyBytes, resp.Header.Get("Content-Type"))
	meta := foundation.SourceMeta{
		Source:    "sina",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	return parseRealtime(body, normalized, meta)
}

func (c *Client) requestURL(values url.Values) string {
	if strings.Contains(c.baseURL, "hq.sinajs.cn") {
		return c.baseURL + "/rn=" + values.Get("rn") + "&list=" + values.Get("list")
	}
	return c.baseURL + "?" + values.Encode()
}

func sinaKLineScale(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "day", "daily", "101", "240":
		return "240"
	case "week", "weekly", "102", "1200":
		return "1200"
	case "month", "monthly", "103", "7200":
		return "7200"
	case "1", "5", "15", "30", "60":
		return period
	default:
		return ""
	}
}

type sinaKLineItem struct {
	Day    string `json:"day"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
	Amount string `json:"amount"`
}

func parseKLineJSONP(body string, symbol string, meta foundation.SourceMeta) (result []foundation.KLine, parseErr error) {
	defer func() {
		if parseErr != nil && !errors.Is(parseErr, foundation.ErrPriceNoData) {
			parseErr = fmt.Errorf("%w: %v", foundation.ErrInvalidPriceData, parseErr)
		}
	}()
	trimmed := strings.TrimSpace(body)
	if trimmed == "null" || trimmed == "" || strings.HasSuffix(trimmed, "(null);") || strings.HasSuffix(trimmed, "(null)") {
		return nil, fmt.Errorf("%w: sina returned no requested kline series", foundation.ErrPriceNoData)
	}
	start := strings.Index(body, "[")
	end := strings.LastIndex(body, "]")
	if start < 0 || end < start {
		return nil, fmt.Errorf("%w: sina kline response has no JSON array", foundation.ErrInvalidPriceData)
	}
	var rawItems []sinaKLineItem
	if err := json.Unmarshal([]byte(body[start:end+1]), &rawItems); err != nil {
		return nil, err
	}
	items := make([]foundation.KLine, 0, len(rawItems))
	for _, raw := range rawItems {
		day, err := parseKLineTime(raw.Day)
		if err != nil {
			return nil, err
		}
		open, _ := strconv.ParseFloat(raw.Open, 64)
		high, _ := strconv.ParseFloat(raw.High, 64)
		low, _ := strconv.ParseFloat(raw.Low, 64)
		closePrice, _ := strconv.ParseFloat(raw.Close, 64)
		volume, _ := strconv.ParseFloat(raw.Volume, 64)
		if !isFiniteKLinePrice(open) || !isFiniteKLinePrice(high) || !isFiniteKLinePrice(low) || !isFiniteKLinePrice(closePrice) || high < math.Max(open, closePrice) || low > math.Min(open, closePrice) || math.IsNaN(volume) || math.IsInf(volume, 0) || volume < 0 {
			return nil, fmt.Errorf("%w: sina returned malformed OHLCV", foundation.ErrInvalidPriceData)
		}
		rowMeta := meta
		rowMeta.FieldsKnown, rowMeta.VolumeUnit, rowMeta.AmountCurrency = true, "shares", "CNY"
		rowMeta.AvailableFields = []string{"open", "high", "low", "close", "volume"}
		amount, amountErr := strconv.ParseFloat(raw.Amount, 64)
		if amountErr == nil && !math.IsNaN(amount) && !math.IsInf(amount, 0) && amount >= 0 {
			rowMeta.AvailableFields = append(rowMeta.AvailableFields, "amount")
		} else {
			amount = 0
		}
		items = append(items, foundation.KLine{
			Symbol: symbol,
			Time:   day,
			Open:   open,
			High:   high,
			Low:    low,
			Close:  closePrice,
			Volume: volume,
			Amount: amount,
			Meta:   rowMeta,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: sina returned no kline bars", foundation.ErrPriceNoData)
	}
	return items, nil
}

func isFiniteKLinePrice(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func parseKLineTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"2006/01/02",
	} {
		if parsed, err := time.ParseInLocation(layout, value, time.FixedZone("CST", 8*60*60)); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid kline time %q", value)
}

func decodeSinaBody(body []byte, contentTypes ...string) string {
	contentType := strings.ToLower(strings.Join(contentTypes, " "))
	if strings.Contains(contentType, "gb18030") || strings.Contains(contentType, "gbk") || strings.Contains(contentType, "gb2312") {
		decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(body)
		if err == nil {
			return string(decoded)
		}
	}
	if utf8.Valid(body) {
		return string(body)
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(body)
	if err != nil {
		return string(body)
	}
	return string(decoded)
}

func parseRealtime(body string, symbols []foundation.Symbol, meta foundation.SourceMeta) ([]foundation.Quote, error) {
	lines := strings.Split(body, ";")
	byCode := map[string]foundation.Symbol{}
	for _, symbol := range symbols {
		byCode[symbol.Sina] = symbol
	}
	quotes := make([]foundation.Quote, 0, len(symbols))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		left, payload, ok := strings.Cut(line, "\"")
		if !ok {
			continue
		}
		payload, _, _ = strings.Cut(payload, "\"")
		code := strings.TrimPrefix(strings.TrimSpace(left), "var hq_str_")
		code = strings.TrimSuffix(code, "=")
		symbol, ok := byCode[code]
		if !ok {
			continue
		}
		fields := strings.Split(payload, ",")
		if len(fields) < 32 || fields[0] == "" {
			continue
		}
		open, _ := strconv.ParseFloat(fields[1], 64)
		prevClose, _ := strconv.ParseFloat(fields[2], 64)
		price, _ := strconv.ParseFloat(fields[3], 64)
		high, _ := strconv.ParseFloat(fields[4], 64)
		low, _ := strconv.ParseFloat(fields[5], 64)
		change := price - prevClose
		changePercent := 0.0
		if prevClose != 0 {
			changePercent = change / prevClose * 100
		}
		optionalNumber := func(index int) *float64 {
			value, parseErr := strconv.ParseFloat(fields[index], 64)
			if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				return nil
			}
			return &value
		}
		levels := func(start int) []foundation.QuoteLevel {
			result := make([]foundation.QuoteLevel, 5)
			for index := range result {
				volume, price := optionalNumber(start+index*2), optionalNumber(start+index*2+1)
				if volume == nil || price == nil {
					return nil
				}
				result[index] = foundation.QuoteLevel{Price: *price, Volume: *volume}
			}
			return result
		}
		tradeTime, _ := time.ParseInLocation("2006-01-02 15:04:05", fields[30]+" "+fields[31], time.FixedZone("CST", 8*60*60))
		quotes = append(quotes, foundation.Quote{
			Symbol:        symbol.Canonical,
			Name:          fields[0],
			Price:         price,
			Open:          open,
			PreviousClose: prevClose,
			High:          high,
			Low:           low,
			Change:        change,
			ChangePercent: changePercent,
			TradeTime:     tradeTime,
			Volume:        optionalNumber(8),
			Amount:        optionalNumber(9),
			Bids:          levels(10),
			Asks:          levels(20),
			Meta:          meta,
		})
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("sina returned no quotes")
	}
	return quotes, nil
}
