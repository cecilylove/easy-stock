package tencent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// PriceKLineClient routes stocks and known research benchmarks without
// broadening the stock adapter to indexes or guessing index volume units.
type PriceKLineClient struct {
	client *Client
	stocks *StockKLineClient
}

func NewPriceKLineClient(client *Client) *PriceKLineClient {
	if client == nil {
		client = NewClient()
	}
	return &PriceKLineClient{client: client, stocks: NewStockKLineClient(client)}
}

func BenchmarkIndexID(symbol string) (string, bool) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return "", false
	}
	ids := map[string]string{"000001.SH": "sse", "000300.SH": "csi300", "000016.SH": "sse50", "000852.SH": "csi1000", "000688.SH": "star50", "399001.SZ": "szse", "399006.SZ": "chinext"}
	id, ok := ids[normalized.Canonical]
	return id, ok
}

func (c *PriceKLineClient) SupportsKLine(symbol, period string) bool {
	if id, ok := BenchmarkIndexID(symbol); ok {
		return SupportsIndexSeries(id, stockKLinePeriod(period)) && stockKLinePeriod(period) != ""
	}
	return c.stocks.SupportsKLine(symbol, period)
}
func (c *PriceKLineClient) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	id, index := BenchmarkIndexID(symbol)
	if !index {
		return c.stocks.KLine(ctx, symbol, period, limit)
	}
	normalized, _ := foundation.NormalizeSymbol(symbol)
	period = stockKLinePeriod(period)
	if period == "" {
		return nil, fmt.Errorf("unsupported benchmark period")
	}
	series, err := c.client.MarketIndexSeries(ctx, id, period, limit)
	if err != nil {
		return nil, err
	}
	if len(series.Lines) == 0 {
		return nil, fmt.Errorf("%w: benchmark has no series", foundation.ErrPriceNoData)
	}
	if series.Index.ID != id {
		return nil, fmt.Errorf("%w: benchmark index identity mismatch", foundation.ErrInvalidPriceData)
	}
	lines := append([]foundation.KLine(nil), series.Lines...)
	// Preserve the date label, not a UTC instant shifted by the host timezone.
	china := time.FixedZone("Asia/Shanghai", 8*60*60)
	for i := range lines {
		bar := &lines[i]
		if bar.Symbol != id || bar.Time.IsZero() || !strings.HasPrefix(bar.Meta.Source, "tencent:index") {
			return nil, fmt.Errorf("%w: benchmark supplier identity mismatch", foundation.ErrInvalidPriceData)
		}
		year, month, day := bar.Time.Date()
		bar.Time = time.Date(year, month, day, 0, 0, 0, 0, china)
		bar.Symbol = normalized.Canonical
		bar.Meta.InstrumentID = id
		bar.Meta.TimeZone = "Asia/Shanghai"
		bar.Meta.AmountCurrency = "CNY"
		bar.Meta.VolumeUnit = "provider_index_volume"
		bar.Meta.EffectiveAdjustment = "none"
		bar.Meta.AdjustmentConvention = "tencent:index:unadjusted"
		bar.Meta.BasisID = "tencent:index:" + id + ":none"
		if i > 0 {
			bar.PreviousClose = lines[i-1].Close
			bar.Meta.AvailableFields = append(append([]string(nil), bar.Meta.AvailableFields...), "previous_close")
		}
	}
	return lines, nil
}
