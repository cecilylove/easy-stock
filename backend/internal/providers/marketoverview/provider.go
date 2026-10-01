package marketoverview

import (
	"context"
	"fmt"

	"easy-stock/backend/internal/foundation"
)

type Primary interface {
	IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error)
	MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error)
	MarketMarginSeries(ctx context.Context, limit int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error)
	MarketBillboard(ctx context.Context, tradeDate string, limit int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error)
	MarketBillboardDetail(ctx context.Context, symbol string, tradeDate string, reason string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error)
	MarketAnnouncements(ctx context.Context, query string, symbol string, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
	MarketReports(ctx context.Context, kind string, query string, symbol string, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
}

type IndustryMomentumProvider interface {
	IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error)
}

type FundFlowProvider interface {
	MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error)
}

type USSectorMomentumProvider interface {
	USSectorMomentum(ctx context.Context, limit int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error)
}

type IndexProvider interface {
	MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)
	MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error)
}

type Provider struct {
	primary          Primary
	indexProvider    IndexProvider
	industryProvider IndustryMomentumProvider
	fundFlowProvider FundFlowProvider
}

func New(primary Primary, indexProvider IndexProvider, industryProvider IndustryMomentumProvider, fundFlowProvider FundFlowProvider) *Provider {
	return &Provider{primary: primary, indexProvider: indexProvider, industryProvider: industryProvider, fundFlowProvider: fundFlowProvider}
}

// The constructor retains its parameter order. indexProvider is the sole
// index source; primary remains Eastmoney for the other services.
// The primary interface intentionally has no index methods.
func (p *Provider) MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	return p.indexSnapshots(ctx, scope)
}

func (p *Provider) MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error) {
	return p.indexSeries(ctx, id, period, limit)
}

func (p *Provider) IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	if p.industryProvider == nil {
		return p.primary.IndustryMomentum(ctx, limit)
	}
	if err := ctx.Err(); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	items, meta, err := p.industryProvider.IndustryMomentum(ctx, limit)
	if err == nil && len(items) == 0 {
		err = fmt.Errorf("tencent returned no industry rows")
	}
	capability := "industry-momentum"
	observation := sourceAttempt(meta, "tencent", capability, err)
	meta.Observations = []foundation.SourceObservation{observation}
	meta.Capability = capability
	if err == nil {
		return items, meta, nil
	}
	if ctx.Err() != nil {
		return nil, meta, ctx.Err()
	}
	fallbackItems, fallbackMeta, fallbackErr := p.primary.IndustryMomentum(ctx, limit)
	if fallbackErr == nil && len(fallbackItems) == 0 {
		fallbackErr = fmt.Errorf("eastmoney returned no industry rows")
	}
	fallbackMeta.Observations = []foundation.SourceObservation{observation, sourceAttempt(fallbackMeta, "eastmoney", capability, fallbackErr)}
	fallbackMeta.Capability = capability
	if fallbackErr != nil {
		return nil, fallbackMeta, fmt.Errorf("tencent industry momentum failed: %v; eastmoney fallback failed: %w", err, fallbackErr)
	}
	fallbackMeta.FallbackReason = joinFallbackReason("腾讯行业强度不可用，已回退东方财富可用字段", fallbackMeta.FallbackReason)
	for index := range fallbackItems {
		if fallbackItems[index].Meta.Source == "" {
			fallbackItems[index].Meta = fallbackMeta
		} else {
			fallbackItems[index].Meta.FallbackReason = fallbackMeta.FallbackReason
			fallbackItems[index].Meta.Capability = capability
		}
	}
	return fallbackItems, fallbackMeta, nil
}

func (p *Provider) MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	if p.fundFlowProvider == nil {
		return p.primary.MarketFundFlows(ctx, dimension, sortKey, limit)
	}
	if err := ctx.Err(); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	items, meta, err := p.fundFlowProvider.MarketFundFlows(ctx, dimension, sortKey, limit)
	capability := "fund-flow/" + dimension
	if err == nil && len(items) == 0 {
		err = fmt.Errorf("sina returned no fund-flow rows")
	}
	observation := sourceAttempt(meta, "sina", capability, err)
	meta.Observations = []foundation.SourceObservation{observation}
	meta.Capability = capability
	if err == nil {
		return items, meta, nil
	}
	if ctx.Err() != nil {
		return nil, meta, ctx.Err()
	}
	fallbackItems, fallbackMeta, fallbackErr := p.primary.MarketFundFlows(ctx, dimension, sortKey, limit)
	if fallbackErr == nil && len(fallbackItems) == 0 {
		fallbackErr = fmt.Errorf("eastmoney returned no fund-flow rows")
	}
	fallbackMeta.Observations = []foundation.SourceObservation{observation, sourceAttempt(fallbackMeta, "eastmoney", capability, fallbackErr)}
	fallbackMeta.Capability = capability
	if fallbackErr != nil {
		return nil, fallbackMeta, fmt.Errorf("sina %s fund flow failed: %v; eastmoney fallback failed: %w", dimension, err, fallbackErr)
	}
	fallbackMeta.FallbackReason = joinFallbackReason("新浪资金榜不可用，已回退东方财富可用字段", fallbackMeta.FallbackReason)
	for index := range fallbackItems {
		if fallbackItems[index].Meta.Source == "" {
			fallbackItems[index].Meta = fallbackMeta
		} else {
			fallbackItems[index].Meta.FallbackReason = fallbackMeta.FallbackReason
			fallbackItems[index].Meta.Capability = capability
		}
	}
	return fallbackItems, fallbackMeta, nil
}

func (p *Provider) USSectorMomentum(ctx context.Context, limit int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error) {
	if provider, ok := p.primary.(USSectorMomentumProvider); ok {
		return provider.USSectorMomentum(ctx, limit)
	}
	if provider, ok := p.indexProvider.(USSectorMomentumProvider); ok {
		items, meta, err := provider.USSectorMomentum(ctx, limit)
		if err != nil {
			return nil, foundation.SourceMeta{}, err
		}
		meta.FallbackReason = joinFallbackReason("主行情源不提供美股板块ETF，已切换腾讯行情", meta.FallbackReason)
		for index := range items {
			items[index].Meta.FallbackReason = meta.FallbackReason
		}
		return items, meta, nil
	}
	return nil, foundation.SourceMeta{}, fmt.Errorf("US sector momentum provider unavailable")
}

func (p *Provider) MarketMarginSeries(ctx context.Context, limit int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error) {
	return p.primary.MarketMarginSeries(ctx, limit)
}

func joinFallbackReason(primary string, secondary string) string {
	if secondary == "" {
		return primary
	}
	return primary + "；" + secondary
}

func (p *Provider) MarketBillboard(ctx context.Context, tradeDate string, limit int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
	return p.primary.MarketBillboard(ctx, tradeDate, limit)
}

func (p *Provider) MarketBillboardDetail(ctx context.Context, symbol string, tradeDate string, reason string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	return p.primary.MarketBillboardDetail(ctx, symbol, tradeDate, reason)
}

func (p *Provider) MarketAnnouncements(ctx context.Context, query string, symbol string, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return p.primary.MarketAnnouncements(ctx, query, symbol, category, limit)
}

func (p *Provider) MarketReports(ctx context.Context, kind string, query string, symbol string, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return p.primary.MarketReports(ctx, kind, query, symbol, industry, limit)
}
