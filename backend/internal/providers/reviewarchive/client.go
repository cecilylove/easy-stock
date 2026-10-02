// Package reviewarchive acquires prepublished daily review objects. It does
// not crawl authors' original platforms or manage subscriptions and local posts.
package reviewarchive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
)

const DefaultBaseURL = "https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/reviews/daily"
const maxBodyBytes = 2 << 20

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, client *http.Client) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{baseURL: baseURL, httpClient: client}
}

func (c *Client) FetchAuthors(ctx context.Context, query contracts.ArchiveAuthorsRequest) (contracts.ArchiveAuthorsResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/authors.json", nil)
	if err != nil {
		return contracts.ArchiveAuthorsResult{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "easy-stock-daily-review/2")
	if query.ETag != "" {
		request.Header.Set("If-None-Match", query.ETag)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return contracts.ArchiveAuthorsResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return contracts.ArchiveAuthorsResult{NotModified: true}, nil
	}
	if response.StatusCode == http.StatusNotFound {
		return contracts.ArchiveAuthorsResult{}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contracts.ArchiveAuthorsResult{}, fmt.Errorf("作者清单返回 HTTP %d", response.StatusCode)
	}
	body, err := readBody(response.Body)
	if err != nil {
		return contracts.ArchiveAuthorsResult{}, err
	}
	var manifest contracts.ArchiveManifest
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return contracts.ArchiveAuthorsResult{}, fmt.Errorf("解析作者清单: %w", err)
	}
	return contracts.ArchiveAuthorsResult{Manifest: manifest, ETag: strings.TrimSpace(response.Header.Get("ETag")), Found: true}, nil
}

func (c *Client) FetchArchiveArticle(ctx context.Context, query contracts.ArchiveArticleRequest) (contracts.ArchiveArticleResult, error) {
	objectURL := c.baseURL + "/" + url.PathEscape(query.AuthorID) + "/" + query.TradeDate + ".json"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, objectURL, nil)
	if err != nil {
		return contracts.ArchiveArticleResult{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "easy-stock-daily-review/2")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return contracts.ArchiveArticleResult{}, fmt.Errorf("请求文章: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return contracts.ArchiveArticleResult{ObjectURL: objectURL}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contracts.ArchiveArticleResult{}, fmt.Errorf("文章返回 HTTP %d", response.StatusCode)
	}
	body, err := readBody(response.Body)
	if err != nil {
		return contracts.ArchiveArticleResult{}, err
	}
	var item contracts.ArchivedArticle
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&item); err != nil {
		return contracts.ArchiveArticleResult{}, fmt.Errorf("解析文章: %w", err)
	}
	return contracts.ArchiveArticleResult{Article: item, ObjectURL: objectURL, Found: true}, nil
}

func readBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodyBytes {
		return nil, errors.New("远程每日复盘文件超过 2MB 限制")
	}
	return body, nil
}

var _ contracts.ArchiveProvider = (*Client)(nil)
