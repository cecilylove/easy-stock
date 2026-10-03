package eastmoney

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// limitPoolNumber distinguishes valid zero from missing/non-finite wire values.
type limitPoolNumber struct {
	value float64
	valid bool
}

func (n *limitPoolNumber) UnmarshalJSON(data []byte) error {
	*n = limitPoolNumber{}
	text := strings.TrimSpace(string(data))
	if strings.HasPrefix(text, "\"") {
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
		n.value, n.valid = value, true
	}
	return nil
}
func (n limitPoolNumber) validInt() bool {
	return n.valid && math.Trunc(n.value) == n.value && n.value >= 0 && n.value < float64(int(^uint(0)>>1))
}

type limitUpPoolPayload struct {
	RC   int `json:"rc"`
	Data *struct {
		Pool *[]struct {
			Code           string          `json:"c"`
			Name           string          `json:"n"`
			Price          limitPoolNumber `json:"p"`
			ChangePercent  limitPoolNumber `json:"zdp"`
			Amount         limitPoolNumber `json:"amount"`
			FloatMarketCap limitPoolNumber `json:"ltsz"`
			TurnoverRate   limitPoolNumber `json:"hs"`
			Streak         limitPoolNumber `json:"lbc"`
			FirstLimitTime limitPoolNumber `json:"fbt"`
			LastLimitTime  limitPoolNumber `json:"lbt"`
			OpenCount      limitPoolNumber `json:"zbc"`
			Industry       string          `json:"hybk"`
			Statistics     struct {
				Days  limitPoolNumber `json:"days"`
				Count limitPoolNumber `json:"ct"`
			} `json:"zttj"`
		} `json:"pool"`
	} `json:"data"`
}

type marketLimitPoolPayload struct {
	RC   int `json:"rc"`
	Data struct {
		Pool []struct {
			Code          string  `json:"c"`
			Name          string  `json:"n"`
			Price         float64 `json:"p"`
			ChangePercent float64 `json:"zdp"`
			Amount        float64 `json:"amount"`
			Industry      string  `json:"hybk"`
		} `json:"pool"`
	} `json:"data"`
}

func (c *Client) RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error) {
	history, err := c.RecentLimitUpHistory(ctx, lookbackDays)
	return history.Events, err
}

func (c *Client) RecentLimitUpHistory(ctx context.Context, days int) (foundation.LimitUpHistory, error) {
	return c.collectLimitUpHistory(ctx, days, nil)
}

func (c *Client) ProgressiveRecentLimitUpHistory(ctx context.Context, days int, publish func(foundation.LimitUpHistory)) (foundation.LimitUpHistory, error) {
	return c.collectLimitUpHistory(ctx, days, publish)
}

// ProgressiveRecentLimitUps also reports incomplete history, so comparisons
// are not calculated from a silently missing trading day.
func (c *Client) ProgressiveRecentLimitUps(ctx context.Context, days int, publish func([]foundation.LimitUpEvent)) ([]foundation.LimitUpEvent, error) {
	history, err := c.collectLimitUpHistory(ctx, days, func(value foundation.LimitUpHistory) {
		if publish != nil {
			publish(value.Events)
		}
	})
	return history.Events, err
}

func (c *Client) collectLimitUpHistory(ctx context.Context, lookbackDays int, publish func(foundation.LimitUpHistory)) (foundation.LimitUpHistory, error) {
	if err := ctx.Err(); err != nil {
		return foundation.LimitUpHistory{}, err
	}
	if lookbackDays <= 0 {
		lookbackDays = 12
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Now().In(location)
	requested := make([]string, 0, lookbackDays)
	for offset := lookbackDays - 1; offset >= 0; offset-- {
		date := now.AddDate(0, 0, -offset)
		if foundation.IsAStockTradingDay(date) {
			requested = append(requested, date.Format("2006-01-02"))
		}
	}
	covered := map[string]bool{}
	type dayResult struct {
		offset int
		events []foundation.LimitUpEvent
		err    error
	}
	results := make(chan dayResult, lookbackDays)
	jobs := make(chan int, lookbackDays)
	for offset := 0; offset < lookbackDays; offset++ {
		jobs <- offset
	}
	close(jobs)
	for worker := 0; worker < min(3, lookbackDays); worker++ {
		go func() {
			for offset := range jobs {
				if ctx.Err() != nil {
					return
				}
				date := now.AddDate(0, 0, -offset)
				if !foundation.IsAStockTradingDay(date) {
					results <- dayResult{offset: offset}
					continue
				}
				events, err := c.LimitUpPool(ctx, date)
				results <- dayResult{offset, events, err}
			}
		}()
	}
	days := make([][]foundation.LimitUpEvent, lookbackDays)
	collect := func() []foundation.LimitUpEvent {
		events := make([]foundation.LimitUpEvent, 0, 200)
		for offset := lookbackDays - 1; offset >= 0; offset-- {
			events = append(events, cloneLimitEvents(days[offset])...)
		}
		return events
	}
	var failures []error
	finish := func(cause error) (foundation.LimitUpHistory, error) {
		var missing, successful []string
		for _, date := range requested {
			if covered[date] {
				successful = append(successful, date)
			} else {
				missing = append(missing, date)
			}
		}
		events := collect()
		value := foundation.LimitUpHistory{Events: events, RequestedDates: append([]string(nil), requested...), CoveredDates: successful, MissingDates: missing,
			Meta: foundation.SourceMeta{Source: "eastmoney:limit-up-pool", Provider: "eastmoney", Partial: len(missing) > 0 || cause != nil}}
		// Do not claim a fresh network timestamp here: individual day calls may
		// have been served by the existing supplier day cache.
		for _, event := range events {
			if event.Meta.FetchedAt.After(value.Meta.FetchedAt) {
				value.Meta.FetchedAt = event.Meta.FetchedAt
			}
		}
		if len(missing) == 0 && cause == nil {
			return foundation.StampLimitUpHistory(value), nil
		}
		for i := range value.Events {
			value.Events[i].Meta.Partial = true
			value.Events[i].Meta.MissingIDs = append([]string(nil), missing...)
		}
		return foundation.StampLimitUpHistory(value), &foundation.LimitUpCoverageError{RequestedDates: append([]string(nil), requested...), CoveredDates: append([]string(nil), successful...), MissingDates: append([]string(nil), missing...), Cause: cause}
	}
	for remaining := lookbackDays; remaining > 0; remaining-- {
		if err := ctx.Err(); err != nil {
			return finish(errors.Join(append(failures, err)...))
		}
		select {
		case <-ctx.Done():
			return finish(errors.Join(append(failures, ctx.Err())...))
		case result := <-results:
			date := now.AddDate(0, 0, -result.offset)
			if result.err != nil {
				failures = append(failures, fmt.Errorf("%s 涨停池: %w", date.Format("2006-01-02"), result.err))
			} else if foundation.IsAStockTradingDay(date) {
				covered[date.Format("2006-01-02")] = true
			}
			days[result.offset] = result.events
			if publish != nil && foundation.IsAStockTradingDay(date) && ctx.Err() == nil {
				partial, _ := finish(nil)
				publish(partial)
			}
		}
	}
	return finish(errors.Join(failures...))
}

func (c *Client) fetchLimitUpPool(ctx context.Context, date time.Time) ([]foundation.LimitUpEvent, error) {
	endpoint := c.topicBaseURL + "/getTopicZTPool"
	params := url.Values{}
	params.Set("ut", "7eea3edcaed734bea9cbfc24409ed989")
	params.Set("dpt", "wz.ztzt")
	params.Set("Pageindex", "0")
	params.Set("pagesize", "500")
	params.Set("sort", "fbt:asc")
	params.Set("date", date.Format("20060102"))
	requestURL := endpoint + "?" + params.Encode()

	start := time.Now()
	var payload limitUpPoolPayload
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 {
		return nil, fmt.Errorf("eastmoney limit-up pool rc=%d", payload.RC)
	}
	if payload.Data == nil || payload.Data.Pool == nil {
		return nil, fmt.Errorf("eastmoney limit-up pool missing data/pool for %s", date.Format("2006-01-02"))
	}
	meta := foundation.SourceMeta{
		Source: "eastmoney:limit-up-pool", Provider: "eastmoney",
		SourceURL: requestURL, FetchedAt: time.Now(),
		TradeDate: date.Format("2006-01-02"), FieldsKnown: true,
		LatencyMS: time.Since(start).Milliseconds(),
	}
	events := make([]foundation.LimitUpEvent, 0, len(*payload.Data.Pool))
	for _, raw := range *payload.Data.Pool {
		symbol, err := normalizeEastMoneyStockCode(raw.Code)
		if err != nil {
			continue
		}
		event := foundation.LimitUpEvent{
			Symbol: symbol, Name: raw.Name, Date: date, Industry: raw.Industry,
			Meta: foundation.CloneSourceMeta(meta),
		}
		addNumber := func(field string, number limitPoolNumber, target *float64, divisor float64) {
			if number.valid {
				*target = number.value / divisor
				event.Meta.AvailableFields = append(event.Meta.AvailableFields, field)
			}
		}
		addInt := func(field string, number limitPoolNumber, target *int) {
			if number.validInt() {
				*target = int(number.value)
				event.Meta.AvailableFields = append(event.Meta.AvailableFields, field)
			}
		}
		addNumber("price", raw.Price, &event.Price, 1000)
		addNumber("change_percent", raw.ChangePercent, &event.ChangePercent, 1)
		addNumber("amount", raw.Amount, &event.Amount, 1)
		addNumber("float_market_cap", raw.FloatMarketCap, &event.FloatMarketCap, 1)
		addNumber("turnover_rate", raw.TurnoverRate, &event.TurnoverRate, 1)
		addInt("streak", raw.Streak, &event.Streak)
		addInt("open_count", raw.OpenCount, &event.OpenCount)
		addInt("days", raw.Statistics.Days, &event.Days)
		addInt("count", raw.Statistics.Count, &event.Count)
		if raw.FirstLimitTime.validInt() {
			event.FirstLimitTime = formatTradeClock(int(raw.FirstLimitTime.value))
		}
		if raw.LastLimitTime.validInt() {
			event.LastLimitTime = formatTradeClock(int(raw.LastLimitTime.value))
		}
		for _, field := range []struct{ key, value string }{{"name", event.Name}, {"industry", event.Industry}, {"first_limit_time", event.FirstLimitTime}, {"last_limit_time", event.LastLimitTime}} {
			if strings.TrimSpace(field.value) != "" && field.value != "--" {
				event.Meta.AvailableFields = append(event.Meta.AvailableFields, field.key)
			}
		}
		events = append(events, event)
	}
	if len(events) != len(*payload.Data.Pool) {
		return events, fmt.Errorf("eastmoney limit-up pool contains invalid stock rows for %s", date.Format("2006-01-02"))
	}
	return events, nil
}

// BrokenLimitUpPool returns stocks that touched their limit-up price but did
// not remain sealed. This is the numerator of the final broken-board rate and
// must not be confused with LimitUpEvent.OpenCount, which describes stocks
// that reopened but eventually sealed again.
func (c *Client) BrokenLimitUpPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error) {
	return c.marketLimitPool(ctx, date, "/getTopicZBPool", "fbt:asc", "eastmoney:broken-limit-up-pool")
}

// LimitDownPool returns the final daily limit-down pool. The endpoint does not
// support the limit-up pool's fbt sort field; fund:asc is intentionally used so
// a non-empty pool is not silently returned as an empty response.
func (c *Client) LimitDownPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error) {
	return c.marketLimitPool(ctx, date, "/getTopicDTPool", "fund:asc", "eastmoney:limit-down-pool")
}

func (c *Client) marketLimitPool(
	ctx context.Context,
	date time.Time,
	path string,
	sortValue string,
	source string,
) ([]foundation.MarketLimitEvent, error) {
	endpoint := c.topicBaseURL + path
	params := url.Values{}
	params.Set("ut", "7eea3edcaed734bea9cbfc24409ed989")
	params.Set("dpt", "wz.ztzt")
	params.Set("Pageindex", "0")
	params.Set("pagesize", "500")
	params.Set("sort", sortValue)
	params.Set("date", date.Format("20060102"))
	requestURL := endpoint + "?" + params.Encode()

	start := time.Now()
	var payload marketLimitPoolPayload
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 {
		return nil, fmt.Errorf("eastmoney market limit pool rc=%d", payload.RC)
	}
	meta := foundation.SourceMeta{
		Source:    source,
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	events := make([]foundation.MarketLimitEvent, 0, len(payload.Data.Pool))
	for _, raw := range payload.Data.Pool {
		symbol, err := normalizeEastMoneyStockCode(raw.Code)
		if err != nil {
			continue
		}
		events = append(events, foundation.MarketLimitEvent{
			Symbol:        symbol,
			Name:          raw.Name,
			Date:          date,
			Price:         raw.Price / 1000,
			ChangePercent: raw.ChangePercent,
			Amount:        raw.Amount,
			Industry:      raw.Industry,
			Meta:          meta,
		})
	}
	return events, nil
}

func formatTradeClock(value int) string {
	if value <= 0 || value > 235959 || value/100%100 > 59 || value%100 > 59 {
		return ""
	}
	raw := fmt.Sprintf("%06d", value)
	return raw[0:2] + ":" + raw[2:4] + ":" + raw[4:6]
}

type limitUpDayFlight struct {
	done    chan struct{}
	events  []foundation.LimitUpEvent
	err     error
	expires time.Time
}

func (c *Client) LimitUpPool(ctx context.Context, date time.Time) ([]foundation.LimitUpEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := date.Format("2006-01-02")
	c.poolMu.Lock()
	if f := c.limitUpDays[key]; f != nil && (f.expires.IsZero() || time.Now().Before(f.expires)) {
		c.poolMu.Unlock()
		select {
		case <-f.done:
			return cloneLimitEvents(f.events), f.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if c.limitUpDays == nil {
		c.limitUpDays = make(map[string]*limitUpDayFlight)
	}
	for day, flight := range c.limitUpDays {
		if !flight.expires.IsZero() && time.Now().After(flight.expires) {
			delete(c.limitUpDays, day)
		}
	}
	f := &limitUpDayFlight{done: make(chan struct{})}
	c.limitUpDays[key] = f
	c.poolMu.Unlock()
	start := time.Now()
	f.events, f.err = c.fetchLimitUpPool(ctx, date)
	ttl := 30 * time.Second
	if key < time.Now().In(date.Location()).Format("2006-01-02") && len(f.events) > 0 && f.err == nil {
		ttl = 6 * time.Hour
	}
	if f.err != nil {
		ttl = 5 * time.Second
	}
	c.poolMu.Lock()
	f.expires = time.Now().Add(ttl)
	close(f.done)
	c.poolMu.Unlock()
	log.Printf("event=limit_up_stage stage=eastmoney_day date=%s duration_ms=%d failed=%t", key, time.Since(start).Milliseconds(), f.err != nil)
	return cloneLimitEvents(f.events), f.err
}

func cloneLimitEvents(events []foundation.LimitUpEvent) []foundation.LimitUpEvent {
	result := append([]foundation.LimitUpEvent(nil), events...)
	for i := range result {
		result[i].Concepts = append([]string(nil), result[i].Concepts...)
		result[i].Meta = foundation.CloneSourceMeta(result[i].Meta)
	}
	return result
}
