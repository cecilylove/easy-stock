package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// AuctionTrace reads EastMoney's one-day pre-open trend endpoint. It exposes
// only 09:15–09:25 indicative prices; later minute amounts are excluded because
// their exact auction execution semantics are not established.
func (c *Client) AuctionTrace(ctx context.Context, symbol string) (foundation.AuctionTrace, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return foundation.AuctionTrace{}, err
	}
	params := url.Values{}
	params.Set("fields1", "f1,f2,f3,f4,f5,f6,f7,f8,f9,f10,f11,f12,f13")
	params.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58")
	params.Set("ndays", "1")
	params.Set("iscr", "1")
	params.Set("iscca", "0")
	params.Set("secid", normalized.EastMoneySecID)
	baseURLs := append([]string{c.quoteBaseURL}, c.quoteFallbackURLs...)
	var lastErr error
	for index, baseURL := range baseURLs {
		attemptCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		trace, err := c.auctionTraceFromBaseURL(attemptCtx, normalized.Canonical, baseURL, params, index > 0)
		cancel()
		if err == nil {
			return trace, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return foundation.AuctionTrace{}, ctx.Err()
		}
	}
	return foundation.AuctionTrace{}, fmt.Errorf("eastmoney pre-open unavailable: %w", lastErr)
}

func (c *Client) auctionTraceFromBaseURL(ctx context.Context, symbol string, baseURL string, params url.Values, fallback bool) (foundation.AuctionTrace, error) {
	requestURL := baseURL + "/api/qt/stock/trends2/get?" + params.Encode()
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return foundation.AuctionTrace{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return foundation.AuctionTrace{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return foundation.AuctionTrace{}, fmt.Errorf("eastmoney pre-open http status %d", resp.StatusCode)
	}
	var payload struct {
		RC   int `json:"rc"`
		Data *struct {
			Trends []string `json:"trends"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return foundation.AuctionTrace{}, err
	}
	if payload.RC != 0 || payload.Data == nil {
		return foundation.AuctionTrace{}, fmt.Errorf("eastmoney pre-open response unavailable (rc=%d)", payload.RC)
	}
	trace, err := parseAuctionTrends(symbol, payload.Data.Trends)
	if err != nil {
		return foundation.AuctionTrace{}, err
	}
	trace.Meta = foundation.SourceMeta{Source: "eastmoney:pre-open", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(), TradeDate: trace.TradeDate}
	if fallback {
		trace.Meta.FallbackReason = "东方财富主行情节点暂不可用，已切换备用节点"
	}
	return trace, nil
}

func parseAuctionTrends(symbol string, rows []string) (foundation.AuctionTrace, error) {
	result := foundation.AuctionTrace{Symbol: symbol, Points: []foundation.AuctionPoint{}}
	var date string
	var lastTime time.Time
	for _, raw := range rows {
		values := strings.Split(raw, ",")
		if len(values) < 7 {
			return foundation.AuctionTrace{}, fmt.Errorf("invalid pre-open row field count")
		}
		moment, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(values[0]), time.FixedZone("CST", 8*60*60))
		if err != nil {
			return foundation.AuctionTrace{}, fmt.Errorf("invalid pre-open time: %w", err)
		}
		if date == "" {
			date = moment.Format("2006-01-02")
		}
		if moment.Format("2006-01-02") != date || (!lastTime.IsZero() && !moment.After(lastTime)) {
			return foundation.AuctionTrace{}, fmt.Errorf("pre-open rows contain mixed dates or unordered times")
		}
		lastTime = moment
		hour, minute := moment.Hour(), moment.Minute()
		if hour != 9 || minute < 15 || minute > 25 {
			continue
		}
		price, err := strconv.ParseFloat(strings.TrimSpace(values[2]), 64)
		if err != nil || !isFinitePositive(price) {
			return foundation.AuctionTrace{}, fmt.Errorf("invalid pre-open price")
		}
		// Volume/amount are not presented as auction executions. Validate them
		// anyway: malformed upstream fields must not result in a valid trace.
		volume, volErr := strconv.ParseFloat(strings.TrimSpace(values[5]), 64)
		amount, amountErr := strconv.ParseFloat(strings.TrimSpace(values[6]), 64)
		if volErr != nil || amountErr != nil || !isFiniteNonNegative(volume) || !isFiniteNonNegative(amount) {
			return foundation.AuctionTrace{}, fmt.Errorf("invalid pre-open volume or amount")
		}
		result.Points = append(result.Points, foundation.AuctionPoint{Time: moment, Price: price})
	}
	if len(result.Points) == 0 {
		return foundation.AuctionTrace{}, fmt.Errorf("no 09:15–09:25 indicative points available")
	}
	result.TradeDate = date
	result.Meta.TradeDate = date
	return result, nil
}

func isFinitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isFiniteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
