package hotstock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"time"

	"easy-stock/backend/internal/datasource/service"
	"easy-stock/backend/internal/foundation"
)

const (
	defaultTHSURL       = "https://eq.10jqka.com.cn/open/api/hot_list/v1/hot_stock/a/hour/data.txt"
	defaultEastMoneyURL = "https://emappdata.eastmoney.com/stockrank/getAllCurrentList"
)

type Client struct {
	httpClient   *http.Client
	thsURL       string
	eastMoneyURL string
}

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(target *Client) {
		if client != nil {
			target.httpClient = client
		}
	}
}

func WithSourceURLs(thsURL, eastMoneyURL string) Option {
	return func(target *Client) {
		if strings.TrimSpace(thsURL) != "" {
			target.thsURL = thsURL
		}
		if strings.TrimSpace(eastMoneyURL) != "" {
			target.eastMoneyURL = eastMoneyURL
		}
	}
}

func NewClient(options ...Option) *Client {
	client := &Client{
		httpClient:   &http.Client{Timeout: 12 * time.Second},
		thsURL:       defaultTHSURL,
		eastMoneyURL: defaultEastMoneyURL,
	}
	for _, option := range options {
		option(client)
	}
	return client
}

// NewTHSClient uses only the implemented public THS ranking endpoint.
func NewTHSClient(options ...Option) *Client {
	client := NewClient(options...)
	client.eastMoneyURL = ""
	return client
}

// SourceClient implements one supplier capability without cross-source routing.
type SourceClient struct {
	client *Client
	id     string
}

func NewTHSRankClient(options ...Option) *SourceClient {
	return &SourceClient{client: NewClient(options...), id: "ths"}
}
func NewEastMoneyRankClient(options ...Option) *SourceClient {
	return &SourceClient{client: NewClient(options...), id: "eastmoney"}
}
func (c *SourceClient) HotRank(ctx context.Context, limit int) foundation.HotStockRankList {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if c.id == "ths" {
		return c.client.loadTHS(ctx, limit)
	}
	return c.client.loadEastMoney(ctx, limit)
}

// Compatibility facade; default assembly uses the two independent adapters.
func (client *Client) HotStockRanks(ctx context.Context, limit int) []foundation.HotStockRankList {
	ths := &SourceClient{client: client, id: "ths"}
	if client.eastMoneyURL == "" {
		return service.NewHotRanks(ths).HotStockRanks(ctx, limit)
	}
	return service.NewHotRanks(ths, &SourceClient{client: client, id: "eastmoney"}).HotStockRanks(ctx, limit)
}
func (client *Client) loadTHS(ctx context.Context, limit int) foundation.HotStockRankList {
	list := foundation.HotStockRankList{Source: "ths", SourceName: "同花顺"}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.thsURL, nil)
	if err != nil {
		list.Error = "热榜请求创建失败"
		return list
	}
	setBrowserHeaders(request, "https://eq.10jqka.com.cn/")
	var payload struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
		Data       struct {
			Stocks []struct {
				Order int    `json:"order"`
				Code  string `json:"code"`
				Name  string `json:"name"`
			} `json:"stock_list"`
		} `json:"data"`
	}
	if err := client.doJSON(request, &payload); err != nil {
		list.Error = sourceError("同花顺", err)
		return list
	}
	if payload.StatusCode != 0 {
		list.Error = firstNonEmpty(strings.TrimSpace(payload.StatusMsg), "同花顺热榜暂不可用")
		return list
	}
	list.FetchedAt = time.Now()
	list.Items = normalizeItems(len(payload.Data.Stocks), limit, func(index int) (string, string, int) {
		item := payload.Data.Stocks[index]
		return item.Code, item.Name, item.Order
	})
	if len(list.Items) == 0 {
		list.Error = "同花顺热榜未返回股票"
	}
	return list
}

func (client *Client) loadEastMoney(ctx context.Context, limit int) foundation.HotStockRankList {
	list := foundation.HotStockRankList{Source: "eastmoney", SourceName: "东方财富"}
	body, _ := json.Marshal(map[string]any{
		"appId": "appId01", "globalId": "easy-stock-hot-rank", "marketType": "", "pageNo": 1, "pageSize": limit,
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.eastMoneyURL, bytes.NewReader(body))
	if err != nil {
		list.Error = "热榜请求创建失败"
		return list
	}
	setBrowserHeaders(request, "https://emappdata.eastmoney.com/")
	request.Header.Set("Content-Type", "application/json")
	var payload struct {
		Code    int    `json:"code"`
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    []struct {
			Symbol string `json:"sc"`
			Rank   int    `json:"rk"`
		} `json:"data"`
	}
	if err := client.doJSON(request, &payload); err != nil {
		list.Error = sourceError("东方财富", err)
		return list
	}
	if payload.Code != 0 || payload.Status != 0 {
		list.Error = firstNonEmpty(strings.TrimSpace(payload.Message), "东方财富热榜暂不可用")
		return list
	}
	list.FetchedAt = time.Now()
	list.Items = normalizeItems(len(payload.Data), limit, func(index int) (string, string, int) {
		item := payload.Data[index]
		return item.Symbol, "", item.Rank
	})
	if len(list.Items) == 0 {
		list.Error = "东方财富热榜未返回股票"
	}
	return list
}

func (client *Client) doJSON(request *http.Request, target any) error {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(target)
}

func normalizeItems(length, limit int, itemAt func(int) (string, string, int)) []foundation.HotStockRankItem {
	items := make([]foundation.HotStockRankItem, 0, min(length, limit))
	seen := make(map[string]struct{}, min(length, limit))
	for index := 0; index < length && len(items) < limit; index++ {
		rawSymbol, name, rank := itemAt(index)
		symbol, ok := normalizeHotStockSymbol(rawSymbol)
		if !ok {
			continue
		}
		if _, exists := seen[symbol]; exists {
			continue
		}
		seen[symbol] = struct{}{}
		if rank <= 0 {
			rank = index + 1
		}
		items = append(items, foundation.HotStockRankItem{Symbol: symbol, Name: strings.TrimSpace(name), Rank: rank})
	}
	return items
}

func normalizeHotStockSymbol(raw string) (string, bool) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	value = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(value, "SH"), "SZ"), "BJ")
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		value = value[:dot]
	}
	if _, err := strconv.Atoi(value); err != nil || (len(value) != 5 && len(value) != 6) {
		return "", false
	}
	normalized, err := foundation.NormalizeSymbol(value)
	if err != nil {
		return "", false
	}
	return normalized.Canonical, true
}

func setBrowserHeaders(request *http.Request, referer string) {
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	request.Header.Set("Referer", referer)
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/124 Safari/537.36")
}

func sourceError(source string, err error) string {
	if err == nil {
		return source + "热榜暂不可用"
	}
	if strings.Contains(strings.ToLower(err.Error()), "context deadline") || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return source + "热榜请求超时"
	}
	return source + "热榜暂不可用"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
