package tencent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// StockKLineClient is separate from the index adapter: a matching adjustment
// label is not evidence of equivalent factors or anchoring across suppliers.
type StockKLineClient struct{ client *Client }

func NewStockKLineClient(client *Client) *StockKLineClient {
	if client == nil {
		client = NewClient()
	}
	return &StockKLineClient{client: client}
}

func stockKLinePeriod(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "day", "daily", "101":
		return "day"
	case "week", "weekly", "102":
		return "week"
	case "month", "monthly", "103":
		return "month"
	default:
		return ""
	}
}

// Beijing, minute bars and indexes are not claimed by this stock capability.
func SupportsStockKLine(symbol, period, adjustment string) bool {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil || len(normalized.RawCode) != 6 || stockKLinePeriod(period) == "" {
		return false
	}
	if adjustment != "none" && adjustment != "qfq" && adjustment != "hfq" {
		return false
	}
	_, known := stockKLineVolumeFactor(normalized)
	return known
}

// Tencent's stock endpoint does not use a single A-share volume convention:
// STAR 688 quotes/bars use shares, while sampled main-board/ChiNext use lots.
// Restrict to these stock board families; indexes, depositary receipts and new
// board prefixes require independent identity/unit verification.
func stockKLineVolumeFactor(symbol foundation.Symbol) (float64, bool) {
	if symbol.Market == "SH" {
		if strings.HasPrefix(symbol.RawCode, "688") {
			return 1, true
		}
		if strings.HasPrefix(symbol.RawCode, "60") {
			return 100, true
		}
	}
	if symbol.Market == "SZ" && (strings.HasPrefix(symbol.RawCode, "00") || strings.HasPrefix(symbol.RawCode, "30")) {
		return 100, true
	}
	return 0, false
}

func (c *StockKLineClient) SupportsKLine(symbol, period string) bool {
	return SupportsStockKLine(symbol, period, "none")
}
func (c *StockKLineClient) SupportsAdjustedKLine(symbol, period, adjustment string) bool {
	return SupportsStockKLine(symbol, period, adjustment)
}
func (c *StockKLineClient) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	return c.KLineAdjusted(ctx, symbol, period, limit, "none")
}

func (c *StockKLineClient) KLineAdjusted(ctx context.Context, symbol, period string, limit int, adjustment string) ([]foundation.KLine, error) {
	if !SupportsStockKLine(symbol, period, adjustment) {
		return nil, fmt.Errorf("unsupported Tencent stock market, period or adjustment")
	}
	normalized, _ := foundation.NormalizeSymbol(symbol)
	period = stockKLinePeriod(period)
	if limit <= 0 {
		limit = 120
	}
	limit = min(limit, 2000)
	nativeCode := strings.ToLower(normalized.Market) + normalized.RawCode
	suffix := adjustment
	key := adjustment + period
	if adjustment == "none" {
		suffix, key = "", period
	}
	params := url.Values{}
	params.Set("param", fmt.Sprintf("%s,%s,,,%d,%s", nativeCode, period, limit, suffix))
	requestURL := c.client.klineBaseURL + "?" + params.Encode()
	started := time.Now()
	var payload struct {
		Code int                                   `json:"code"`
		Data map[string]map[string]json.RawMessage `json:"data"`
	}
	if err := c.client.getJSON(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.Code != 0 {
		return nil, fmt.Errorf("%w: tencent stock kline code=%d", foundation.ErrInvalidPriceData, payload.Code)
	}
	raw, ok := payload.Data[nativeCode][key]
	// Never accept an unadjusted key when the requested qfq/hfq key is absent.
	if !ok {
		return nil, fmt.Errorf("%w: tencent stock response missing requested %s series", foundation.ErrPriceNoData, key)
	}
	var rows [][]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("tencent stock %s decode: %w", key, err)
	}
	if err := validateStockQuoteVolumeUnit(payload.Data[nativeCode]["qt"], nativeCode, normalized); err != nil {
		return nil, err
	}
	meta := foundation.SourceMeta{
		Source: "tencent:stock-kline", Provider: "tencent", SourceURL: requestURL,
		NativeCode: nativeCode, InstrumentID: normalized.Canonical, Period: period,
		RequestedAdjustment: adjustment, EffectiveAdjustment: adjustment,
		AdjustmentConvention: "tencent:fqkline:provider-current", BasisID: "tencent:fqkline:" + adjustment + ":provider-current",
		TimeZone: "Asia/Shanghai", VolumeUnit: "shares", AmountCurrency: "CNY",
		FieldsKnown: true, AvailableFields: []string{"open", "high", "low", "close", "volume"},
		FetchedAt: time.Now(), LatencyMS: time.Since(started).Milliseconds(),
	}
	return parseStockKLines(rows, normalized.Canonical, adjustment, limit, meta)
}

// A current raw quote can corroborate the stock endpoint's board volume unit,
// including adjusted/week/month requests. It is not used to invent bar amount.
// Missing or empty quote snapshots cannot validate a session (e.g. suspension);
// contradictory usable snapshots fail closed instead of silently converting.
func validateStockQuoteVolumeUnit(raw json.RawMessage, nativeCode string, symbol foundation.Symbol) (validationErr error) {
	defer func() {
		if validationErr != nil {
			validationErr = fmt.Errorf("%w: %v", foundation.ErrInvalidPriceData, validationErr)
		}
	}()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var quotes map[string]json.RawMessage
	if err := json.Unmarshal(raw, &quotes); err != nil {
		return fmt.Errorf("invalid Tencent embedded quote schema: %w", err)
	}
	quoteRaw := quotes[nativeCode]
	if len(quoteRaw) == 0 {
		return nil
	}
	var fields []string
	if err := json.Unmarshal(quoteRaw, &fields); err != nil {
		return fmt.Errorf("invalid Tencent embedded stock quote: %w", err)
	}
	if len(fields) <= 35 {
		return nil
	}
	triple := strings.Split(fields[35], "/")
	if len(triple) != 3 {
		return nil
	}
	volume, volumeErr := strconv.ParseFloat(triple[1], 64)
	amount, amountErr := strconv.ParseFloat(triple[2], 64)
	high, highErr := strconv.ParseFloat(fields[33], 64)
	low, lowErr := strconv.ParseFloat(fields[34], 64)
	if volumeErr != nil || amountErr != nil || highErr != nil || lowErr != nil || volume <= 0 || amount <= 0 || low <= 0 || high < low {
		return nil
	}
	for _, value := range []float64{volume, amount, high, low} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("nonfinite Tencent stock quote unit evidence")
		}
	}
	factor, known := stockKLineVolumeFactor(symbol)
	if !known {
		return fmt.Errorf("Tencent stock board unit is not verified")
	}
	average := amount / volume / factor
	if math.IsNaN(average) || math.IsInf(average, 0) || average < low*.98 || average > high*1.02 {
		return fmt.Errorf("Tencent raw quote contradicts expected stock board volume unit")
	}
	return nil
}

func parseStockKLines(rows [][]any, symbol, adjustment string, limit int, meta foundation.SourceMeta) (result []foundation.KLine, parseErr error) {
	defer func() {
		if parseErr != nil && !errors.Is(parseErr, foundation.ErrPriceNoData) {
			parseErr = fmt.Errorf("%w: %v", foundation.ErrInvalidPriceData, parseErr)
		}
	}()
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: tencent returned no stock kline bars", foundation.ErrPriceNoData)
	}
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	volumeFactor, known := stockKLineVolumeFactor(normalized)
	if !known {
		return nil, fmt.Errorf("Tencent stock volume convention unverified for this board")
	}
	meta.VolumeUnit = "shares"
	lines := make([]foundation.KLine, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if len(row) < 6 {
			return nil, fmt.Errorf("tencent stock kline has fewer than six fields")
		}
		date := strings.TrimSpace(fmt.Sprint(row[0]))
		moment, err := time.ParseInLocation("2006-01-02", date, time.FixedZone("Asia/Shanghai", 8*60*60))
		if err != nil || seen[date] {
			return nil, fmt.Errorf("invalid or duplicate Tencent stock bar date %q", date)
		}
		seen[date] = true
		numbers := make([]float64, 5)
		for index := range numbers {
			value, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(row[index+1])), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("invalid Tencent stock kline numeric field")
			}
			numbers[index] = value
		}
		open, closePrice, high, low, nativeVolume := numbers[0], numbers[1], numbers[2], numbers[3], numbers[4]
		if high < max(open, closePrice) || low > min(open, closePrice) || high < low || nativeVolume < 0 || math.IsInf(nativeVolume*volumeFactor, 0) {
			return nil, fmt.Errorf("invalid Tencent stock OHLC or volume")
		}
		if adjustment == "none" && low <= 0 {
			return nil, fmt.Errorf("nonpositive Tencent unadjusted transaction price")
		}
		// Use the verified board-specific convention (STAR already shares).
		// No historical amount is provided; a current qt snapshot must not be
		// copied onto historical/weekly/monthly or adjusted bars.
		lines = append(lines, foundation.KLine{Symbol: symbol, Time: moment, Open: open, Close: closePrice, High: high, Low: low, Volume: nativeVolume * volumeFactor, Meta: meta})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Time.Before(lines[j].Time) })
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	tradeDate := lines[len(lines)-1].Time.Format("2006-01-02")
	for index := range lines {
		lines[index].Meta.TradeDate = tradeDate
		lines[index].Meta.Partial = limit > 0 && len(lines) < limit
		if index > 0 {
			lines[index].PreviousClose = lines[index-1].Close
			lines[index].Meta.AvailableFields = append(append([]string(nil), meta.AvailableFields...), "previous_close")
			if lines[index].PreviousClose > 0 {
				lines[index].ChangePercent = (lines[index].Close/lines[index].PreviousClose - 1) * 100
				lines[index].Meta.AvailableFields = append(lines[index].Meta.AvailableFields, "change_percent")
			}
		}
	}
	return lines, nil
}
