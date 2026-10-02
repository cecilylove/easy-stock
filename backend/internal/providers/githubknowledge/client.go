// Package githubknowledge implements a remote knowledge collection using GitHub
// tree and raw-document endpoints. It does not select traders or manage caches.
package githubknowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
)

const (
	DefaultTreeURL     = "https://api.github.com/repos/zhouqinglong520/trading-mastery/git/trees/main?recursive=1"
	DefaultRawBaseURL  = "https://raw.githubusercontent.com/zhouqinglong520/trading-mastery/main/"
	DefaultSourceURL   = "https://github.com/zhouqinglong520/trading-mastery/tree/main/%E6%B8%B8%E8%B5%84%E5%BF%83%E6%B3%95"
	defaultBlobBaseURL = "https://github.com/zhouqinglong520/trading-mastery/blob/main/"
)

type Config struct {
	HTTPClient *http.Client
	TreeURL    string
	RawBaseURL string
	SourceURL  string
}

type Client struct{ config Config }

var _ contracts.KnowledgeProvider = (*Client)(nil)

func NewClient(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if strings.TrimSpace(cfg.TreeURL) == "" {
		cfg.TreeURL = DefaultTreeURL
	}
	if strings.TrimSpace(cfg.RawBaseURL) == "" {
		cfg.RawBaseURL = DefaultRawBaseURL
	}
	if strings.TrimSpace(cfg.SourceURL) == "" {
		cfg.SourceURL = DefaultSourceURL
	}
	return &Client{config: cfg}
}

func (c *Client) KnowledgeIdentity() contracts.KnowledgeIdentity {
	return contracts.KnowledgeIdentity{SourceID: "githubknowledge", SourceURL: c.config.SourceURL}
}

func (c *Client) Tree(ctx context.Context) (contracts.KnowledgeTree, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.TreeURL, nil)
	if err != nil {
		return contracts.KnowledgeTree{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "easy-stock")
	response, err := c.config.HTTPClient.Do(request)
	if err != nil {
		return contracts.KnowledgeTree{}, fmt.Errorf("读取 GitHub 游资心法目录: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contracts.KnowledgeTree{}, fmt.Errorf("读取 GitHub 游资心法目录返回 HTTP %d", response.StatusCode)
	}
	var wire struct {
		SHA       string `json:"sha"`
		Truncated bool   `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&wire); err != nil {
		return contracts.KnowledgeTree{}, fmt.Errorf("解析 GitHub 游资心法目录: %w", err)
	}
	tree := contracts.KnowledgeTree{SourceID: "githubknowledge", Revision: wire.SHA, Complete: !wire.Truncated, SourceURL: c.config.SourceURL, Entries: make([]contracts.KnowledgeTreeEntry, 0, len(wire.Tree))}
	for _, entry := range wire.Tree {
		tree.Entries = append(tree.Entries, contracts.KnowledgeTreeEntry{Path: entry.Path, File: entry.Type == "blob", SourceURL: defaultBlobBaseURL + escapePath(entry.Path)})
	}
	return tree, nil
}

func (c *Client) Document(ctx context.Context, relativePath string) (contracts.KnowledgeDocument, error) {
	rawURL, err := joinURL(c.config.RawBaseURL, relativePath)
	if err != nil {
		return contracts.KnowledgeDocument{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return contracts.KnowledgeDocument{}, err
	}
	request.Header.Set("User-Agent", "easy-stock")
	response, err := c.config.HTTPClient.Do(request)
	if err != nil {
		return contracts.KnowledgeDocument{}, fmt.Errorf("下载 %s: %w", relativePath, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contracts.KnowledgeDocument{}, fmt.Errorf("下载 %s 返回 HTTP %d", relativePath, response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return contracts.KnowledgeDocument{}, fmt.Errorf("下载 %s: %w", relativePath, err)
	}
	if len(content) > 2<<20 {
		return contracts.KnowledgeDocument{}, fmt.Errorf("下载 %s 内容超过大小限制", relativePath)
	}
	return contracts.KnowledgeDocument{Content: content}, nil
}

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func joinURL(baseURL, relativePath string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + relativePath
	return base.String(), nil
}
