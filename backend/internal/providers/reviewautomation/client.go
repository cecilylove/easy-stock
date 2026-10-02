// Package reviewautomation implements browser-bridge and legacy content-service
// protocols. Subscription state, deduplication and model processing stay in review.
package reviewautomation

import (
	"bytes"
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/article"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Client struct{ httpClient, browserClient *http.Client }

func NewClient(client, browser *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	if browser == nil {
		copyClient := *client
		if copyClient.Timeout == 0 || copyClient.Timeout < 5*time.Minute {
			copyClient.Timeout = 5 * time.Minute
		}
		browser = &copyClient
	}
	return &Client{httpClient: client, browserClient: browser}
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
func browserSourceLabel(source string) string {
	if source == "taoguba" {
		return "淘股吧"
	}
	return "雪球"
}
func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

var titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func (c *Client) CollectBrowser(ctx context.Context, input contracts.BrowserCollectionRequest) (contracts.BrowserCollection, error) {
	if input.SourceID != "xueqiu" && input.SourceID != "taoguba" {
		return contracts.BrowserCollection{}, &contracts.Error{Kind: contracts.Unsupported, Capability: "browser-collection"}
	}
	bridgeURL, bridgeToken := input.BridgeURL, input.Token
	body, err := json.Marshal(map[string]any{
		"profile_id":   input.ProfileID,
		"homepage_url": input.HomepageURL,
		"limit":        input.Limit,
	})
	if err != nil {
		return contracts.BrowserCollection{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, bridgeURL+"/v1/"+input.SourceID+"/collect", bytes.NewReader(body))
	if err != nil {
		return contracts.BrowserCollection{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if bridgeToken != "" {
		request.Header.Set("X-A-Stock-Browser-Token", bridgeToken)
	}
	response, err := c.browserClient.Do(request)
	if err != nil {
		return contracts.BrowserCollection{}, fmt.Errorf("连接内置浏览器失败: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return contracts.BrowserCollection{}, err
	}
	var payload struct {
		OK    bool                        `json:"ok"`
		Data  contracts.BrowserCollection `json:"data"`
		Error string                      `json:"error"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return contracts.BrowserCollection{}, fmt.Errorf("内置浏览器返回格式无效: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !payload.OK {
		return contracts.BrowserCollection{}, errors.New(firstNonEmpty(strings.TrimSpace(payload.Error), fmt.Sprintf("内置浏览器返回 HTTP %d", response.StatusCode)))
	}
	if len(payload.Data.Articles) == 0 {
		return contracts.BrowserCollection{}, errors.New("内置浏览器没有读取到" + browserSourceLabel(input.SourceID) + "文章")
	}
	return payload.Data, nil
}

func (c *Client) discoverHTML(ctx context.Context, input contracts.AuthorLinksRequest) ([]string, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.HomepageURL, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/124 Safari/537.36")
	if input.Credential != "" {
		req.Header.Set("Cookie", input.Credential)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("作者主页返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, article.MaxArticleBytes))
	if err != nil {
		return nil, "", "", err
	}
	document := string(body)
	base, _ := url.Parse(input.HomepageURL)
	links := []string{}
	for _, match := range regexp.MustCompile(`(?is)href=["']([^"']+)["']`).FindAllStringSubmatch(document, -1) {
		target, err := url.Parse(html.UnescapeString(match[1]))
		if err != nil {
			continue
		}
		absolute := base.ResolveReference(target)
		host := strings.ToLower(absolute.Hostname())
		if (strings.HasSuffix(host, "taoguba.com.cn") || strings.HasSuffix(host, "tgb.cn")) && (strings.Contains(absolute.Path, "/a/") || strings.Contains(absolute.Path, "/Article/") || strings.Contains(absolute.Path, "/article/")) {
			links = append(links, absolute.String())
		}
	}
	name := article.CleanInline(firstNonEmpty(article.MetaValue(document, "property", "og:title"), article.MatchText(titlePattern, document), input.Name))
	return uniqueStrings(links), name, input.ExternalID, nil
}

func (c *Client) discoverWechat(ctx context.Context, input contracts.AuthorLinksRequest) ([]string, string, string, error) {
	base, token := strings.TrimRight(input.BaseURL, "/"), input.Credential
	if base == "" {
		return nil, "", "", errors.New("请先在设置中配置微信公众号解析服务地址")
	}
	fakeID := input.ExternalID
	name := input.Name
	if fakeID == "" || strings.Contains(fakeID, "/") || strings.HasPrefix(fakeID, "http") {
		endpoint := base + "/api/public/searchbiz?query=" + url.QueryEscape(firstNonEmpty(name, input.HomepageURL))
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				List []struct {
					FakeID   string `json:"fakeid"`
					Nickname string `json:"nickname"`
				} `json:"list"`
			} `json:"data"`
			Error string `json:"error"`
		}
		if err := c.getJSON(ctx, endpoint, token, &response); err != nil {
			return nil, "", "", err
		}
		if !response.Success || len(response.Data.List) == 0 {
			return nil, "", "", errors.New(firstNonEmpty(response.Error, "没有搜索到该公众号"))
		}
		fakeID = response.Data.List[0].FakeID
		name = response.Data.List[0].Nickname
	}
	endpoint := base + "/api/public/articles?fakeid=" + url.QueryEscape(fakeID) + "&begin=0&count=20"
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Articles []struct {
				Link string `json:"link"`
				URL  string `json:"url"`
			} `json:"articles"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := c.getJSON(ctx, endpoint, token, &response); err != nil {
		return nil, "", "", err
	}
	if !response.Success {
		return nil, "", "", errors.New(firstNonEmpty(response.Error, "公众号文章列表获取失败，请检查扫码登录状态"))
	}
	links := []string{}
	for _, item := range response.Data.Articles {
		if link := firstNonEmpty(item.Link, item.URL); link != "" {
			links = append(links, link)
		}
	}
	return uniqueStrings(links), name, fakeID, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint, token string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var payload struct {
			Error string `json:"error"`
			Data  struct {
				Error string `json:"error"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &payload) == nil {
			if message := firstNonEmpty(payload.Error, payload.Data.Error); message != "" {
				return errors.New(message)
			}
		}
		return fmt.Errorf("内容服务返回 HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, article.MaxArticleBytes)).Decode(target)
}
func (c *Client) FetchAuthorizedArticle(ctx context.Context, input contracts.AuthorizedArticleRequest) (foundation.Article, error) {
	base, token, link := input.BaseURL, input.Token, input.URL
	body, _ := json.Marshal(map[string]string{"url": link})
	if input.SourceID != "wechat" {
		return foundation.Article{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: input.SourceID, Capability: "authorized-article"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/article", bytes.NewReader(body))
	if err != nil {
		return foundation.Article{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return foundation.Article{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Title        string `json:"title"`
			PlainContent string `json:"plain_content"`
			Author       string `json:"author"`
			PublishTime  int64  `json:"publish_time"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, article.MaxArticleBytes)).Decode(&payload); err != nil {
		return foundation.Article{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return foundation.Article{}, fmt.Errorf("内容服务返回 HTTP %d", resp.StatusCode)
	}
	if !payload.Success {
		return foundation.Article{}, errors.New(firstNonEmpty(payload.Error, "微信文章解析失败"))
	}
	if payload.Data.PublishTime <= 0 {
		return foundation.Article{}, errors.New("微信公众号响应没有可靠的发布时间，为避免旧文章被误判为今日内容，本次不导入")
	}
	published := time.Unix(payload.Data.PublishTime, 0)
	content, err := article.NormalizeImportedContent(payload.Data.PlainContent)
	if err != nil {
		return foundation.Article{}, err
	}
	return foundation.Article{Source: "wechat", OriginalURL: link, AuthorName: payload.Data.Author, Title: payload.Data.Title, ContentText: content, PublishedAt: published, FetchedAt: time.Now()}, nil
}

func (c *Client) DiscoverAuthorLinks(ctx context.Context, input contracts.AuthorLinksRequest) (contracts.AuthorLinks, error) {
	var links []string
	var name, id string
	var err error
	switch input.SourceID {
	case "taoguba":
		links, name, id, err = c.discoverHTML(ctx, input)
	case "wechat":
		links, name, id, err = c.discoverWechat(ctx, input)
	default:
		return contracts.AuthorLinks{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: input.SourceID, Capability: "author-links"}
	}
	return contracts.AuthorLinks{URLs: links, Name: name, ExternalID: id}, err
}
