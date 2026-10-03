package eastmoney

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
)

type Client struct {
	baseURL             string
	quoteBaseURL        string
	quoteFallbackURLs   []string
	dataBaseURL         string
	topicBaseURL        string
	datacenterBaseURL   string
	f10BaseURL          string
	announcementBaseURL string
	reportBaseURL       string
	thsBaseURL          string
	httpClient          *http.Client
	cooldownMu          sync.Mutex
	cooldowns           map[string]time.Time
	poolMu              sync.Mutex
	limitUpDays         map[string]*limitUpDayFlight
	catalogCacheMu      sync.RWMutex
	catalogMu           sync.Mutex
	catalog             []foundation.StockCatalogEntry
	catalogUntil        time.Time
	thsBillboardMu      sync.Mutex
	thsBillboardPages   map[string]thsBillboardPage
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithQuoteBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.quoteBaseURL = strings.TrimRight(baseURL, "/")
		c.quoteFallbackURLs = nil
	}
}

func WithQuoteFallbackBaseURLs(baseURLs ...string) Option {
	return func(c *Client) {
		c.quoteFallbackURLs = c.quoteFallbackURLs[:0]
		for _, baseURL := range baseURLs {
			if normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/"); normalized != "" {
				c.quoteFallbackURLs = append(c.quoteFallbackURLs, normalized)
			}
		}
	}
}

func WithDataBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.dataBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithTopicBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.topicBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithDatacenterBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.datacenterBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithF10BaseURL(baseURL string) Option {
	return func(c *Client) {
		c.f10BaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithAnnouncementBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.announcementBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithReportBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.reportBaseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithTHSBaseURL is primarily used by tests and local mirrors. Production
// requests use the public 同花顺 data-center page for seat classifications.
func WithTHSBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.thsBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:             "https://push2his.eastmoney.com",
		quoteBaseURL:        "https://push2.eastmoney.com",
		quoteFallbackURLs:   []string{"https://82.push2.eastmoney.com", "https://90.push2.eastmoney.com"},
		dataBaseURL:         "https://data.eastmoney.com",
		topicBaseURL:        "https://push2ex.eastmoney.com",
		datacenterBaseURL:   "https://datacenter-web.eastmoney.com",
		f10BaseURL:          "https://datacenter.eastmoney.com/securities",
		announcementBaseURL: "https://np-anotice-stock.eastmoney.com",
		reportBaseURL:       "https://reportapi.eastmoney.com",
		thsBaseURL:          "https://data.10jqka.com.cn",
		httpClient:          &http.Client{Timeout: 15 * time.Second},
		thsBillboardPages:   make(map[string]thsBillboardPage),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	return c.KLineAdjusted(ctx, symbol, period, limit, "qfq")
}

func (c *Client) KLineAdjusted(ctx context.Context, symbol string, period string, limit int, adjustment string) ([]foundation.KLine, error) {
	fqt := map[string]string{"none": "0", "qfq": "1", "hfq": "2"}[adjustment]
	if fqt == "" {
		return nil, fmt.Errorf("unsupported adjustment %q", adjustment)
	}
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 120
	}
	klt := eastMoneyPeriod(period)
	if klt == "" {
		return nil, fmt.Errorf("unsupported period %q", period)
	}

	endpoint := c.baseURL + "/api/qt/stock/kline/get"
	params := url.Values{}
	params.Set("secid", normalized.EastMoneySecID)
	params.Set("fields1", "f1,f2,f3,f4,f5,f6")
	params.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61")
	params.Set("klt", klt)
	params.Set("fqt", fqt)
	params.Set("end", "20500101")
	params.Set("lmt", strconv.Itoa(limit))
	params.Set("_", strconv.FormatInt(time.Now().UnixMilli(), 10))
	requestURL := endpoint + "?" + params.Encode()

	start := time.Now()
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			KLines []string `json:"klines"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 {
		return nil, fmt.Errorf("eastmoney rc=%d", payload.RC)
	}

	meta := foundation.SourceMeta{
		Source:    "eastmoney",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	items := make([]foundation.KLine, 0, len(payload.Data.KLines))
	for _, raw := range payload.Data.KLines {
		item, err := parseKLine(raw, normalized.Canonical, meta)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("eastmoney returned no kline bars")
	}
	return items, nil
}

// This is the sole retry owner for JSON GETs: at most three transport attempts,
// including the first. Backoff and origin-local 429 cooldown share the caller's
// deadline; malformed JSON and permanent HTTP failures never enter this loop.
func (c *Client) getJSONWithRetry(ctx context.Context, requestURL string, target any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return eastmoneyContextError(err, lastErr)
		}
		delay := time.Duration(0)
		if attempt > 0 {
			base := 150 * time.Millisecond * time.Duration(1<<(attempt-1))
			delay = base + time.Duration(rand.Int64N(int64(base/2)+1))
		}
		if err := c.waitJSONRetry(ctx, requestURL, delay); err != nil {
			return eastmoneyContextError(err, lastErr)
		}
		err := c.getJSON(ctx, requestURL, target)
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return eastmoneyContextError(ctx.Err(), err)
		}
		if !isTransient(err) {
			return err
		}
	}
	return lastErr
}

func eastmoneyContextError(err, previous error) error {
	kind := contracts.Canceled
	if errors.Is(err, context.DeadlineExceeded) {
		kind = contracts.TimedOut
	}
	typed := &contracts.Error{Kind: kind, SourceID: "eastmoney", Timeout: kind == contracts.TimedOut, Cause: errors.Join(err, previous)}
	var prior *contracts.Error
	if errors.As(previous, &prior) {
		typed.HTTPStatus, typed.RetryAfter = prior.HTTPStatus, prior.RetryAfter
	}
	return typed
}

func (c *Client) getJSON(ctx context.Context, requestURL string, target any) error {
	if err := ctx.Err(); err != nil {
		return eastmoneyContextError(err, nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return &contracts.Error{Kind: contracts.InvalidResponse, SourceID: "eastmoney", Cause: err}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	req.Header.Set("Referer", "https://quote.eastmoney.com/")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return eastmoneyContextError(ctx.Err(), err)
		}
		kind, timeout := contracts.UpstreamFailure, false
		var network net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
			kind, timeout = contracts.TimedOut, true
		} else if errors.Is(err, context.Canceled) {
			kind = contracts.Canceled
		}
		return &contracts.Error{Kind: kind, SourceID: "eastmoney", Cause: err, Timeout: timeout}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		kind := contracts.UpstreamFailure
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			kind = contracts.RateLimited
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			kind = contracts.Unauthorized
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			kind = contracts.InvalidResponse
		}
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		if kind == contracts.RateLimited {
			c.setJSONCooldown(requestURL, max(retryAfter, 150*time.Millisecond))
		}
		return &contracts.Error{Kind: kind, SourceID: "eastmoney", HTTPStatus: resp.StatusCode, RetryAfter: retryAfter, Cause: fmt.Errorf("eastmoney http status %d", resp.StatusCode)}
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		typed := &contracts.Error{Kind: contracts.InvalidResponse, SourceID: "eastmoney", HTTPStatus: resp.StatusCode, Cause: err}
		if ctx.Err() != nil {
			return eastmoneyContextError(ctx.Err(), typed)
		}
		var network net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
			typed.Kind, typed.Timeout = contracts.TimedOut, true
		} else if errors.Is(err, context.Canceled) {
			typed.Kind = contracts.Canceled
		}
		return typed
	}
	if err := ctx.Err(); err != nil {
		return eastmoneyContextError(err, nil)
	}
	return nil
}

func isTransient(err error) bool {
	var typed *contracts.Error
	if !errors.As(err, &typed) || errors.Is(err, context.Canceled) {
		return false
	}
	if typed.HTTPStatus != 0 {
		return typed.HTTPStatus == http.StatusTooManyRequests || (typed.HTTPStatus >= 500 && typed.HTTPStatus <= 599)
	}
	if typed.Kind != contracts.UpstreamFailure && typed.Kind != contracts.TimedOut {
		return false
	}
	var network net.Error
	return errors.Is(typed.Cause, io.EOF) || errors.Is(typed.Cause, io.ErrUnexpectedEOF) ||
		(errors.As(typed.Cause, &network) && (network.Timeout() || network.Temporary()))
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		// Saturate rather than overflow an untrusted header's seconds value.
		return time.Duration(min(seconds, int64((1<<63-1)/time.Second))) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}

func jsonOrigin(requestURL string) string {
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func (c *Client) setJSONCooldown(requestURL string, delay time.Duration) {
	c.cooldownMu.Lock()
	defer c.cooldownMu.Unlock()
	if c.cooldowns == nil {
		c.cooldowns = make(map[string]time.Time)
	}
	origin, until := jsonOrigin(requestURL), time.Now().Add(delay)
	if until.After(c.cooldowns[origin]) {
		c.cooldowns[origin] = until
	}
}

func (c *Client) waitJSONRetry(ctx context.Context, requestURL string, delay time.Duration) error {
	backoffUntil := time.Now().Add(delay)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.cooldownMu.Lock()
		until := maxTime(backoffUntil, c.cooldowns[jsonOrigin(requestURL)])
		c.cooldownMu.Unlock()
		remaining := time.Until(until)
		if remaining <= 0 {
			return nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		// A concurrent 429 may have extended the same origin's cooldown.
	}
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func eastMoneyPeriod(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "day", "daily", "101":
		return "101"
	case "week", "weekly", "102":
		return "102"
	case "month", "monthly", "103":
		return "103"
	case "1", "5", "15", "30", "60", "120":
		return period
	default:
		return ""
	}
}

func parseKLine(raw string, symbol string, meta foundation.SourceMeta) (foundation.KLine, error) {
	fields := strings.Split(raw, ",")
	if len(fields) < 7 {
		return foundation.KLine{}, fmt.Errorf("invalid eastmoney kline %q", raw)
	}
	day, err := parseKLineTime(fields[0])
	if err != nil {
		return foundation.KLine{}, err
	}
	numbers := make([]float64, 6)
	for index := range numbers {
		value, parseErr := strconv.ParseFloat(strings.TrimSpace(fields[index+1]), 64)
		if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return foundation.KLine{}, fmt.Errorf("invalid eastmoney required kline number")
		}
		numbers[index] = value
	}
	open, closePrice, high, low, volume, amount := numbers[0], numbers[1], numbers[2], numbers[3], numbers[4], numbers[5]
	if high < math.Max(open, closePrice) || low > math.Min(open, closePrice) || high < low || volume < 0 || amount < 0 {
		return foundation.KLine{}, fmt.Errorf("invalid eastmoney OHLCV structure")
	}
	meta.FieldsKnown = true
	meta.AvailableFields = []string{"open", "close", "high", "low", "volume", "amount"}
	changePercent, turnover := 0.0, 0.0
	optional := func(index int, name string) float64 {
		if len(fields) <= index {
			return 0
		}
		value, parseErr := strconv.ParseFloat(strings.TrimSpace(fields[index]), 64)
		if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return 0
		}
		meta.AvailableFields = append(meta.AvailableFields, name)
		return value
	}
	changePercent = optional(8, "change_percent")
	turnover = optional(10, "turnover_rate")
	return foundation.KLine{
		Symbol:        symbol,
		Time:          day,
		Open:          open,
		High:          high,
		Low:           low,
		Close:         closePrice,
		Volume:        volume,
		Amount:        amount,
		ChangePercent: changePercent,
		TurnoverRate:  turnover,
		Meta:          meta,
	}, nil
}

// parseKLineTime accepts both daily bars (YYYY-MM-DD) and intraday bars
// returned by Eastmoney (YYYY-MM-DD HH:MM[:SS]). A few upstream responses
// use slash-separated dates, so keep that format for resilient parsing too.
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
