package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
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
	id, period = CanonicalIndexID(id), CanonicalIndexPeriod(period)
	for _, candidate := range append(expectedIndexIDs("core"), "ftse") {
		if id == candidate {
			return period == "day" || period == "week" || period == "month"
		}
	}
	return false
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
	if source == "" {
		source, _, _ = strings.Cut(meta.Source, ":")
	}
	if meta.Source == "" {
		meta.Source = source
	}
	meta.Capability = capability
	return foundation.SourceObservation{Meta: meta, SourceID: source, Capability: capability, AttemptAt: time.Now(), Failed: err != nil}
}

func (p *Market) indexSnapshots(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Capability: "index-snapshot/" + scope}
	if p.config.IndexSourceID != "" {
		meta.Source = p.config.IndexSourceID + ":index"
	}
	if err := ctx.Err(); err != nil {
		return nil, meta, err
	}
	if p.indexProvider == nil {
		return nil, meta, &contracts.Error{Kind: contracts.Unsupported, SourceID: p.config.IndexSourceID, Capability: meta.Capability}
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
			err = &contracts.Error{Kind: contracts.NoData, SourceID: p.config.IndexSourceID, Capability: meta.Capability}
		}
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
		meta.Observations = []foundation.SourceObservation{sourceAttempt(tMeta, p.config.IndexSourceID, meta.Capability, err)}
	}
	if parentErr := ctx.Err(); parentErr != nil {
		return nil, meta, parentErr
	}
	if err != nil {
		return nil, meta, fmt.Errorf("%s index snapshots unavailable: %w", sourceLabel(p.config.IndexSourceID), err)
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
		meta.FallbackReason = sourceLabel(p.config.IndexSourceID) + "指数目录部分覆盖，缺失ID: " + strings.Join(meta.MissingIDs, ",")
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

func (p *Market) indexSeries(ctx context.Context, id, period string, limit int) (foundation.MarketIndexSeries, error) {
	id, period = CanonicalIndexID(id), CanonicalIndexPeriod(period)
	if !p.SupportsIndexSeries(id, period) {
		return foundation.MarketIndexSeries{}, fmt.Errorf("%w: id=%s period=%s", ErrUnsupportedIndexSeries, id, period)
	}
	if err := ctx.Err(); err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	if p.indexProvider == nil {
		return foundation.MarketIndexSeries{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: p.config.IndexSourceID, Capability: "index-kline/" + period}
	}
	capability := "index-kline/" + period
	attemptCtx, cancel := indexRequestContext(ctx)
	series, err := p.indexProvider.MarketIndexSeries(attemptCtx, id, period, limit)
	cancel()
	if err == nil && !validIndexSeries(series, id) {
		err = &contracts.Error{Kind: contracts.InvalidResponse, SourceID: p.config.IndexSourceID, Capability: capability}
	}
	series.Meta.Capability = capability
	if series.Meta.Source == "" && p.config.IndexSourceID != "" {
		series.Meta.Source = p.config.IndexSourceID + ":index-kline"
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
		series.Meta.Observations = []foundation.SourceObservation{sourceAttempt(series.Meta, p.config.IndexSourceID, capability, err)}
	}
	if parentErr := ctx.Err(); parentErr != nil {
		return series, parentErr
	}
	if err != nil {
		return series, fmt.Errorf("%s index history unavailable: %w", sourceLabel(p.config.IndexSourceID), err)
	}
	series.Index.Meta.Capability = capability
	for i := range series.Lines {
		series.Lines[i].Meta.Capability = capability
	}
	return series, nil
}

// A replacement adapter may declare broader history support. The default
// catalog remains the compatibility boundary for adapters without this method.
func (p *Market) SupportsIndexSeries(id, period string) bool {
	id, period = CanonicalIndexID(id), CanonicalIndexPeriod(period)
	if provider, ok := p.indexProvider.(interface{ SupportsIndexSeries(string, string) bool }); ok {
		return provider.SupportsIndexSeries(id, period)
	}
	return SupportsIndexSeries(id, period)
}
