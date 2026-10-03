package tencent

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

var industryMomentumFields = []string{
	"change_percent", "five_day_change_percent", "twenty_day_change_percent",
	"leader_name", "leader_change_percent",
}

func (c *Client) IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 150 {
		limit = 150
	}
	values := url.Values{}
	values.Set("l", strconv.Itoa(limit))
	values.Set("p", "1")
	values.Set("t", "01/averatio")
	values.Set("ordertype", "")
	values.Set("o", "0")
	requestURL := c.industryBaseURL + "?" + values.Encode()
	start := time.Now()
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Name          string         `json:"bd_name"`
			Code          string         `json:"bd_code"`
			Price         string         `json:"bd_zxj"`
			ChangePercent industryNumber `json:"bd_zdf"`
			FiveDay       industryNumber `json:"bd_zdf5"`
			TwentyDay     industryNumber `json:"bd_zdf20"`
			LeaderCode    string         `json:"nzg_code"`
			LeaderName    string         `json:"nzg_name"`
			LeaderPrice   string         `json:"nzg_zxj"`
			LeaderChange  industryNumber `json:"nzg_zdf"`
		} `json:"data"`
	}
	if err := c.getIndustryJSON(ctx, requestURL, &payload); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	if payload.Code != 0 || len(payload.Data) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("tencent industry rank code=%d: %s", payload.Code, payload.Msg)
	}
	meta := foundation.SourceMeta{
		Source: "tencent:industry-rank", SourceURL: requestURL, FieldsKnown: true, Provider: "tencent",
		FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(),
	}
	items := make([]foundation.MarketIndustryMomentum, 0, len(payload.Data))
	for _, raw := range payload.Data {
		if strings.TrimSpace(raw.Code) == "" || strings.TrimSpace(raw.Name) == "" {
			continue
		}
		rowMeta := meta
		rowMeta.NativeCode = raw.Code
		rowMeta.AvailableFields = nil
		change := raw.ChangePercent.value(&rowMeta, "change_percent")
		fiveDay := raw.FiveDay.value(&rowMeta, "five_day_change_percent")
		twentyDay := raw.TwentyDay.value(&rowMeta, "twenty_day_change_percent")
		leaderChange := raw.LeaderChange.value(&rowMeta, "leader_change_percent")
		if strings.TrimSpace(raw.LeaderName) != "" {
			rowMeta.AvailableFields = append(rowMeta.AvailableFields, "leader_name")
		}
		// Missing changes must not enter the score formula as zero; valid zero is allowed.
		score := 0.0
		if raw.ChangePercent.Valid && raw.FiveDay.Valid && raw.TwentyDay.Valid {
			score = tencentIndustryScore(change, fiveDay, twentyDay)
			rowMeta.AvailableFields = append(rowMeta.AvailableFields, "score")
		}
		leaderSymbol := ""
		if normalized, err := foundation.NormalizeSymbol(raw.LeaderCode); err == nil {
			leaderSymbol = normalized.Canonical
		}
		items = append(items, foundation.MarketIndustryMomentum{
			Code: raw.Code, Name: raw.Name, ChangePercent: change, FiveDayChangePercent: fiveDay,
			TwentyDayChange: twentyDay, LeaderSymbol: leaderSymbol, LeaderName: raw.LeaderName, LeaderChangePercent: leaderChange,
			Score: score, Meta: rowMeta,
		})
	}
	if len(items) == 0 {
		return nil, meta, fmt.Errorf("tencent industry rank returned no identified industries")
	}
	meta.AvailableFields = append([]string(nil), items[0].Meta.AvailableFields...)
	for _, item := range items[1:] {
		meta.AvailableFields = intersectIndustryFields(meta.AvailableFields, item.Meta.AvailableFields)
	}
	return items, meta, nil
}

// industryNumber retains presence independently of value, including valid zero.
type industryNumber struct {
	Value float64
	Valid bool
}

func (number *industryNumber) UnmarshalJSON(data []byte) error {
	number.Value, number.Valid = 0, false
	raw := strings.TrimSpace(string(data))
	if strings.HasPrefix(raw, "\"") {
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
		number.Value, number.Valid = value, true
	}
	return nil
}

func (number industryNumber) value(meta *foundation.SourceMeta, field string) float64 {
	if number.Valid {
		meta.AvailableFields = append(meta.AvailableFields, field)
	}
	return number.Value
}

func intersectIndustryFields(left, right []string) []string {
	result := make([]string, 0, len(left))
	for _, field := range left {
		for _, available := range right {
			if field == available {
				result = append(result, field)
				break
			}
		}
	}
	return result
}

func (c *Client) getIndustryJSON(ctx context.Context, requestURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "https://stockapp.finance.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tencent industry http status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func tencentIndustryScore(change, fiveDay, twentyDay float64) float64 {
	score := 50 + change*6 + fiveDay*2 + twentyDay*.8
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}
