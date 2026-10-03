package sina

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"golang.org/x/net/html"
)

const (
	businessBaseURL = "https://vip.stock.finance.sina.com.cn/corp/go.php/vCI_CorpInfo/stockid/{code}.phtml"
	businessMaxBody = 2 << 20
	businessTimeout = 6 * time.Second
)

// BusinessConfig configures only the public company-profile adapter. BaseURL
// accepts a {code} URL template, or a base directory to append /{code}.phtml to.
// Neither cookies nor a browser/login session is required.
type BusinessConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

type BusinessClient struct {
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
}

func NewBusinessClient(config BusinessConfig) *BusinessClient {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = businessBaseURL
	}
	client := &http.Client{Timeout: businessTimeout}
	if config.HTTPClient != nil {
		copy := *config.HTTPClient
		client = &copy
	}
	// Do not follow a public-page redirect into a login or challenge endpoint.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// The adapter never sends a caller's cookie jar to this public endpoint.
	client.Jar = nil
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &BusinessClient{baseURL: baseURL, httpClient: client, now: now}
}

// StockBusinessProfile reads only explicit cells from Sina's company profile.
// A valid name/code/market identity and BOTH main business and description are
// required. Industry and business scope are never substitutes for either one.
func (c *BusinessClient) StockBusinessProfile(ctx context.Context, symbol string) (foundation.StockBusinessProfile, error) {
	normalized, err := businessSymbol(symbol)
	if err != nil {
		return foundation.StockBusinessProfile{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, businessTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return foundation.StockBusinessProfile{}, businessRequestError(err)
	}
	requestURL := c.baseURL
	if strings.Contains(requestURL, "{code}") {
		requestURL = strings.ReplaceAll(requestURL, "{code}", normalized.RawCode)
	} else {
		requestURL = strings.TrimRight(requestURL, "/") + "/" + normalized.RawCode + ".phtml"
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return foundation.StockBusinessProfile{}, businessError(contracts.InvalidResponse, "invalid profile URL", err)
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return foundation.StockBusinessProfile{}, businessRequestError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		kind := contracts.UpstreamFailure
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			kind = contracts.NoData
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			kind = contracts.Unauthorized
		case resp.StatusCode == http.StatusTooManyRequests:
			kind = contracts.RateLimited
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			kind = contracts.InvalidResponse
		}
		return foundation.StockBusinessProfile{}, &contracts.Error{Kind: kind, SourceID: "sina", Capability: "business", HTTPStatus: resp.StatusCode}
	}
	if resp.ContentLength > businessMaxBody {
		return foundation.StockBusinessProfile{}, businessError(contracts.InvalidResponse, "profile exceeds 2 MiB", nil)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, businessMaxBody+1))
	if err != nil {
		return foundation.StockBusinessProfile{}, businessRequestError(err)
	}
	if len(body) > businessMaxBody {
		return foundation.StockBusinessProfile{}, businessError(contracts.InvalidResponse, "profile exceeds 2 MiB", nil)
	}
	if err := ctx.Err(); err != nil {
		return foundation.StockBusinessProfile{}, businessRequestError(err)
	}
	// decodeSinaBody prioritizes a GBK header over valid UTF-8 bytes. Public
	// mirrors sometimes retain that header after transcoding; prefer actual
	// valid UTF-8, otherwise use its existing GB18030/GBK fallback.
	contentType := resp.Header.Get("Content-Type")
	if utf8.Valid(body) {
		contentType = "charset=utf-8"
	}
	decoded := decodeSinaBody(body, contentType)
	profile, err := parseBusinessHTML(decoded, normalized)
	if contextErr := ctx.Err(); contextErr != nil {
		return foundation.StockBusinessProfile{}, businessRequestError(contextErr)
	}
	profile.Meta = foundation.SourceMeta{
		Source: "sina:business", Provider: "sina", Capability: "business", SourceURL: requestURL,
		NativeCode: normalized.Sina, InstrumentID: normalized.Canonical,
		FetchedAt: c.now().In(time.FixedZone("Asia/Shanghai", 8*60*60)), TimeZone: "Asia/Shanghai",
		LatencyMS: time.Since(start).Milliseconds(), ExecutionState: "fetched", FieldsKnown: true,
		AvailableFields: businessAvailableFields(profile), Partial: err != nil,
	}
	// AsOf/NativeTimestamp are deliberately empty: this page has no reliable
	// publication/update timestamp. FetchedAt is NOT a source publication date.
	return profile, err
}

func businessError(kind contracts.ErrorKind, message string, cause error) error {
	if cause != nil {
		cause = fmt.Errorf("%s: %w", message, cause)
	} else {
		cause = errors.New(message)
	}
	return &contracts.Error{Kind: kind, SourceID: "sina", Capability: "business", Cause: cause}
}

func businessRequestError(err error) error {
	kind := contracts.Kind(err)
	var timeout interface{ Timeout() bool }
	if kind == contracts.UpstreamFailure && errors.As(err, &timeout) && timeout.Timeout() {
		kind = contracts.TimedOut
	}
	return &contracts.Error{Kind: kind, SourceID: "sina", Capability: "business", Cause: err, Timeout: kind == contracts.TimedOut}
}

func businessSymbol(input string) (foundation.Symbol, error) {
	symbol, err := foundation.NormalizeSymbol(input)
	if err != nil || len(symbol.RawCode) != 6 {
		return foundation.Symbol{}, businessError(contracts.Unsupported, "unsupported A-share symbol", err)
	}
	code := symbol.RawCode
	supported := false
	switch symbol.Market {
	case "SH":
		supported = strings.HasPrefix(code, "600") || strings.HasPrefix(code, "601") || strings.HasPrefix(code, "603") || strings.HasPrefix(code, "605") || strings.HasPrefix(code, "688") || strings.HasPrefix(code, "689")
	case "SZ":
		supported = strings.HasPrefix(code, "000") || strings.HasPrefix(code, "001") || strings.HasPrefix(code, "002") || strings.HasPrefix(code, "003") || strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301")
	case "BJ":
		supported = strings.HasPrefix(code, "43") || strings.HasPrefix(code, "83") || strings.HasPrefix(code, "87") || strings.HasPrefix(code, "88") || strings.HasPrefix(code, "92")
	}
	if !supported {
		return foundation.Symbol{}, businessError(contracts.Unsupported, "unsupported A-share market/code prefix", nil)
	}
	return symbol, nil
}

func parseBusinessHTML(body string, symbol foundation.Symbol) (foundation.StockBusinessProfile, error) {
	profile := foundation.StockBusinessProfile{Symbol: symbol.Canonical}
	if strings.ContainsRune(body, utf8.RuneError) {
		return profile, businessError(contracts.InvalidResponse, "invalid profile character encoding", nil)
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return profile, businessError(contracts.InvalidResponse, "invalid profile HTML", err)
	}
	var tables []*html.Node
	var identities []string
	var title string
	var challenge bool
	businessWalk(doc, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		switch node.Data {
		case "table":
			tables = append(tables, node)
		case "title":
			title = businessText(node)
			identities = append(identities, title)
		case "h1":
			text := businessText(node)
			identities = append(identities, text)
			challenge = challenge || businessGate(text)
		case "h2", "h3", "form":
			challenge = challenge || businessGate(businessText(node))
		}
	})
	if challenge {
		return profile, businessError(contracts.InvalidResponse, "page contains an active login/challenge/error form or heading", nil)
	}
	var fields map[string]string
	for _, table := range tables {
		candidate, fieldErr := businessTableFields(table)
		_, hasName := candidate["公司名称"]
		if businessAttr(table, "id") != "comInfo1" && !hasName {
			continue
		}
		if fieldErr != nil {
			return profile, fieldErr
		}
		if fields != nil {
			return profile, businessError(contracts.InvalidResponse, "ambiguous company profile tables", nil)
		}
		fields = candidate
	}
	if fields == nil {
		visible := businessText(doc)
		if businessGate(title) || businessGate(visible) {
			return profile, businessError(contracts.InvalidResponse, "login/challenge/error page, not a public profile", nil)
		}
		if strings.Contains(visible, "暂无资料") || strings.Contains(visible, "暂无数据") || strings.Contains(visible, "不支持") {
			return profile, businessError(contracts.NoData, "no company profile on this page", nil)
		}
		return profile, businessError(contracts.InvalidResponse, "missing company profile table", nil)
	}
	if businessGate(title) {
		return profile, businessError(contracts.InvalidResponse, "profile title is a login/challenge/error page", nil)
	}
	profile.Name = businessValue(fields["公司名称"])
	if profile.Name == "" {
		return profile, businessError(contracts.NoData, "company name is empty or a template", nil)
	}
	codeSeen := false
	for _, identity := range identities {
		name, code, found := businessIdentity(identity)
		if !found {
			continue
		}
		if !businessCodeMatches(code, symbol) || !businessNameMatches(name, profile.Name) {
			return profile, businessError(contracts.InvalidResponse, "page company name/code does not match requested stock", nil)
		}
		codeSeen = true
	}
	for _, key := range []string{"股票代码", "证券代码"} {
		if code := businessValue(fields[key]); code != "" {
			if !businessCodeMatches(code, symbol) {
				return profile, businessError(contracts.InvalidResponse, "table stock code does not match requested stock", nil)
			}
			codeSeen = true
		}
	}
	if !codeSeen {
		return profile, businessError(contracts.InvalidResponse, "profile has no stock-code identity", nil)
	}
	market := businessValue(fields["上市市场"])
	wantMarket := map[string]string{"SH": "上海证券交易所", "SZ": "深圳证券交易所", "BJ": "北京证券交易所"}[symbol.Market]
	if market != "" && market != wantMarket {
		kind := contracts.InvalidResponse
		if symbol.Market == "BJ" {
			kind = contracts.Unsupported
		}
		return profile, businessError(kind, "profile listing market does not match requested stock", nil)
	}
	if symbol.Market == "BJ" && market != wantMarket {
		return profile, businessError(contracts.Unsupported, "Beijing company page lacks its own listing-market identity", nil)
	}
	profile.MainBusiness = businessFirstValue(fields, "主营业务", "主营业务描述")
	profile.Description = businessFirstValue(fields, "公司简介", "公司介绍")
	profile.Industry = businessFirstValue(fields, "所属行业", "行业")
	profile.BusinessScope = businessFirstValue(fields, "经营范围")
	if businessGate(profile.MainBusiness) || businessGate(profile.Description) {
		return profile, businessError(contracts.InvalidResponse, "company cells contain a login/challenge/error response", nil)
	}
	if profile.MainBusiness == "" || profile.Description == "" {
		return profile, businessError(contracts.NoData, "explicit main business and company description are both required", nil)
	}
	return profile, nil
}

// Only label/value sibling cells in a single table row are paired. Nested
// tables are excluded, and neither scripts nor whole-page text supply fields.
func businessTableFields(table *html.Node) (map[string]string, error) {
	fields := make(map[string]string)
	var rows func(*html.Node) error
	rows = func(node *html.Node) error {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "table" {
				continue
			}
			if child.Type == html.ElementNode && child.Data == "tr" {
				var cells []*html.Node
				for cell := child.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type == html.ElementNode && (cell.Data == "td" || cell.Data == "th") {
						cells = append(cells, cell)
					}
				}
				for index := 0; index+1 < len(cells); index++ {
					label := strings.TrimRight(strings.Join(strings.Fields(businessText(cells[index])), ""), ":：")
					switch label {
					case "公司名称", "股票代码", "证券代码", "股票简称", "证券简称", "证券简称更名历史", "上市市场", "主营业务", "主营业务描述", "公司简介", "公司介绍", "所属行业", "行业", "经营范围":
						value := businessText(cells[index+1])
						if previous, exists := fields[label]; exists && previous != value {
							return businessError(contracts.InvalidResponse, "conflicting company table fields", nil)
						}
						fields[label] = value
						index++
					}
				}
			} else if err := rows(child); err != nil {
				return err
			}
		}
		return nil
	}
	return fields, rows(table)
}

func businessAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func businessWalk(node *html.Node, visit func(*html.Node)) {
	if node.Type == html.ElementNode {
		switch node.Data {
		case "script", "style", "noscript", "template":
			return
		}
		if businessAttr(node, "id") == "loginLayer" || businessAttr(node, "id") == "loginBG" {
			return
		}
	}
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		businessWalk(child, visit)
	}
}

func businessText(node *html.Node) string {
	var text strings.Builder
	businessWalk(node, func(child *html.Node) {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		} else if child.Type == html.ElementNode && (child.Data == "br" || child.Data == "p" || child.Data == "div") {
			text.WriteByte('\n')
		}
	})
	lines := strings.Split(strings.ReplaceAll(text.String(), "\r", ""), "\n")
	var nonempty []string
	for _, line := range lines {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			nonempty = append(nonempty, line)
		}
	}
	return strings.Join(nonempty, "\n")
}

func businessValue(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "-", "--", "—", "暂无", "暂无资料", "暂无数据", "暂无信息", "暂无相关资料", "暂无相关数据", "暂无公司简介", "暂无主营业务", "待更新", "加载中", "加载中...", "无", "n/a", "null", "undefined":
		return ""
	}
	for _, marker := range []string{"@", "{{", "}}", "${", "<%"} {
		if strings.Contains(value, marker) {
			return ""
		}
	}
	return value
}

func businessFirstValue(fields map[string]string, labels ...string) string {
	for _, label := range labels {
		if value := businessValue(fields[label]); value != "" {
			return value
		}
	}
	return ""
}

func businessGate(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{"验证码", "访问异常", "访问受限", "安全验证", "请先登录", "请登录", "登录后查看", "用户登录", "页面不存在", "页面错误", "系统错误", "系统繁忙", "服务不可用", "captcha", "access denied", "not found"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

// Identity comes from a title/h1's name(code), never URLs, JavaScript, or a
// six-digit substring elsewhere in the page (e.g. another stock's navigation).
func businessIdentity(value string) (string, string, bool) {
	value = strings.NewReplacer("（", "(", "）", ")").Replace(value)
	start := strings.IndexByte(value, '(')
	if start <= 0 {
		return "", "", false
	}
	end := strings.IndexByte(value[start+1:], ')')
	if end < 0 {
		return "", "", false
	}
	code := strings.TrimSpace(value[start+1 : start+1+end])
	if len(code) != 6 && len(code) != 9 {
		return "", "", false
	}
	for _, r := range code[:6] {
		if r < '0' || r > '9' {
			return "", "", false
		}
	}
	return strings.TrimSpace(value[:start]), code, true
}

func businessCodeMatches(code string, symbol foundation.Symbol) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	return code == symbol.RawCode || code == symbol.Canonical || code == strings.ToUpper(symbol.Sina)
}

func businessNameMatches(shortName, companyName string) bool {
	// Short names can omit words: 京沪高铁 / 京沪高速铁路股份有限公司,
	// 万达轴承 / 江苏万达特种轴承股份有限公司. Check ordered characters,
	// not arbitrary names; explicit stock-code and market evidence is separate.
	name := strings.ToUpper(strings.Join(strings.Fields(shortName), ""))
	for _, prefix := range []string{"*ST", "ST", "XD", "XR", "DR"} {
		name = strings.TrimPrefix(name, prefix)
	}
	if (strings.HasPrefix(name, "N") || strings.HasPrefix(name, "C")) && len(name) > 1 {
		name = name[1:]
	}
	if utf8.RuneCountInString(name) < 2 {
		return false
	}
	company := []rune(strings.ToUpper(strings.Join(strings.Fields(companyName), "")))
	index := 0
	for _, r := range name {
		for index < len(company) && company[index] != r {
			index++
		}
		if index == len(company) {
			return false
		}
		index++
	}
	return strings.IndexFunc(name, unicode.IsLetter) >= 0
}

func businessAvailableFields(profile foundation.StockBusinessProfile) []string {
	fields := []string{"symbol"}
	for _, field := range []struct{ name, value string }{
		{"name", profile.Name}, {"main_business", profile.MainBusiness}, {"description", profile.Description},
		{"industry", profile.Industry}, {"business_scope", profile.BusinessScope},
	} {
		if field.value != "" {
			fields = append(fields, field.name)
		}
	}
	return fields
}
