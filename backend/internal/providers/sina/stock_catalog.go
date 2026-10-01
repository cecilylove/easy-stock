package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

const stockCatalogPageSize = 100

type stockCatalogPage struct {
	rows []map[string]any
	url  string
	err  error
}

// A bounded batch keeps initial directory loading within the handler budget.
// Results are consumed in page order and never published as a partial catalog.
func (c *Client) stockCatalogPages(ctx context.Context, start int) []stockCatalogPage {
	count := min(4, 101-start)
	results := make([]stockCatalogPage, count)
	type result struct {
		index int
		page  stockCatalogPage
	}
	completed := make(chan result, count)
	for index := 0; index < count; index++ {
		go func(index int) {
			rows, requestURL, err := c.stockCatalogPage(ctx, start+index)
			completed <- result{index, stockCatalogPage{rows, requestURL, err}}
		}(index)
	}
	for range count {
		select {
		case entry := <-completed:
			results[entry.index] = entry.page
		case <-ctx.Done():
			return []stockCatalogPage{{err: ctx.Err()}}
		}
	}
	return results
}

// StockCatalog is a symbol/name directory for Sina's hs_a node (Shanghai and
// Shenzhen A shares). It does not supply industry/concept membership or claim
// coverage of Beijing. A partial page sequence is never cached as a full list.
func (c *Client) StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error) {
	start := time.Now()
	items := make([]foundation.StockCatalogEntry, 0, 5600)
	seen := make(map[string]bool)
	seenPages := make(map[string]bool)
	for startPage := 1; startPage <= 100; startPage += 4 {
		for offset, result := range c.stockCatalogPages(ctx, startPage) {
			page := startPage + offset
			rows, requestURL := result.rows, result.url
			if result.err != nil {
				return nil, result.err
			}
			// Pagination progress is independent of the supported output markets:
			// hs_a can include an entire Beijing-only page before SH/SZ symbols.
			pageSymbols := make([]string, 0, len(rows))
			for _, row := range rows {
				pageSymbols = append(pageSymbols, strings.ToLower(strings.TrimSpace(sinaString(row["symbol"]))))
			}
			fingerprint := strings.Join(pageSymbols, ",")
			if len(rows) == stockCatalogPageSize && seenPages[fingerprint] {
				return nil, fmt.Errorf("sina stock directory pagination did not advance at page %d", page)
			}
			seenPages[fingerprint] = true
			for _, row := range rows {
				rawSymbol := strings.ToLower(strings.TrimSpace(sinaString(row["symbol"])))
				if (!strings.HasPrefix(rawSymbol, "sh") && !strings.HasPrefix(rawSymbol, "sz")) || !isAStockMoneyFlowSymbol(rawSymbol) {
					continue
				}
				symbol, err := foundation.NormalizeSymbol(rawSymbol)
				name := strings.TrimSpace(sinaString(row["name"]))
				if err != nil || name == "" || seen[symbol.Canonical] {
					continue
				}
				seen[symbol.Canonical] = true
				items = append(items, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{
					Symbol: symbol.Canonical, Name: name, Price: parseSinaFloat(row["trade"]),
					Change: parseSinaFloat(row["pricechange"]), ChangePercent: parseSinaFloat(row["changepercent"]),
					Volume: parseSinaFloat(row["volume"]), Amount: parseSinaFloat(row["amount"]),
					Meta: foundation.SourceMeta{Source: "sina:stock-directory", SourceURL: requestURL},
				}})
			}
			if len(rows) < stockCatalogPageSize {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if len(items) == 0 {
					return nil, fmt.Errorf("sina stock directory returned no Shanghai/Shenzhen A shares")
				}
				fetchedAt := time.Now()
				for i := range items {
					items[i].Meta.FetchedAt = fetchedAt
					items[i].Meta.LatencyMS = time.Since(start).Milliseconds()
					items[i].Meta.AvailableFields = []string{"symbol", "name", "price", "change_percent"}
				}
				return items, nil
			}
		}
	}
	return nil, fmt.Errorf("sina stock directory exceeded pagination limit")
}

func (c *Client) stockCatalogPage(ctx context.Context, page int) ([]map[string]any, string, error) {
	values := url.Values{"page": {strconv.Itoa(page)}, "num": {strconv.Itoa(stockCatalogPageSize)}, "node": {"hs_a"}, "sort": {"symbol"}, "asc": {"1"}}
	requestURL := c.stockCatalogBaseURL + "?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, requestURL, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, requestURL, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, requestURL, fmt.Errorf("sina stock directory http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, requestURL, err
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(decodeSinaBody(body, resp.Header.Get("Content-Type"))), &rows); err != nil {
		return nil, requestURL, fmt.Errorf("decode sina stock directory: %w", err)
	}
	return rows, requestURL, nil
}
