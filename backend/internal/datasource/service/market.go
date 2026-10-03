package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/runtime"
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

// MarketConfig accepts one small capability per slot, so replacing a margin or
// report supplier does not require implementing an entire market facade.
type MarketConfig struct {
	Index                                                                                                                 contracts.IndexProvider
	Industry                                                                                                              contracts.IndustryProvider
	IndustryFallback                                                                                                      contracts.IndustryProvider
	FundFlow                                                                                                              contracts.FundFlowProvider
	FundFlowFallback                                                                                                      contracts.FundFlowProvider
	Margin                                                                                                                contracts.MarginProvider
	Billboard                                                                                                             contracts.BillboardProvider
	Announcements                                                                                                         contracts.AnnouncementProvider
	Reports                                                                                                               contracts.ReportProvider
	USSector                                                                                                              contracts.USSectorProvider
	USSectorFallback                                                                                                      contracts.USSectorProvider
	IndexSourceID, IndustrySourceID, IndustryFallbackSourceID, FundFlowSourceID, FundFlowFallbackSourceID                 string
	MarginSourceID, BillboardSourceID, AnnouncementsSourceID, ReportsSourceID, USSectorSourceID, USSectorFallbackSourceID string
}
type Market struct {
	config           MarketConfig
	indexProvider    IndexProvider
	industryProvider IndustryMomentumProvider
	fundFlowProvider FundFlowProvider
}

func NewMarket(config MarketConfig) *Market {
	return &Market{config: config, indexProvider: config.Index, industryProvider: config.Industry, fundFlowProvider: config.FundFlow}
}
func NewLegacyMarket(primary Primary, indexProvider IndexProvider, industryProvider IndustryMomentumProvider, fundFlowProvider FundFlowProvider) *Market {
	config := MarketConfig{Index: indexProvider, Industry: industryProvider, IndustryFallback: primary, FundFlow: fundFlowProvider, FundFlowFallback: primary, Margin: primary, Billboard: primary, Announcements: primary, Reports: primary, IndexSourceID: "tencent", IndustrySourceID: "tencent", IndustryFallbackSourceID: "eastmoney", FundFlowSourceID: "sina", FundFlowFallbackSourceID: "eastmoney", MarginSourceID: "eastmoney", BillboardSourceID: "eastmoney", AnnouncementsSourceID: "eastmoney", ReportsSourceID: "eastmoney", USSectorSourceID: "eastmoney", USSectorFallbackSourceID: "tencent"}
	config.USSector, _ = primary.(USSectorMomentumProvider)
	config.USSectorFallback, _ = indexProvider.(USSectorMomentumProvider)
	return NewMarket(config)
}

// The constructor retains its parameter order. indexProvider is the sole
// index source; primary remains Eastmoney for the other services.
// The primary interface intentionally has no index methods.
func (p *Market) MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	return p.indexSnapshots(ctx, scope)
}

func (p *Market) MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error) {
	return p.indexSeries(ctx, id, period, limit)
}

func (p *Market) IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	ctx, cancel := runtime.Budget(ctx, 10*time.Second)
	defer cancel()
	if p.industryProvider == nil {
		if p.config.IndustryFallback == nil {
			return nil, foundation.SourceMeta{}, unsupportedMarket("industry-momentum")
		}
		return runMarketCapability(ctx, p.config.IndustryFallbackSourceID, "industry-momentum", func(ctx context.Context) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
			items, meta, err := p.config.IndustryFallback.IndustryMomentum(ctx, limit)
			if err == nil && len(items) == 0 {
				err = &contracts.Error{Kind: contracts.NoData, SourceID: p.config.IndustryFallbackSourceID, Capability: "industry-momentum"}
			}
			return items, meta, err
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	primaryCtx, stopPrimary := marketPrimaryContext(ctx, p.config.IndustryFallback != nil)
	items, meta, err := p.industryProvider.IndustryMomentum(primaryCtx, limit)
	err = marketContextResult(primaryCtx, err)
	stopPrimary()
	err = marketContextResult(ctx, err)
	if err == nil && len(items) == 0 {
		err = &contracts.Error{Kind: contracts.NoData, SourceID: p.config.IndustrySourceID, Capability: "industry-momentum"}
	}
	capability := "industry-momentum"
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return nil, meta, context.Canceled
	}
	if meta.Source == "" {
		meta.Source = p.config.IndustrySourceID
	}
	observation := sourceAttempt(meta, p.config.IndustrySourceID, capability, err)
	meta.Observations = []foundation.SourceObservation{observation}
	meta.Capability = capability
	if err == nil {
		return items, meta, nil
	}
	if ctx.Err() != nil {
		return nil, meta, ctx.Err()
	}
	if errors.Is(err, context.Canceled) || p.config.IndustryFallback == nil {
		return nil, meta, err
	}
	fallbackCtx, stopFallback := runtime.Budget(ctx, 7*time.Second)
	fallbackItems, fallbackMeta, fallbackErr := p.config.IndustryFallback.IndustryMomentum(fallbackCtx, limit)
	fallbackErr = marketContextResult(fallbackCtx, fallbackErr)
	stopFallback()
	fallbackErr = marketContextResult(ctx, fallbackErr)
	if fallbackErr == nil && len(fallbackItems) == 0 {
		fallbackErr = &contracts.Error{Kind: contracts.NoData, SourceID: p.config.IndustryFallbackSourceID, Capability: capability}
	}
	fallbackMeta.Observations = []foundation.SourceObservation{observation, sourceAttempt(fallbackMeta, p.config.IndustryFallbackSourceID, capability, fallbackErr)}
	if errors.Is(fallbackErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		fallbackMeta.Observations = []foundation.SourceObservation{observation}
		return nil, fallbackMeta, context.Canceled
	}
	if fallbackMeta.Source == "" {
		fallbackMeta.Source = p.config.IndustryFallbackSourceID
	}
	fallbackMeta.Capability = capability
	if fallbackErr != nil {
		return nil, fallbackMeta, fmt.Errorf("%s industry momentum failed: %v; %s fallback failed: %w", sourceLabel(p.config.IndustrySourceID), err, sourceLabel(p.config.IndustryFallbackSourceID), fallbackErr)
	}
	fallbackMeta.FallbackReason = joinFallbackReason(fmt.Sprintf("%s行业强度不可用，已回退%s可用字段", sourceLabel(p.config.IndustrySourceID), sourceLabel(p.config.IndustryFallbackSourceID)), fallbackMeta.FallbackReason)
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

func (p *Market) MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	ctx, cancel := runtime.Budget(ctx, 10*time.Second)
	defer cancel()
	if p.fundFlowProvider == nil {
		if p.config.FundFlowFallback == nil {
			return nil, foundation.SourceMeta{}, unsupportedMarket("fund-flow/" + dimension)
		}
		return runMarketCapability(ctx, p.config.FundFlowFallbackSourceID, "fund-flow/"+dimension, func(ctx context.Context) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
			items, meta, err := p.config.FundFlowFallback.MarketFundFlows(ctx, dimension, sortKey, limit)
			if err == nil && len(items) == 0 {
				err = &contracts.Error{Kind: contracts.NoData, SourceID: p.config.FundFlowFallbackSourceID, Capability: "fund-flow/" + dimension}
			}
			return items, meta, err
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	primaryCtx, stopPrimary := marketPrimaryContext(ctx, p.config.FundFlowFallback != nil)
	items, meta, err := p.fundFlowProvider.MarketFundFlows(primaryCtx, dimension, sortKey, limit)
	err = marketContextResult(primaryCtx, err)
	stopPrimary()
	err = marketContextResult(ctx, err)
	capability := "fund-flow/" + dimension
	if err == nil && len(items) == 0 {
		err = &contracts.Error{Kind: contracts.NoData, SourceID: p.config.FundFlowSourceID, Capability: capability}
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return nil, meta, context.Canceled
	}
	if meta.Source == "" {
		meta.Source = p.config.FundFlowSourceID
	}
	observation := sourceAttempt(meta, p.config.FundFlowSourceID, capability, err)
	meta.Observations = []foundation.SourceObservation{observation}
	meta.Capability = capability
	if err == nil {
		return items, meta, nil
	}
	if ctx.Err() != nil {
		return nil, meta, ctx.Err()
	}
	if errors.Is(err, context.Canceled) || p.config.FundFlowFallback == nil {
		return nil, meta, err
	}
	fallbackCtx, stopFallback := runtime.Budget(ctx, 7*time.Second)
	fallbackItems, fallbackMeta, fallbackErr := p.config.FundFlowFallback.MarketFundFlows(fallbackCtx, dimension, sortKey, limit)
	fallbackErr = marketContextResult(fallbackCtx, fallbackErr)
	stopFallback()
	fallbackErr = marketContextResult(ctx, fallbackErr)
	if fallbackErr == nil && len(fallbackItems) == 0 {
		fallbackErr = &contracts.Error{Kind: contracts.NoData, SourceID: p.config.FundFlowFallbackSourceID, Capability: capability}
	}
	fallbackMeta.Observations = []foundation.SourceObservation{observation, sourceAttempt(fallbackMeta, p.config.FundFlowFallbackSourceID, capability, fallbackErr)}
	if errors.Is(fallbackErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		fallbackMeta.Observations = []foundation.SourceObservation{observation}
		return nil, fallbackMeta, context.Canceled
	}
	if fallbackMeta.Source == "" {
		fallbackMeta.Source = p.config.FundFlowFallbackSourceID
	}
	fallbackMeta.Capability = capability
	if fallbackErr != nil {
		return nil, fallbackMeta, fmt.Errorf("%s %s fund flow failed: %v; %s fallback failed: %w", sourceLabel(p.config.FundFlowSourceID), dimension, err, sourceLabel(p.config.FundFlowFallbackSourceID), fallbackErr)
	}
	fallbackMeta.FallbackReason = joinFallbackReason(fmt.Sprintf("%s资金榜不可用，已回退%s可用字段", sourceLabel(p.config.FundFlowSourceID), sourceLabel(p.config.FundFlowFallbackSourceID)), fallbackMeta.FallbackReason)
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

func (p *Market) USSectorMomentum(ctx context.Context, limit int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error) {
	if provider := p.config.USSector; provider != nil {
		return runMarketCapability(ctx, p.config.USSectorSourceID, "us-sector", func(ctx context.Context) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error) {
			return provider.USSectorMomentum(ctx, limit)
		})
	}
	if provider := p.config.USSectorFallback; provider != nil {
		items, meta, err := runMarketCapability(ctx, p.config.USSectorFallbackSourceID, "us-sector", func(ctx context.Context) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error) {
			return provider.USSectorMomentum(ctx, limit)
		})
		if err != nil {
			return nil, meta, err
		}
		meta.FallbackReason = joinFallbackReason("主行情源不提供美股板块ETF，已切换"+sourceLabel(p.config.USSectorFallbackSourceID)+"行情", meta.FallbackReason)
		for index := range items {
			items[index].Meta.FallbackReason = meta.FallbackReason
		}
		return items, meta, nil
	}
	return nil, foundation.SourceMeta{}, unsupportedMarket("us-sector")
}

func (p *Market) MarketMarginSeries(ctx context.Context, limit int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error) {
	if p.config.Margin == nil {
		return nil, foundation.SourceMeta{}, unsupportedMarket("margin")
	}
	return runMarketCapability(ctx, p.config.MarginSourceID, "margin", func(ctx context.Context) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error) {
		return p.config.Margin.MarketMarginSeries(ctx, limit)
	})
}

func joinFallbackReason(primary string, secondary string) string {
	if secondary == "" {
		return primary
	}
	return primary + "；" + secondary
}

func (p *Market) MarketBillboard(ctx context.Context, tradeDate string, limit int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
	if p.config.Billboard == nil {
		return nil, foundation.SourceMeta{}, unsupportedMarket("billboard")
	}
	return runMarketCapability(ctx, p.config.BillboardSourceID, "billboard", func(ctx context.Context) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
		return p.config.Billboard.MarketBillboard(ctx, tradeDate, limit)
	})
}

func (p *Market) MarketBillboardDetail(ctx context.Context, symbol string, tradeDate string, reason string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	if p.config.Billboard == nil {
		return foundation.MarketBillboardDetail{}, foundation.SourceMeta{}, unsupportedMarket("billboard-detail")
	}
	return runMarketCapability(ctx, p.config.BillboardSourceID, "billboard-detail", func(ctx context.Context) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
		return p.config.Billboard.MarketBillboardDetail(ctx, symbol, tradeDate, reason)
	})
}

func (p *Market) MarketAnnouncements(ctx context.Context, query string, symbol string, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	if p.config.Announcements == nil {
		return nil, foundation.SourceMeta{}, unsupportedMarket("announcement")
	}
	return runMarketCapability(ctx, p.config.AnnouncementsSourceID, "announcement", func(ctx context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
		return p.config.Announcements.MarketAnnouncements(ctx, query, symbol, category, limit)
	})
}

func (p *Market) MarketReports(ctx context.Context, kind string, query string, symbol string, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	if p.config.Reports == nil {
		return nil, foundation.SourceMeta{}, unsupportedMarket("report")
	}
	return runMarketCapability(ctx, p.config.ReportsSourceID, "report", func(ctx context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
		return p.config.Reports.MarketReports(ctx, kind, query, symbol, industry, limit)
	})
}

func unsupportedMarket(capability string) error {
	return &contracts.Error{Kind: contracts.Unsupported, Capability: capability}
}

func sourceLabel(id string) string {
	switch id {
	case "tencent":
		return "腾讯"
	case "eastmoney":
		return "东方财富"
	case "sina":
		return "新浪"
	case "cffex":
		return "中金所"
	}
	if id == "" {
		return "配置来源"
	}
	return id
}

// Only the two explicit industry/fund-flow chains reserve fallback time.
func marketPrimaryContext(ctx context.Context, hasFallback bool) (context.Context, context.CancelFunc) {
	if hasFallback {
		return runtime.PrimaryBudget(ctx, 7*time.Second, 3*time.Second)
	}
	return runtime.Budget(ctx, 10*time.Second)
}

// Inspect before calling the child's CancelFunc: a provider returning nil after
// its deadline must not publish success, including a populated/cached result.
func marketContextResult(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return errors.Join(contextErr, err)
	}
	return err
}

func runMarketCapability[T any](ctx context.Context, id, capability string, load func(context.Context) (T, foundation.SourceMeta, error)) (T, foundation.SourceMeta, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, foundation.SourceMeta{}, err
	}
	value, meta, err := load(ctx)
	err = marketContextResult(ctx, err)
	if err != nil {
		value = zero
	}
	if id == "" {
		id, _, _ = strings.Cut(meta.Source, ":")
	}
	if meta.Source == "" && id != "" {
		meta.Source = id
	}
	meta.Capability = capability
	if !errors.Is(ctx.Err(), context.Canceled) && !errors.Is(err, context.Canceled) && len(meta.Observations) == 0 {
		meta.Observations = []foundation.SourceObservation{sourceAttempt(meta, id, capability, err)}
	}
	return value, meta, err
}
