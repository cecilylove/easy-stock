// Package ths contains independent public Tonghuashun acquisition adapters.
package ths

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/runtime"
	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	billboardCacheTTL   = 12 * time.Hour
	billboardCachePages = 32
	billboardHTTPBudget = 8 * time.Second
	billboardBodyLimit  = 12 << 20
)

var (
	stockBlockPattern = regexp.MustCompile(`(?is)<div\s+class=["']stockcont["'][^>]*stockcode=["']?([0-9]{6})["']?[^>]*>`)
	seatCellPattern   = regexp.MustCompile(`(?is)<td\s+class=["']tl\s+rel["'][^>]*>(.*?)</td>`)
	seatNamePattern   = regexp.MustCompile(`(?is)<a[^>]*\stitle=["']([^"']+)["'][^>]*>`)
	labelPattern      = regexp.MustCompile(`(?is)<label[^>]*class=["'][^"']*label[^"']*["'][^>]*>([^<]+)</label>`)
	symbolPattern     = regexp.MustCompile(`^[0-9]{6}(?:\.(?:SH|SZ|BJ))?$`)
)

type billboardPage struct {
	document string
	meta     foundation.SourceMeta
	expires  time.Time
}
type billboardFlight struct {
	done     chan struct{}
	page     billboardPage
	err      error
	cancel   context.CancelFunc
	waiters  int
	observed bool
}

// BillboardLabelClient owns a bounded date-page cache. No detail-data provider
// or business merge is called here; labels are an independently routed capability.
type BillboardLabelClient struct {
	http    *http.Client
	baseURL string
	now     func() time.Time
	mu      sync.Mutex
	pages   map[string]billboardPage
	flights map[string]*billboardFlight
}

var _ contracts.BillboardLabelProvider = (*BillboardLabelClient)(nil)

func NewBillboardLabelClient() *BillboardLabelClient {
	return &BillboardLabelClient{http: &http.Client{Timeout: billboardHTTPBudget}, baseURL: "https://data.10jqka.com.cn", now: time.Now, pages: make(map[string]billboardPage), flights: make(map[string]*billboardFlight)}
}

func (c *BillboardLabelClient) Fetch(ctx context.Context, symbol, tradeDate string) (map[string]string, foundation.SourceMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, foundation.SourceMeta{ExecutionState: "skipped"}, err
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if !symbolPattern.MatchString(symbol) {
		return nil, foundation.SourceMeta{ExecutionState: "skipped"}, fmt.Errorf("invalid billboard symbol %q", symbol)
	}
	date, err := time.Parse("2006-01-02", tradeDate)
	if err != nil || date.Format("2006-01-02") != tradeDate {
		return nil, foundation.SourceMeta{ExecutionState: "skipped"}, fmt.Errorf("invalid billboard trade date %q", tradeDate)
	}
	page, err := c.fetchPage(ctx, tradeDate)
	if err != nil {
		return nil, page.meta, err
	}
	if err := ctx.Err(); err != nil {
		return nil, page.meta, err
	}
	// Parsing per access creates a private map; callers never mutate cached data.
	return parseBillboardSeatLabels(page.document, strings.Split(symbol, ".")[0]), page.meta, nil
}

func (c *BillboardLabelClient) fetchPage(ctx context.Context, date string) (billboardPage, error) {
	c.mu.Lock()
	now := c.now()
	for key, page := range c.pages {
		if !now.Before(page.expires) {
			delete(c.pages, key)
		}
	}
	if page, ok := c.pages[date]; ok {
		c.mu.Unlock()
		page.meta.ExecutionState = "cache"
		return page, nil
	}
	flight, ok := c.flights[date]
	if !ok {
		// One viewer's cancellation must not abort another viewer's request.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), billboardHTTPBudget)
		flight = &billboardFlight{done: make(chan struct{}), cancel: cancel}
		c.flights[date] = flight
		go c.completePage(fetchCtx, date, flight)
	}
	flight.waiters++
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		c.mu.Lock()
		flight.waiters--
		if flight.waiters == 0 {
			flight.cancel()
			if c.flights[date] == flight {
				delete(c.flights, date)
			}
		}
		c.mu.Unlock()
		return billboardPage{meta: foundation.SourceMeta{ExecutionState: "skipped"}}, ctx.Err()
	case <-flight.done:
		c.mu.Lock()
		flight.waiters--
		page, err := flight.page, flight.err
		if flight.observed {
			page.meta.ExecutionState = "joined"
		} else {
			flight.observed = true
		}
		c.mu.Unlock()
		return page, err
	}
}

func (c *BillboardLabelClient) completePage(ctx context.Context, date string, flight *billboardFlight) {
	defer flight.cancel()
	page, err := c.requestPage(ctx, date)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && ctx.Err() == nil && c.flights[date] == flight {
		if len(c.pages) >= billboardCachePages {
			oldestKey := ""
			var oldest time.Time
			for key, candidate := range c.pages {
				if oldestKey == "" || candidate.meta.FetchedAt.Before(oldest) {
					oldestKey, oldest = key, candidate.meta.FetchedAt
				}
			}
			delete(c.pages, oldestKey)
		}
		c.pages[date] = page
	}
	flight.page, flight.err = page, err
	if c.flights[date] == flight {
		delete(c.flights, date)
	}
	close(flight.done)
}

func (c *BillboardLabelClient) requestPage(ctx context.Context, date string) (billboardPage, error) {
	requestURL := fmt.Sprintf("%s/ifmarket/lhbggxq/report/%s/", strings.TrimRight(c.baseURL, "/"), date)
	meta := foundation.SourceMeta{Source: "ths:billboard-labels", SourceURL: requestURL, Provider: "ths", Capability: "billboard-labels", TradeDate: date, ExecutionState: "fetched"}
	start := c.now()
	ctx, cancel := runtime.Budget(ctx, billboardHTTPBudget)
	defer cancel()
	fail := func(err error) (billboardPage, error) {
		meta.FetchedAt = c.now()
		meta.LatencyMS = meta.FetchedAt.Sub(start).Milliseconds()
		return billboardPage{meta: meta}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	req.Header.Set("Referer", "https://data.10jqka.com.cn/market/longhu/")
	resp, err := c.http.Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("ths billboard labels http status %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, billboardBodyLimit+1))
	if err != nil {
		return fail(err)
	}
	if len(body) > billboardBodyLimit {
		return fail(fmt.Errorf("ths billboard labels response exceeds size budget"))
	}
	if !utf8.Valid(body) {
		body, _, err = transform.Bytes(simplifiedchinese.GBK.NewDecoder(), body)
		if err != nil {
			return fail(err)
		}
	}
	document := string(body)
	if !stockBlockPattern.MatchString(document) {
		return fail(&contracts.Error{Kind: contracts.InvalidResponse, SourceID: "ths", Capability: "billboard-labels", Cause: fmt.Errorf("report contains no stock blocks")})
	}
	meta.FetchedAt = c.now()
	meta.LatencyMS = meta.FetchedAt.Sub(start).Milliseconds()
	return billboardPage{document: document, meta: meta, expires: meta.FetchedAt.Add(billboardCacheTTL)}, nil
}

func parseBillboardSeatLabels(document, code string) map[string]string {
	result := map[string]string{}
	blocks := stockBlockPattern.FindAllStringSubmatchIndex(document, -1)
	for index, block := range blocks {
		if len(block) < 4 || document[block[2]:block[3]] != code {
			continue
		}
		end := len(document)
		if index+1 < len(blocks) {
			end = blocks[index+1][0]
		}
		for _, cell := range seatCellPattern.FindAllStringSubmatch(document[block[1]:end], -1) {
			nameMatch := seatNamePattern.FindStringSubmatch(cell[1])
			if len(nameMatch) < 2 {
				continue
			}
			name := NormalizeBillboardSeatName(html.UnescapeString(strings.TrimSpace(nameMatch[1])))
			label := ""
			if match := labelPattern.FindStringSubmatch(cell[1]); len(match) >= 2 {
				label = strings.TrimSpace(html.UnescapeString(match[1]))
			}
			if strings.Contains(name, "机构专用") {
				label = "机构"
			}
			if name != "" && label != "" {
				result[name] = label
			}
		}
	}
	return result
}

// NormalizeBillboardSeatName preserves the legacy cross-platform seat matching
// semantics; it does not infer a seat's institution status or funding identity.
func NormalizeBillboardSeatName(value string) string {
	return foundation.NormalizeBillboardSeatName(value)
}
