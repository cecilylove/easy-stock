package sina

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	repOrigin     = "https://stock.finance.sina.com.cn"
	repPrefix     = "/stock/go.php/"
	repMaxBody    = 2 << 20
	repBudget     = 10 * time.Second
	repMaxPages   = 5
	repMaxDetails = 8
	repMaxContent = 24000
)

// ReportConfig configures a public, cookie-free HTML adapter. BaseURL is the
// official origin (or an explicit loopback origin for isolated fixture tests).
// List/search/detail paths are fixed; no JavaScript or PDF is executed/fetched.
type ReportConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}
type ReportClient struct {
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
}

func NewReportClient(config ReportConfig) *ReportClient {
	base := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if base == "" {
		base = repOrigin
	}
	client := &http.Client{Timeout: repBudget}
	if config.HTTPClient != nil {
		copy := *config.HTTPClient
		client = &copy
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ReportClient{baseURL: base, httpClient: client, now: now}
}
func repError(kind contracts.ErrorKind, message string, cause error) error {
	if cause == nil {
		cause = errors.New(message)
	} else {
		cause = fmt.Errorf("%s: %w", message, cause)
	}
	return &contracts.Error{Kind: kind, SourceID: "sina", Capability: "report", Cause: cause, Timeout: kind == contracts.TimedOut}
}
func repRequestError(err error) error {
	kind := contracts.Kind(err)
	var timeout interface{ Timeout() bool }
	if kind == contracts.UpstreamFailure && errors.As(err, &timeout) && timeout.Timeout() {
		kind = contracts.TimedOut
	}
	return repError(kind, "public report request failed", err)
}
func repMeta(now time.Time) foundation.SourceMeta {
	return foundation.SourceMeta{Source: "sina:reports", Provider: "sina", Capability: "report", TimeZone: "Asia/Shanghai", FetchedAt: now, FieldsKnown: true, QueryCoverage: "complete"}
}
func repPartial(meta *foundation.SourceMeta, reason string) {
	meta.Partial = true
	if !strings.Contains(meta.FallbackReason, reason) {
		if meta.FallbackReason != "" {
			meta.FallbackReason += "; "
		}
		meta.FallbackReason += reason
	}
}

// MarketReports covers the latest 45 calendar days with at most five official
// pages and eight platform-readable details (three workers), sharing ONE 10s
// parent-respecting budget. An industry classification search is NOT an industry
// research search: Sina's t1=3 endpoint returns company reports. Until that scope
// is verified it returns typed Unsupported, allowing an explicit fallback.
func (c *ReportClient) MarketReports(ctx context.Context, kind, query, symbol, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	start := time.Now()
	now := c.now().In(time.FixedZone("Asia/Shanghai", 8*3600))
	meta := repMeta(now)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" || kind == "company" {
		kind = "stock"
	}
	unsupported := func(reason string) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
		repPartial(&meta, reason+"; explicit report fallback required")
		meta.ExecutionState = "skipped"
		meta.QueryCoverage = "unsupported"
		return nil, meta, repError(contracts.Unsupported, reason, nil)
	}
	if kind != "stock" && kind != "industry" {
		return unsupported("unknown report kind")
	}
	if strings.TrimSpace(industry) != "" {
		return unsupported("industry classification coverage is unverified; official classification search returns company reports")
	}
	if kind == "industry" && strings.TrimSpace(symbol) != "" {
		return unsupported("stock filter cannot select industry research")
	}
	var normalized foundation.Symbol
	var err error
	if strings.TrimSpace(symbol) != "" {
		normalized, err = foundation.NormalizeSymbol(symbol)
		if err != nil || !repStockCode(normalized.RawCode, normalized.Market) {
			return unsupported("unsupported company stock identity")
		}
	}
	base, err := url.Parse(c.baseURL)
	if err != nil || !repAllowedBase(base) {
		return unsupported("report base URL is outside the public Sina/loopback allowlist")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if len(query) > 512 {
		return unsupported("report query is too long")
	}
	ctx, cancel := context.WithTimeout(ctx, repBudget)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, meta, repRequestError(err)
	}
	listKind := "company"
	if kind == "industry" {
		listKind = "industry"
	}
	params := url.Values{}
	if normalized.RawCode != "" {
		listKind = "search"
		params.Set("symbol", normalized.RawCode)
		params.Set("t1", "all")
	}
	path := repPrefix + "vReport_List/kind/" + listKind + "/index.phtml"
	officialURL := repOrigin + path
	if len(params) > 0 {
		officialURL += "?" + params.Encode()
	}
	meta.SourceURL = officialURL
	meta.ExecutionState = "fetched"
	if query != "" {
		repPartial(&meta, "query coverage is restricted to explicit title, organization, researchers and title stock identity within bounded pages")
		meta.QueryCoverage = "bounded"
	}
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -45)
	items := make([]foundation.MarketResearchItem, 0, limit)
	seen := map[string]foundation.MarketResearchItem{}
	var lastDate time.Time
	page := 1
	var listErr error
	for scanned := 0; scanned < repMaxPages; scanned++ {
		requestParams := url.Values{}
		for key, values := range params {
			requestParams[key] = append([]string(nil), values...)
		}
		if page > 1 {
			requestParams.Set("p", strconv.Itoa(page))
		}
		target := c.baseURL + path
		if len(requestParams) > 0 {
			target += "?" + requestParams.Encode()
		}
		body, fetchErr := c.repFetch(ctx, target)
		if fetchErr != nil {
			listErr = fetchErr
			repPartial(&meta, "list page unavailable; remaining pages not traversed")
			break
		}
		parsed, parseErr := repParseList(body, now, page)
		if parseErr != nil {
			listErr = parseErr
			repPartial(&meta, "invalid list page; remaining coverage unknown")
			break
		}
		if parsed.paginationUnknown {
			repPartial(&meta, "missing pagination controls; remaining coverage unknown")
		}
		if parsed.invalid > 0 {
			repPartial(&meta, "invalid or truncated report rows rejected")
		}
		if scanned == 0 && len(parsed.items) == 0 && normalized.RawCode != "" {
			listErr = repError(contracts.Unsupported, "empty search page has no company identity to prove the stock filter was applied", nil)
			repPartial(&meta, "stock search empty coverage unverified; explicit report fallback required")
			break
		}
		for _, row := range parsed.items {
			if row.Kind != kind {
				repPartial(&meta, "unexpected research kind excluded; list coverage unverified")
			}
		}
		if normalized.RawCode != "" {
			for _, item := range parsed.items {
				if item.Kind != "stock" || item.Symbol != normalized.Canonical {
					listErr = repError(contracts.Unsupported, "official stock search did not preserve the requested company identity", nil)
					repPartial(&meta, "stock filter coverage unverified; explicit report fallback required")
					break
				}
			}
			if listErr != nil {
				break
			}
		}
		progress := 0
		older := false
		ordered := parsed.invalid == 0
		var previous time.Time
		for _, item := range parsed.items {
			if !previous.IsZero() && item.PublishedAt.After(previous) {
				ordered = false
			}
			previous = item.PublishedAt
			if !item.PublishedAt.Before(cutoff) {
				continue
			}
			older = true
		}
		for _, item := range parsed.items {
			if old, exists := seen[item.ID]; exists {
				if old.Title != item.Title || !old.PublishedAt.Equal(item.PublishedAt) || old.Symbol != item.Symbol || old.URL != item.URL {
					listErr = repError(contracts.InvalidResponse, "conflicting duplicate report identity", nil)
					repPartial(&meta, "conflicting duplicate row; query coverage unknown")
				}
				continue
			}
			if !lastDate.IsZero() && item.PublishedAt.After(lastDate) {
				repPartial(&meta, "report date order is not descending; latest-N coverage unverified")
			}
			lastDate = item.PublishedAt
			seen[item.ID] = item
			progress++
			if item.Kind != kind || item.PublishedAt.Before(cutoff) {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(strings.Join([]string{item.Title, item.Organization, item.Researchers, item.StockName, item.Symbol}, " ")), query) {
				continue
			}
			item.Meta = repMeta(now)
			item.Meta.ExecutionState = "fetched"
			item.Meta.SourceURL = item.URL
			item.Meta.NativeCode = strings.Split(item.Symbol, ".")[0]
			item.Meta.InstrumentID = item.Symbol
			if item.Symbol != "" {
				item.Meta.FieldSources = map[string]string{"symbol": "sina:reports:title", "stock_name": "sina:reports:title"}
			}
			item.Meta.AvailableFields = repFields(item)
			item.ContentStatus = "unavailable"
			item.ContentScope = "platform-readable"
			item.ContentIssue = "detail not fetched within bounded enrichment"
			items = append(items, item)
			if len(items) >= limit {
				break
			}
		}
		if listErr != nil {
			break
		}
		if !ordered {
			repPartial(&meta, "report page is not date-descending; latest-N coverage unverified")
		}
		if len(items) >= limit {
			// A latest-N query is satisfied by N valid descending records.
			// Not traversing the older archive is not lost query coverage.
			break
		}
		if parsed.next == 0 {
			break
		}
		if progress == 0 {
			repPartial(&meta, "pagination made no progress; remaining pages not traversed")
			break
		}
		if older && ordered {
			break
		} // proven descending page has crossed the requested 45-day window
		if scanned == repMaxPages-1 {
			repPartial(&meta, "five-page scan bound reached; remaining coverage unknown")
			break
		}
		page = parsed.next
	}
	if meta.Partial && meta.QueryCoverage == "complete" {
		meta.QueryCoverage = "bounded"
	}
	if contracts.Kind(listErr) == contracts.Unsupported {
		meta.QueryCoverage = "unsupported"
	}
	for i := range items {
		items[i].Meta.QueryCoverage = meta.QueryCoverage
		if meta.Partial {
			repPartial(&items[i].Meta, meta.FallbackReason)
		}
	}
	if len(items) == 0 {
		meta.LatencyMS = time.Since(start).Milliseconds()
		if listErr != nil {
			return items, meta, listErr
		}
		if query != "" || meta.Partial {
			return items, meta, repError(contracts.Unsupported, "bounded report coverage cannot prove an empty filtered result; explicit fallback required", nil)
		}
		return items, meta, nil
	}
	c.repDetails(ctx, items)
	for i := range items {
		item := &items[i]
		item.Meta.AvailableFields = repFields(*item)
		if item.ContentStatus != "available" {
			repPartial(&item.Meta, item.ContentIssue)
			repPartial(&meta, "some platform-readable details unavailable or truncated")
		}
		item.Meta.LatencyMS = time.Since(start).Milliseconds()
	}
	if err := ctx.Err(); err != nil {
		repPartial(&meta, "shared report budget/cancellation interrupted enrichment")
		if errors.Is(err, context.Canceled) {
			listErr = repRequestError(err)
		}
		// An inner body deadline degrades text quality, not a valid query list.
	}
	meta.AvailableFields = []string{"items"}
	meta.LatencyMS = time.Since(start).Milliseconds()
	return items, meta, listErr
}

func repStockCode(code, market string) bool {
	prefixes := map[string][]string{"SH": {"600", "601", "603", "605", "688", "689"}, "SZ": {"000", "001", "002", "003", "300", "301"}, "BJ": {"43", "83", "87", "88", "92"}}
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
		return false
	}
	for _, prefix := range prefixes[market] {
		if strings.HasPrefix(code, prefix) {
			return true
		}
	}
	return false
}
func repAllowedBase(u *url.URL) bool {
	if u == nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	if u.Scheme == "https" && u.Host == "stock.finance.sina.com.cn" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")
}

var repListPath = regexp.MustCompile(`^/stock/go\.php/vReport_List/kind/(company|industry|search)/index\.phtml$`)
var repShowPath = regexp.MustCompile(`^/stock/go\.php/vReport_Show/kind/(company|industry|search)/rptid/([0-9]{1,20})/index\.phtml$`)

func (c *ReportClient) repFetch(ctx context.Context, target string) (string, error) {
	u, err := url.Parse(target)
	base, _ := url.Parse(c.baseURL)
	if err != nil || u.User != nil || u.Fragment != "" || u.Host != base.Host || u.Scheme != base.Scheme || (!repListPath.MatchString(u.Path) && !repShowPath.MatchString(u.Path)) {
		return "", repError(contracts.Unsupported, "report URL outside allowlist", nil)
	}
	if err := ctx.Err(); err != nil {
		return "", repRequestError(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", repRequestError(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	req.Header.Set("Referer", repOrigin+"/")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", repRequestError(err)
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
			kind = contracts.InvalidResponse
		}
		return "", &contracts.Error{Kind: kind, SourceID: "sina", Capability: "report", HTTPStatus: resp.StatusCode}
	}
	if resp.ContentLength > repMaxBody {
		return "", repError(contracts.InvalidResponse, "report response exceeds 2 MiB", nil)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, repMaxBody+1))
	if err != nil {
		return "", repRequestError(err)
	}
	if len(raw) > repMaxBody {
		return "", repError(contracts.InvalidResponse, "report response exceeds 2 MiB", nil)
	}
	if err := ctx.Err(); err != nil {
		return "", repRequestError(err)
	}
	if !utf8.Valid(raw) {
		raw, err = simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
		if err != nil {
			return "", repError(contracts.InvalidResponse, "invalid report encoding", err)
		}
	}
	if !utf8.Valid(raw) || strings.ContainsRune(string(raw), utf8.RuneError) {
		return "", repError(contracts.InvalidResponse, "invalid report encoding", nil)
	}
	return string(raw), nil
}

type repList struct {
	items             []foundation.MarketResearchItem
	next, invalid     int
	paginationUnknown bool
}

func repParseList(body string, now time.Time, page int) (repList, error) {
	result := repList{}
	doc, err := disclosureDOM(body)
	if err != nil {
		return result, repError(contracts.InvalidResponse, "invalid report HTML", err)
	}
	var table, pager *html.Node
	challenge := false
	repWalk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if (n.Data == "title" || n.Data == "h1" || n.Data == "h2") && repGate(repText(n)) {
			challenge = true
		}
		if n.Data == "form" && repGate(repText(n)) && repAttr(n, "id") != "loginLayer" {
			challenge = true
		}
		if n.Data == "table" && repClass(n, "tb_01") {
			if table != nil {
				challenge = true
			}
			table = n
		}
		if repAttr(n, "id") == "_function_code_page" {
			pager = n
		}
	})
	if challenge || table == nil {
		return result, repError(contracts.InvalidResponse, "challenge or missing report table", nil)
	}
	var headers []string
	emptyEvidence := false
	repWalk(table, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "th" {
			headers = append(headers, repText(n))
		}
	})
	if strings.Join(headers, "|") != "序号|标题|报告类型|发布日期|机构|研究员" {
		return result, repError(contracts.InvalidResponse, "unknown report table schema", nil)
	}
	repWalk(table, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "tr" {
			return
		}
		var cells []*html.Node
		for cell := n.FirstChild; cell != nil; cell = cell.NextSibling {
			if cell.Type == html.ElementNode && cell.Data == "td" {
				cells = append(cells, cell)
			}
		}
		if len(cells) == 0 {
			return
		}
		if len(cells) == 1 && repAttr(cells[0], "colspan") == "6" {
			if value := strings.TrimSpace(repText(cells[0])); value == "" || value == "暂无研报" || value == "暂无相关研报" {
				emptyEvidence = true
			} else {
				result.invalid++
			}
			return
		}
		if len(cells) != 6 {
			result.invalid++
			return
		}
		rowKind := repCategory(repText(cells[2]))
		if rowKind == "" {
			result.invalid++
			return
		}
		var link *html.Node
		ambiguousLink := false
		repWalk(cells[1], func(a *html.Node) {
			if a.Type == html.ElementNode && a.Data == "a" {
				if link != nil {
					ambiguousLink = true
				}
				link = a
			}
		})
		if link == nil || ambiguousLink {
			result.invalid++
			return
		}
		title := repAttr(link, "title")
		if title == "" {
			title = repText(link)
		}
		if !repRequired(title) || utf8.RuneCountInString(title) > 1024 {
			result.invalid++
			return
		}
		u, e := url.Parse(repAttr(link, "href"))
		if e != nil {
			result.invalid++
			return
		}
		u = (&url.URL{Scheme: "https", Host: "stock.finance.sina.com.cn"}).ResolveReference(u)
		matches := repShowPath.FindStringSubmatch(u.Path)
		if (u.Scheme != "https" && u.Scheme != "http") || u.Host != "stock.finance.sina.com.cn" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || matches == nil || strings.Trim(matches[2], "0") == "" {
			result.invalid++
			return
		}
		showKind := "company"
		if rowKind == "industry" {
			showKind = "industry"
		}
		if matches[1] != "search" && matches[1] != showKind {
			result.invalid++
			return
		}
		date, e := time.ParseInLocation("2006-01-02", repText(cells[3]), now.Location())
		if e != nil || date.Year() < 1990 || date.After(now) {
			result.invalid++
			return
		}
		item := foundation.MarketResearchItem{Kind: rowKind, ID: matches[2], Title: title, PublishedAt: date, URL: repOrigin + repPrefix + "vReport_Show/kind/" + showKind + "/rptid/" + matches[2] + "/index.phtml", Category: repText(cells[2])}
		if rowKind == "stock" {
			name, code := repTitleIdentity(title)
			normalized, e := foundation.NormalizeSymbol(code)
			if e != nil || !repStockCode(normalized.RawCode, normalized.Market) || !repRequired(name) {
				result.invalid++
				return
			}
			item.Symbol = normalized.Canonical
			item.StockName = name
		}
		organization, researchers := repText(cells[4]), repText(cells[5])
		if utf8.RuneCountInString(organization) > 512 || utf8.RuneCountInString(researchers) > 512 {
			result.invalid++
			return
		}
		// Institution/author are optional source fields. Missing author must not
		// discard an otherwise identified and dated report or force whole fallback.
		if repRequired(organization) {
			item.Organization = organization
		}
		if repRequired(researchers) {
			item.Researchers = researchers
		}
		result.items = append(result.items, item)
	})
	if len(result.items) == 0 && (result.invalid > 0 || !emptyEvidence) {
		return result, repError(contracts.InvalidResponse, "empty report page lacks validated empty-result evidence", nil)
	}
	result.paginationUnknown = pager == nil && len(result.items) > 0
	if pager != nil {
		invalidPage := false
		repWalk(pager, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "a" || repText(n) != "下一页" {
				return
			}
			// Only the exact observed declarative parameter pattern is recognized.
			// This extracts a numeric page; it does not evaluate or execute JavaScript.
			m := repPageCall.FindStringSubmatch(repAttr(n, "onclick"))
			if m == nil {
				invalidPage = true
				return
			}
			next, e := strconv.Atoi(m[1])
			if e != nil || next != page+1 {
				invalidPage = true
				return
			}
			result.next = next
		})
		if invalidPage {
			return result, repError(contracts.InvalidResponse, "unsafe or non-sequential report pagination", nil)
		}
	}
	if result.invalid > 0 && len(result.items) == 0 {
		return result, repError(contracts.InvalidResponse, "all report rows invalid", nil)
	}
	return result, nil
}

var repPageCall = regexp.MustCompile(`^set_page_num\(['"]([0-9]{1,6})['"]\);?$`)
var repTitleCode = regexp.MustCompile(`^([^()（）]+)[(（]([0-9]{6})[)）]`)

func repTitleIdentity(title string) (string, string) {
	m := repTitleCode.FindStringSubmatch(title)
	if m == nil {
		return "", ""
	}
	return strings.TrimSpace(m[1]), m[2]
}
func repCategory(value string) string {
	switch value {
	case "公司", "创业板", "中小板", "科创板":
		return "stock"
	case "行业":
		return "industry"
	}
	return ""
}
func (c *ReportClient) repDetails(ctx context.Context, items []foundation.MarketResearchItem) {
	count := min(len(items), repMaxDetails)
	work := make(chan int, count)
	for i := 0; i < count; i++ {
		work <- i
	}
	close(work)
	var wg sync.WaitGroup
	for worker := 0; worker < min(3, count); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				item := &items[i]
				body, err := c.repFetch(ctx, c.baseURL+strings.TrimPrefix(item.URL, repOrigin))
				if err == nil {
					err = repParseDetail(body, item)
				}
				if err != nil {
					item.Content = ""
					item.ContentStatus = "unavailable"
					item.ContentIssue = err.Error()
				}
			}
		}()
	}
	wg.Wait()
}
func repParseDetail(body string, item *foundation.MarketResearchItem) error {
	doc, err := disclosureDOM(body)
	if err != nil {
		return repError(contracts.InvalidResponse, "invalid detail HTML", err)
	}
	var content *html.Node
	ambiguous := false
	challenge := false
	repWalk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if (n.Data == "title" || n.Data == "h1" || n.Data == "h2") && repGate(repText(n)) {
			challenge = true
		}
		if n.Data == "div" && repClass(n, "content") {
			if content != nil {
				ambiguous = true
			}
			content = n
		}
	})
	if content == nil || ambiguous || challenge {
		return repError(contracts.InvalidResponse, "detail unavailable/challenge", nil)
	}
	var title, info string
	var textNode *html.Node
	titleCount, infoCount, textCount := 0, 0, 0
	infoFields := map[string]string{}
	repWalk(content, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "h1" {
			titleCount++
			title = repText(n)
		}
		if repClass(n, "creab") {
			infoCount++
			info = repText(n)
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode {
					parts := strings.SplitN(repText(child), "：", 2)
					if len(parts) == 2 {
						infoFields[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
					}
				}
			}
		}
		if repClass(n, "blk_container") {
			textCount++
			textNode = n
		}
	})
	if titleCount != 1 || infoCount != 1 || textCount != 1 || (item.Organization != "" && infoFields["机构"] != item.Organization) || (item.Researchers != "" && infoFields["研究员"] != item.Researchers) || title != item.Title || !strings.Contains(info, "日期："+item.PublishedAt.Format("2006-01-02")) || !strings.Contains(info, "类别："+item.Category) {
		return repError(contracts.InvalidResponse, "detail report title/date/category identity mismatch", nil)
	}
	if textNode == nil {
		return repError(contracts.NoData, "platform-readable body unavailable", nil)
	}
	text := repText(textNode)
	if !repRequired(text) || repGate(text) {
		return repError(contracts.NoData, "platform-readable body empty or gated", nil)
	}
	item.ContentStatus = "available"
	item.ContentScope = "platform-readable"
	item.ContentIssue = ""
	runes := []rune(text)
	if len(runes) > repMaxContent {
		text = string(runes[:repMaxContent])
		item.ContentStatus = "truncated"
		item.ContentIssue = "platform-readable text capped at 24000 characters; not PDF/full report"
	}
	item.Content = text
	return nil
}
func repFields(item foundation.MarketResearchItem) []string {
	fields := []string{"kind", "id", "title", "published_at", "url"}
	for _, entry := range []struct{ key, value string }{{"symbol", item.Symbol}, {"stock_name", item.StockName}, {"organization", item.Organization}, {"researchers", item.Researchers}, {"category", item.Category}, {"content", item.Content}, {"content_status", item.ContentStatus}, {"content_scope", item.ContentScope}, {"content_issue", item.ContentIssue}} {
		if entry.value != "" {
			fields = append(fields, entry.key)
		}
	}
	// The official HTML has no structured rating/target/EPS/PE cells. Free-text
	// numbers and recommendation prose remain ONLY text, never numeric evidence.
	return fields
}
func repAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}
func repClass(n *html.Node, key string) bool {
	for _, class := range strings.Fields(repAttr(n, "class")) {
		if class == key {
			return true
		}
	}
	return false
}
func repWalk(n *html.Node, visit func(*html.Node)) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "script", "style", "noscript", "template":
			return
		}
		if repAttr(n, "id") == "loginLayer" || repAttr(n, "id") == "loginBG" {
			return
		}
	}
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		repWalk(child, visit)
	}
}
func repText(n *html.Node) string {
	var text strings.Builder
	repWalk(n, func(child *html.Node) {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		}
		if child.Type == html.ElementNode && (child.Data == "br" || child.Data == "p" || child.Data == "div" || child.Data == "span") {
			text.WriteByte('\n')
		}
	})
	var lines []string
	for _, line := range strings.Split(text.String(), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
func repRequired(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsRune(value, utf8.RuneError) || strings.HasSuffix(value, "...") || strings.HasSuffix(value, "…") {
		return false
	}
	switch strings.ToLower(value) {
	case "-", "--", "暂无", "暂无数据", "暂无资料", "暂无相关数据", "暂无相关研报", "暂无研报", "无", "null", "undefined", "n/a", "加载中":
		return false
	}
	for _, token := range []string{"{{", "}}", "${", "<%"} {
		if strings.Contains(value, token) {
			return false
		}
	}
	return true
}
func repGate(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{"验证码", "访问异常", "访问受限", "安全验证", "请先登录", "请登录", "登录后查看", "页面不存在", "系统繁忙", "captcha", "access denied"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
