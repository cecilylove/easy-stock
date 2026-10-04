package sina

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"golang.org/x/net/html"
)

const (
	annOrigin  = "https://vip.stock.finance.sina.com.cn"
	annBudget  = 10 * time.Second
	annMaxHTML = 2 << 20
)

// AnnouncementConfig overrides the origin for offline transports/tests; public
// URLs returned to callers always use the verified Sina origin and paths.
// A URL template containing {code} is also accepted for the first list page.
type AnnouncementConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}
type AnnouncementClient struct {
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
}

func NewAnnouncementClient(config AnnouncementConfig) *AnnouncementClient {
	base := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if base == "" {
		base = annOrigin
	}
	client := &http.Client{Timeout: annBudget}
	if config.HTTPClient != nil {
		copy := *config.HTTPClient
		client = &copy
	}
	if client.Timeout <= 0 || client.Timeout > annBudget {
		client.Timeout = annBudget
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &AnnouncementClient{baseURL: base, httpClient: client, now: now}
}

// MarketAnnouncements supports verified A-share company lists only. The public
// BulletinGather page exposes CompanyCode, not exchange-qualified stock identity;
// it is deliberately Unsupported until that mapping and coverage are verified.
// Titles/known report types are filtered locally, scanning at most 4 x 30 rows.
// MissingIDs ending in :body are text quality issues; listing:* is coverage loss.
func (c *AnnouncementClient) MarketAnnouncements(ctx context.Context, query, symbol, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, foundation.SourceMeta{}, annError(contracts.Unsupported, "all-market BulletinGather identity/coverage is not verified", nil)
	}
	normalized, err := businessSymbol(symbol)
	if err != nil {
		return nil, foundation.SourceMeta{}, annError(contracts.Unsupported, "unsupported A-share market/code", err)
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	ctx, cancel := context.WithTimeout(ctx, annBudget)
	defer cancel()
	start := time.Now()
	listURL := annOrigin + "/corp/go.php/vCB_AllBulletin/stockid/" + normalized.RawCode + ".phtml"
	meta := foundation.SourceMeta{Source: "sina:announcements", Provider: "sina", Capability: "announcement", SourceURL: listURL, NativeCode: normalized.Sina, InstrumentID: normalized.Canonical, TimeZone: "Asia/Shanghai", ExecutionState: "fetched", FieldsKnown: true, AvailableFields: []string{"title", "stock_name", "symbol", "published_at", "id", "url"}, RequestedSort: "date_desc", EffectiveSort: "date_desc"}
	items := make([]foundation.MarketResearchItem, 0, limit)
	seen := map[string]foundation.MarketResearchItem{}
	query = strings.ToLower(strings.TrimSpace(query))
	category = strings.TrimSpace(category)
	requestURL := c.firstURL(normalized.RawCode)
	previousDate := time.Time{}
	for page := 1; page <= 4; page++ {
		document, fetchErr := annFetchHTML(ctx, c.httpClient, requestURL)
		if fetchErr != nil {
			meta.Partial = true
			meta.MissingIDs = append(meta.MissingIDs, fmt.Sprintf("listing:page-%d", page))
			err = fetchErr
			break
		}
		rows, next, parseErr := annParseList(document, normalized, page)
		if ctx.Err() != nil {
			parseErr = annError(contracts.Kind(ctx.Err()), "listing parse budget/cancellation", ctx.Err())
		}
		if parseErr != nil {
			meta.Partial = true
			meta.MissingIDs = append(meta.MissingIDs, fmt.Sprintf("listing:page-%d", page))
			err = parseErr
			break
		}
		progress := 0
		for _, item := range rows {
			if item.PublishedAt.After(c.now()) {
				meta.Partial = true
				meta.MissingIDs = append(meta.MissingIDs, "listing:future-date")
				err = annError(contracts.InvalidResponse, "future announcement date", nil)
				break
			}
			if previous, exists := seen[item.ID]; exists {
				if previous.Title != item.Title || !previous.PublishedAt.Equal(item.PublishedAt) || previous.URL != item.URL {
					meta.Partial = true
					meta.MissingIDs = append(meta.MissingIDs, "listing:conflicting-duplicate")
					err = annError(contracts.InvalidResponse, "conflicting duplicate announcement identity", nil)
					break
				}
				continue
			}
			seen[item.ID] = item
			progress++
			if !previousDate.IsZero() && item.PublishedAt.After(previousDate) {
				meta.Partial = true
				meta.MissingIDs = append(meta.MissingIDs, "listing:date-order")
				err = annError(contracts.InvalidResponse, "list is not date-descending", nil)
				break
			}
			previousDate = item.PublishedAt
			if query != "" && !strings.Contains(strings.ToLower(item.Title), query) {
				continue
			}
			if category != "" && category != "all" && !strings.Contains(item.Title, category) && (item.Category == "" || !strings.Contains(item.Category, category)) {
				continue
			}
			items = append(items, item)
		}
		if err != nil {
			break
		}
		if len(items) >= limit {
			break
		}
		if progress == 0 && (next != "" || (page > 1 && len(rows) > 0)) {
			meta.Partial = true
			meta.MissingIDs = append(meta.MissingIDs, "listing:no-progress")
			err = annError(contracts.InvalidResponse, "pagination made no progress", nil)
			break
		}
		if next == "" {
			break
		}
		if page == 4 {
			meta.Partial = true
			meta.MissingIDs = append(meta.MissingIDs, "listing:page-cap")
			break
		}
		requestURL = c.transportURL(next)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].PublishedAt.After(items[j].PublishedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	meta.QueryCoverage = "complete" // requested latest-N result, not an all-history claim
	if meta.Partial {
		meta.QueryCoverage = "bounded"
	}
	meta.FetchedAt = c.now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	for i := range items {
		items[i].Meta = meta
		items[i].Meta.SourceURL = items[i].URL
		items[i].Meta.NativeTimestamp = items[i].PublishedAt.Format("2006-01-02")
		if items[i].Category != "" {
			items[i].Meta.AvailableFields = append(append([]string(nil), items[i].Meta.AvailableFields...), "category")
		}
		items[i].ContentStatus = "unavailable"
		items[i].ContentScope = "list-only"
	}
	jobs := make(chan int, len(items))
	for i := range items {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	var stopBodies atomic.Bool
	if contracts.Kind(err) == contracts.RateLimited || contracts.Kind(err) == contracts.Unauthorized {
		stopBodies.Store(true)
	}
	for worker := 0; worker < min(3, len(items)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				item := &items[i]
				if ctx.Err() != nil || stopBodies.Load() {
					item.ContentIssue = "正文预算耗尽、请求取消或上游限流/鉴权拦截，未取得正文"
					item.Meta.Partial = true
					continue
				}
				document, bodyErr := annFetchHTML(ctx, c.httpClient, c.transportURL(item.URL))
				if bodyErr == nil {
					item.Content, bodyErr = annParseDetail(document, *item, normalized)
				}
				if ctx.Err() != nil {
					bodyErr = annError(contracts.Kind(ctx.Err()), "body parse budget/cancellation", ctx.Err())
				}
				if contracts.Kind(bodyErr) == contracts.RateLimited || contracts.Kind(bodyErr) == contracts.Unauthorized {
					stopBodies.Store(true)
				}
				if bodyErr != nil || strings.TrimSpace(item.Content) == "" {
					item.Content = ""
					item.ContentIssue = "正文未取得，不能视为阅读全文或据此排除风险"
					item.Meta.Partial = true
					continue
				}
				item.ContentStatus = "available"
				item.ContentScope = "readable-text"
				item.Meta.AvailableFields = append(append([]string(nil), item.Meta.AvailableFields...), "content")
				runes := []rune(item.Content)
				if len(runes) > 8000 {
					item.Content = string(runes[:8000])
					item.ContentStatus = "truncated"
					item.ContentScope = "truncated-text"
					item.ContentIssue = "正文已截断为8000字，不等于PDF附件全文"
					item.Meta.Partial = true
				}
			}
		}()
	}
	wg.Wait()
	for _, item := range items {
		if item.ContentStatus != "available" {
			meta.Partial = true
			meta.MissingIDs = append(meta.MissingIDs, item.ID+":body")
		}
	}
	meta.LatencyMS = time.Since(start).Milliseconds()
	// A body-stage deadline is a per-item text quality loss, not a failed
	// listing source. Explicit caller cancellation still retains its semantics.
	if ctx.Err() == context.Canceled && err == nil {
		err = annError(contracts.Canceled, "announcement request cancellation", ctx.Err())
	}
	if len(items) == 0 && meta.Partial && err == nil {
		err = annError(contracts.InvalidResponse, "filtered list coverage is incomplete; empty does not mean no announcements", nil)
	}
	return items, meta, err
}

func (c *AnnouncementClient) firstURL(code string) string {
	if strings.Contains(c.baseURL, "{code}") {
		return strings.ReplaceAll(c.baseURL, "{code}", code)
	}
	return c.baseURL + "/corp/go.php/vCB_AllBulletin/stockid/" + code + ".phtml"
}
func (c *AnnouncementClient) transportURL(publicURL string) string {
	target, _ := url.Parse(publicURL)
	base, _ := url.Parse(c.baseURL)
	if base != nil && target != nil {
		target.Scheme = base.Scheme
		target.Host = base.Host
		return target.String()
	}
	return publicURL
}
func annError(kind contracts.ErrorKind, message string, cause error) error {
	if cause == nil {
		cause = errors.New(message)
	} else {
		cause = fmt.Errorf("%s: %w", message, cause)
	}
	return &contracts.Error{Kind: kind, SourceID: "sina", Capability: "announcement", Cause: cause, Timeout: kind == contracts.TimedOut}
}
func annFetchHTML(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", annError(contracts.InvalidResponse, "invalid disclosure URL", err)
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := client.Do(req)
	if err != nil {
		kind := contracts.Kind(err)
		var timeout interface{ Timeout() bool }
		if kind == contracts.UpstreamFailure && errors.As(err, &timeout) && timeout.Timeout() {
			kind = contracts.TimedOut
		}
		return "", annError(kind, "disclosure fetch failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		kind := contracts.UpstreamFailure
		switch {
		case resp.StatusCode == 429:
			kind = contracts.RateLimited
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			kind = contracts.Unauthorized
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			kind = contracts.Unauthorized
		case resp.StatusCode == 404 || resp.StatusCode == 410:
			kind = contracts.NoData
		}
		return "", &contracts.Error{Kind: kind, SourceID: "sina", Capability: "announcement", HTTPStatus: resp.StatusCode}
	}
	if resp.ContentLength > annMaxHTML {
		return "", annError(contracts.InvalidResponse, "disclosure exceeds 2 MiB", nil)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, annMaxHTML+1))
	if err != nil {
		return "", annError(contracts.Kind(err), "disclosure read failed", err)
	}
	if len(body) > annMaxHTML {
		return "", annError(contracts.InvalidResponse, "disclosure exceeds 2 MiB", nil)
	}
	if ctx.Err() != nil {
		return "", annError(contracts.Kind(ctx.Err()), "disclosure canceled", ctx.Err())
	}
	contentType := resp.Header.Get("Content-Type")
	if utf8.Valid(body) {
		contentType = "charset=utf-8"
	}
	return decodeSinaBody(body, contentType), nil
}

var annDateRE = regexp.MustCompile(`\b[0-9]{4}-[0-9]{2}-[0-9]{2}\b`)
var annDigitsRE = regexp.MustCompile(`^[0-9]+$`)
var annReportTitleRE = regexp.MustCompile(`^[0-9]{4}年?\s*(半年度|中期|年度|第一季度|一季度|第三季度|三季度)报告(?:摘要|[（(]摘要[）)]|[（(]英文版[）)])?$`)

func annParseList(document string, symbol foundation.Symbol, page int) ([]foundation.MarketResearchItem, string, error) {
	root, err := annParseDOM(document)
	if err != nil {
		return nil, "", annError(contracts.InvalidResponse, "invalid announcement DOM", err)
	}
	var list *html.Node
	listCount := 0
	name := ""
	identity := false
	annWalk(root, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "h1" {
			text := annText(n)
			suffix := "(" + symbol.RawCode + "." + symbol.Market + ")"
			if strings.HasSuffix(text, suffix) {
				identity = true
				name = strings.TrimSpace(strings.TrimSuffix(text, suffix))
			}
		}
		if annHasClass(n, "datelist") {
			listCount++
			list = n
		}
	})
	if !identity || name == "" {
		return nil, "", annError(contracts.InvalidResponse, "announcement company/exchange identity mismatch or missing", nil)
	}
	if list == nil || listCount != 1 {
		return nil, "", annError(contracts.InvalidResponse, "missing announcement list (challenge/login is not empty data)", nil)
	}
	rows := make([]foundation.MarketResearchItem, 0, 30)
	var rowErr error
	count := 0
	annWalk(list, func(n *html.Node) {
		if rowErr != nil || n.Type != html.ElementNode || n.Data != "a" {
			return
		}
		raw := annAttr(n, "href")
		if !strings.Contains(raw, "vCB_AllBulletinDetail.php") {
			rowErr = annError(contracts.InvalidResponse, "unrecognized announcement list link; coverage is not verified", nil)
			return
		}
		count++
		if count > 30 {
			rowErr = annError(contracts.InvalidResponse, "announcement page exceeds 30 rows", nil)
			return
		}
		detail, linkErr := annTrustedURL(raw, "/corp/view/vCB_AllBulletinDetail.php")
		if linkErr != nil || detail.Query().Get("stockid") != symbol.RawCode || !annDigitsRE.MatchString(detail.Query().Get("id")) || len(detail.Query()) != 2 {
			rowErr = annError(contracts.InvalidResponse, "unsafe or wrong-identity announcement link", linkErr)
			return
		}
		prefix := ""
		for prev := n.PrevSibling; prev != nil; prev = prev.PrevSibling {
			if prev.Type == html.ElementNode && (prev.Data == "br" || prev.Data == "a") {
				break
			}
			prefix = annText(prev) + " " + prefix
		}
		date := annDateRE.FindString(prefix)
		published, dateErr := time.ParseInLocation("2006-01-02", date, time.FixedZone("Asia/Shanghai", 8*60*60))
		title := annText(n)
		if dateErr != nil || title == "" || published.Year() < 1990 {
			rowErr = annError(contracts.InvalidResponse, "missing/invalid publication date or title", dateErr)
			return
		}
		q := url.Values{"stockid": {symbol.RawCode}, "id": {detail.Query().Get("id")}}
		rows = append(rows, foundation.MarketResearchItem{Kind: "announcement", ID: detail.Query().Get("id"), Symbol: symbol.Canonical, StockName: name, Title: title, Category: annKnownCategory(title), PublishedAt: published, URL: annOrigin + "/corp/view/vCB_AllBulletinDetail.php?" + q.Encode()})
	})
	if rowErr != nil {
		return nil, "", rowErr
	}
	if len(rows) == 0 && annText(list) != "" {
		return nil, "", annError(contracts.InvalidResponse, "unrecognized nonempty announcement list; not verified empty data", nil)
	}
	next := ""
	annWalk(root, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" || annText(n) != "下一页" {
			return
		}
		u, e := annTrustedURL(annAttr(n, "href"), "/corp/view/vCB_AllBulletin.php")
		if e != nil || u.Query().Get("stockid") != symbol.RawCode || u.Query().Get("Page") != strconv.Itoa(page+1) || len(u.Query()) != 2 {
			rowErr = annError(contracts.InvalidResponse, "unsafe/no-progress pagination URL", e)
			return
		}
		u.Scheme = "https"
		next = u.String()
	})
	if rowErr != nil {
		return nil, "", rowErr
	}
	return rows, next, nil
}
func annKnownCategory(title string) string {
	// Only explicit report title patterns identify a category; unknown stays unknown.
	if colon := strings.Index(title, "："); colon >= 0 {
		title = strings.TrimSpace(title[colon+len("："):])
	}
	parts := annReportTitleRE.FindStringSubmatch(title)
	if len(parts) != 2 {
		return ""
	}
	switch parts[1] {
	case "半年度", "中期":
		return "半年度报告"
	case "年度":
		return "年度报告"
	case "第一季度", "一季度":
		return "一季度报告"
	case "第三季度", "三季度":
		return "三季度报告"
	}
	return ""
}
func annParseDetail(document string, item foundation.MarketResearchItem, symbol foundation.Symbol) (string, error) {
	root, err := annParseDOM(document)
	if err != nil {
		return "", annError(contracts.InvalidResponse, "invalid announcement detail DOM", err)
	}
	var table, content *html.Node
	tableCount, contentCount := 0, 0
	titleOK, marketOK := false, false
	annWalk(root, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && strings.Contains(annText(n), item.StockName+"("+symbol.RawCode+")") {
			titleOK = true
		}
		if n.Data == "a" {
			u, e := annTrustedURL(annAttr(n, "href"), "/corp/go.php/vCB_AllNewsStock/symbol/"+symbol.Sina+".phtml")
			if e == nil && u != nil {
				marketOK = true
			}
		}
		if annAttr(n, "id") == "allbulletin" {
			tableCount++
			table = n
		}
	})
	if !titleOK || !marketOK || table == nil || tableCount != 1 {
		return "", annError(contracts.InvalidResponse, "detail identity missing/mismatch", nil)
	}
	dateOK, headingOK := false, false
	annWalk(table, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if annAttr(n, "id") == "content" {
			contentCount++
			content = n
		}
		if n.Data == "th" {
			var heading strings.Builder
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && child.Data == "font" {
					continue
				}
				heading.WriteString(annText(child))
			}
			if strings.TrimSpace(heading.String()) == item.Title {
				headingOK = true
			}
		}
		if n.Data == "td" && strings.HasPrefix(annText(n), "公告日期:") {
			dateOK = annDateRE.FindString(annText(n)) == item.PublishedAt.Format("2006-01-02")
		}
	})
	if !dateOK || !headingOK || content == nil || contentCount != 1 {
		return "", annError(contracts.InvalidResponse, "detail title/date/content mismatch", nil)
	}
	text := annText(content)
	if text == "" {
		return "", annError(contracts.NoData, "detail has no readable text; PDFs are not downloaded", nil)
	}
	return text, nil
}
func annTrustedURL(raw, path string) (*url.URL, error) {
	origin, _ := url.Parse(annOrigin)
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	u = origin.ResolveReference(u)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host != "vip.stock.finance.sina.com.cn" || u.User != nil || u.Path != path || u.Fragment != "" {
		return nil, errors.New("untrusted Sina URL")
	}
	return u, nil
}

// Bound DOM complexity as well as bytes so malformed deeply nested markup
// cannot spend the shared request budget in quadratic parser repair work.
func annParseDOM(document string) (*html.Node, error) { return disclosureDOM(document) }

func annAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func annHasClass(n *html.Node, class string) bool {
	for _, v := range strings.Fields(annAttr(n, "class")) {
		if v == class {
			return true
		}
	}
	return false
}
func annWalk(root *html.Node, fn func(*html.Node)) {
	// Iterative traversal keeps adversarial nested HTML off the call stack.
	stack := []*html.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript" || n.Data == "iframe") {
			continue
		}
		fn(n)
		for child := n.LastChild; child != nil; child = child.PrevSibling {
			stack = append(stack, child)
		}
	}
}
func annText(n *html.Node) string {
	var text strings.Builder
	annWalk(n, func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		} else if node.Type == html.ElementNode {
			switch node.Data {
			case "p", "br", "td", "th", "tr", "div", "li":
				text.WriteByte(' ')
			}
		}
	})
	return strings.Join(strings.Fields(text.String()), " ")
}
