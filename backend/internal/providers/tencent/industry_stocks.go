package tencent

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

const maxIndustryStockPageSize = 200

type industryStockResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Total    int `json:"total"`
		RankList []struct {
			Code           string         `json:"code"`
			Name           string         `json:"name"`
			Price          industryNumber `json:"zxj"`
			Change         industryNumber `json:"zd"`
			ChangePercent  industryNumber `json:"zdf"`
			Turnover       industryNumber `json:"turnover"`
			Amount         industryNumber `json:"volume"`
			TotalMarketCap industryNumber `json:"zsz"`
			FloatMarketCap industryNumber `json:"ltsz"`
		} `json:"rank_list"`
	} `json:"data"`
}

func (c *Client) IndustryStocks(ctx context.Context, industryCode string, limit int) ([]foundation.BoardStock, foundation.SourceMeta, error) {
	industryCode = strings.TrimSpace(industryCode)
	if !strings.HasPrefix(industryCode, "pt") {
		return nil, foundation.SourceMeta{}, fmt.Errorf("tencent native industry code is required")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	start := time.Now()
	meta := foundation.SourceMeta{
		Source: "tencent:industry-constituents", Provider: "tencent", NativeCode: industryCode,
		FieldsKnown: true, VolumeUnit: "shares", AmountCurrency: "CNY", TimeZone: "Asia/Shanghai",
		MemberSet: &foundation.MemberSetMeta{
			Kind: "native", Scope: "supported_a_shares", Method: "native_board_code",
			BoardRef: foundation.BoardRef{Provider: "tencent", NativeCode: industryCode, Dimension: "industry"},
		},
	}
	stocks := make([]foundation.BoardStock, 0, limit)
	seen := make(map[string]bool, limit)
	offset, skipped := 0, 0
	exhausted := false
	var partialErr error
	for offset < limit {
		if err := ctx.Err(); err != nil {
			return nil, meta, err
		}
		count := min(maxIndustryStockPageSize, limit-offset)
		values := url.Values{}
		values.Set("_appver", "11.17.0")
		values.Set("board_code", industryCode)
		values.Set("sort_type", "priceRatio")
		values.Set("direct", "down")
		values.Set("offset", strconv.Itoa(offset))
		values.Set("count", strconv.Itoa(count))
		pageURL := c.industryStocksBaseURL + "?" + values.Encode()
		if meta.SourceURL == "" {
			meta.SourceURL = pageURL
		}
		var payload industryStockResponse
		if err := c.getIndustryJSON(ctx, pageURL, &payload); err != nil {
			if ctx.Err() != nil {
				return nil, meta, ctx.Err()
			}
			partialErr = err
			break
		}
		if payload.Code != 0 {
			partialErr = fmt.Errorf("tencent industry stocks code=%d: %s", payload.Code, payload.Msg)
			break
		}
		if payload.Data.Total > 0 {
			if meta.MemberSet.Total != 0 && meta.MemberSet.Total != payload.Data.Total {
				partialErr = fmt.Errorf("tencent industry total changed during pagination")
				break
			}
			meta.MemberSet.Total = payload.Data.Total
		}
		if len(payload.Data.RankList) == 0 {
			exhausted = true
			break
		}
		newRows := 0
		for _, raw := range payload.Data.RankList {
			symbol, err := foundation.NormalizeSymbol(raw.Code)
			if err != nil || !isIndustryAStock(symbol) {
				skipped++
				continue
			}
			if seen[symbol.Canonical] {
				skipped++
				continue
			}
			if len(stocks) >= limit {
				break
			}
			seen[symbol.Canonical] = true
			newRows++
			rowMeta := meta
			rowMeta.AvailableFields = nil
			stocks = append(stocks, foundation.BoardStock{
				Symbol: symbol.Canonical, Name: strings.TrimSpace(raw.Name),
				Price: raw.Price.value(&rowMeta, "price"), Change: raw.Change.value(&rowMeta, "change"),
				ChangePercent:  raw.ChangePercent.value(&rowMeta, "change_percent"),
				Volume:         raw.Turnover.value(&rowMeta, "volume") * 100,
				Amount:         raw.Amount.value(&rowMeta, "amount") * 10_000,
				TotalMarketCap: raw.TotalMarketCap.value(&rowMeta, "total_market_cap") * 100_000_000,
				FloatMarketCap: raw.FloatMarketCap.value(&rowMeta, "float_market_cap") * 100_000_000,
				Meta:           rowMeta,
			})
		}
		offset += len(payload.Data.RankList)
		if newRows == 0 {
			partialErr = fmt.Errorf("tencent industry pagination returned no new supported symbols")
			break
		}
		if (meta.MemberSet.Total > 0 && offset >= meta.MemberSet.Total) || len(payload.Data.RankList) < count {
			exhausted = true
			break
		}
	}
	meta.MemberSet.Returned = len(stocks)
	meta.MemberSet.Complete = exhausted && partialErr == nil && skipped == 0 && meta.MemberSet.Total > 0 && len(stocks) == meta.MemberSet.Total
	meta.MemberSet.HasMore = (meta.MemberSet.Total > 0 && offset < meta.MemberSet.Total) || (meta.MemberSet.Total == 0 && !exhausted)
	meta.Partial = !meta.MemberSet.Complete
	meta.FetchedAt, meta.LatencyMS = time.Now(), time.Since(start).Milliseconds()
	if partialErr != nil {
		meta.FallbackReason = "腾讯行业成分仅取得部分页面：" + partialErr.Error()
	}
	if len(stocks) == 0 {
		if partialErr != nil {
			return nil, meta, partialErr
		}
		return nil, meta, fmt.Errorf("tencent industry stocks returned no supported A-share symbols")
	}
	meta.AvailableFields = append([]string(nil), stocks[0].Meta.AvailableFields...)
	for _, stock := range stocks[1:] {
		meta.AvailableFields = intersectIndustryFields(meta.AvailableFields, stock.Meta.AvailableFields)
	}
	for i := range stocks {
		fields := stocks[i].Meta.AvailableFields
		stocks[i].Meta = meta
		stocks[i].Meta.AvailableFields = fields
	}
	return stocks, meta, nil
}

func isIndustryAStock(symbol foundation.Symbol) bool {
	code := symbol.RawCode
	if len(code) != 6 {
		return false
	}
	switch symbol.Market {
	case "SH":
		for _, prefix := range []string{"600", "601", "603", "605", "688", "689"} {
			if strings.HasPrefix(code, prefix) {
				return true
			}
		}
	case "SZ":
		for _, prefix := range []string{"000", "001", "002", "003", "300", "301"} {
			if strings.HasPrefix(code, prefix) {
				return true
			}
		}
	case "BJ":
		return strings.HasPrefix(code, "4") || strings.HasPrefix(code, "8") || strings.HasPrefix(code, "9")
	}
	return false
}
