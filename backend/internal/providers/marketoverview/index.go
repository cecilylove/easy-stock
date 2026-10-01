package marketoverview

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/tencent"
)

var ErrUnsupportedIndexSeries = errors.New("unsupported index or period")

// Legacy nasdaq was NDX, never the Nasdaq Composite.
func CanonicalIndexID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	switch id {
	case "nasdaq", "ndx":
		return "nasdaq100"
	case "ixic":
		return "nasdaq_composite"
	}
	return id
}

// Preserve date-period aliases, not retired minute-index capabilities.
func CanonicalIndexPeriod(period string) string {
	period = strings.ToLower(strings.TrimSpace(period))
	switch period {
	case "", "day", "daily", "101":
		return "day"
	case "week", "weekly", "102":
		return "week"
	case "month", "monthly", "103":
		return "month"
	}
	return period
}

func SupportsIndexSeries(id, period string) bool {
	return tencent.SupportsIndexSeries(CanonicalIndexID(id), CanonicalIndexPeriod(period))
}

func expectedIndexIDs(scope string) []string {
	core := []string{"sse", "szse", "chinext", "csi300", "sse50", "csi1000", "star50", "hsi", "dow", "sp500", "nasdaq100", "nasdaq_composite"}
	if scope == "core" {
		return core
	}
	return append(core, "nikkei", "kospi", "taiwan", "ftse", "dax", "cac")
}

// Indexes have one named source. Bound its request without reserving time for
// a retired alternate source, and always respect parent cancellation/deadline.
func indexRequestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 10*time.Second)
}

func sourceAttempt(meta foundation.SourceMeta, source, capability string, err error) foundation.SourceObservation {
	meta.Capability = capability
	return foundation.SourceObservation{Meta: meta, SourceID: source, Capability: capability, AttemptAt: time.Now(), Failed: err != nil}
}

func (p *Provider) indexSnapshots(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "tencent:index", Capability: "index-snapshot/" + scope, FetchedAt: time.Now()}
	if err := ctx.Err(); err != nil {
		return nil, meta, err
	}
	if p.indexProvider == nil {
		return nil, meta, fmt.Errorf("Tencent index provider is unavailable")
	}
	attemptCtx, cancel := indexRequestContext(ctx)
	raw, tMeta, err := p.indexProvider.MarketIndexes(attemptCtx, scope)
	cancel()
	expected := expectedIndexIDs(scope)
	wanted := map[string]bool{}
	for _, id := range expected {
		wanted[id] = true
	}
	byID := map[string]foundation.MarketIndexSnapshot{}
	if err == nil {
		for _, item := range raw {
			id := CanonicalIndexID(item.ID)
			if !wanted[id] || item.Price <= 0 || math.IsNaN(item.Price) || math.IsInf(item.Price, 0) {
				continue
			}
			if (id == "nasdaq100" && item.Code != "NDX" && item.Code != ".NDX" && item.SecID != "usNDX") || (id == "nasdaq_composite" && item.Code != "IXIC" && item.Code != ".IXIC" && item.SecID != "usIXIC") {
				continue
			}
			if _, exists := byID[id]; exists {
				continue
			}
			item.ID = id
			if item.Meta.Source == "" {
				item.Meta = tMeta
			}
			item.Meta.Capability = meta.Capability
			byID[id] = item
		}
		if len(byID) == 0 {
			err = fmt.Errorf("Tencent returned no valid index snapshots")
		}
	}
	meta.Observations = []foundation.SourceObservation{sourceAttempt(tMeta, "tencent", meta.Capability, err)}
	if parentErr := ctx.Err(); parentErr != nil {
		return nil, meta, parentErr
	}
	if err != nil {
		return nil, meta, fmt.Errorf("Tencent index snapshots unavailable: %w", err)
	}
	if tMeta.Source != "" {
		meta.Source = tMeta.Source
		meta.SourceURL = tMeta.SourceURL
		meta.FetchedAt = tMeta.FetchedAt
		meta.LatencyMS = tMeta.LatencyMS
	}
	items := make([]foundation.MarketIndexSnapshot, 0, len(byID))
	for _, id := range expected {
		if item, ok := byID[id]; ok {
			items = append(items, item)
		} else {
			meta.MissingIDs = append(meta.MissingIDs, id)
		}
	}
	meta.Partial = len(meta.MissingIDs) > 0
	if meta.Partial {
		meta.FallbackReason = "腾讯指数目录部分覆盖，缺失ID: " + strings.Join(meta.MissingIDs, ",")
	}
	return items, meta, nil
}

func validIndexSeries(series foundation.MarketIndexSeries, id string) bool {
	if CanonicalIndexID(series.Index.ID) != id || len(series.Lines) == 0 {
		return false
	}
	if id == "nasdaq100" && series.Index.Code != "NDX" && series.Index.Code != ".NDX" && series.Index.SecID != "usNDX" {
		return false
	}
	if id == "nasdaq_composite" && series.Index.Code != "IXIC" && series.Index.Code != ".IXIC" && series.Index.SecID != "usIXIC" {
		return false
	}
	for i, line := range series.Lines {
		if line.Time.IsZero() || line.Close <= 0 || math.IsNaN(line.Close) || math.IsInf(line.Close, 0) || (i > 0 && !line.Time.After(series.Lines[i-1].Time)) {
			return false
		}
	}
	return true
}

func (p *Provider) indexSeries(ctx context.Context, id, period string, limit int) (foundation.MarketIndexSeries, error) {
	id, period = CanonicalIndexID(id), CanonicalIndexPeriod(period)
	if !SupportsIndexSeries(id, period) {
		return foundation.MarketIndexSeries{}, fmt.Errorf("%w: id=%s period=%s; 腾讯指数仅支持目录内日/周/月 K", ErrUnsupportedIndexSeries, id, period)
	}
	if err := ctx.Err(); err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	if p.indexProvider == nil {
		return foundation.MarketIndexSeries{}, fmt.Errorf("Tencent index provider is unavailable")
	}
	capability := "index-kline/" + period
	attemptCtx, cancel := indexRequestContext(ctx)
	series, err := p.indexProvider.MarketIndexSeries(attemptCtx, id, period, limit)
	cancel()
	if err == nil && !validIndexSeries(series, id) {
		err = fmt.Errorf("Tencent returned empty or mismatched index history")
	}
	series.Meta.Capability = capability
	series.Meta.Observations = []foundation.SourceObservation{sourceAttempt(series.Meta, "tencent", capability, err)}
	if parentErr := ctx.Err(); parentErr != nil {
		return series, parentErr
	}
	if err != nil {
		return series, fmt.Errorf("Tencent index history unavailable: %w", err)
	}
	series.Index.Meta.Capability = capability
	for i := range series.Lines {
		series.Lines[i].Meta.Capability = capability
	}
	return series, nil
}
