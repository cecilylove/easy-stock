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

// Disclosure chains replace complete query results, not individual fields or
// content bodies. Independent acquisition scopes retain explicit limitations.
func (p *Market) loadAnnouncements(ctx context.Context, query, symbol, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return disclosureChain(ctx, "announcement", p.config.AnnouncementsSourceID, p.config.AnnouncementsFallbackSourceID, p.config.Announcements != nil, p.config.AnnouncementsFallback != nil,
		func(c context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
			return p.config.Announcements.MarketAnnouncements(c, query, symbol, category, limit)
		},
		func(c context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
			return p.config.AnnouncementsFallback.MarketAnnouncements(c, query, symbol, category, limit)
		})
}
func (p *Market) loadReports(ctx context.Context, kind, query, symbol, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return disclosureChain(ctx, "report", p.config.ReportsSourceID, p.config.ReportsFallbackSourceID, p.config.Reports != nil, p.config.ReportsFallback != nil,
		func(c context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
			return p.config.Reports.MarketReports(c, kind, query, symbol, industry, limit)
		},
		func(c context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
			return p.config.ReportsFallback.MarketReports(c, kind, query, symbol, industry, limit)
		})
}
func disclosureChain(ctx context.Context, capability, primaryID, fallbackID string, hasPrimary, hasFallback bool, primary, fallback func(context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	ctx, cancel := runtime.Budget(ctx, 18*time.Second)
	defer cancel()
	loaders := []func(context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error){primary, fallback}
	enabled := []bool{hasPrimary, hasFallback}
	ids := []string{primaryID, fallbackID}
	var attempts []foundation.SourceObservation
	var failures []error
	var partialItems []foundation.MarketResearchItem
	var partialMeta foundation.SourceMeta
	partialValid := false
	for i, loader := range loaders {
		if !enabled[i] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, foundation.SourceMeta{Observations: attempts}, err
		}
		child, stop := runtime.Budget(ctx, 12*time.Second)
		if i == 0 && hasFallback {
			stop()
			child, stop = runtime.PrimaryBudget(ctx, 12*time.Second, 6*time.Second)
		}
		items, meta, err := loader(child)
		err = marketContextResult(child, err)
		stop()
		if err == nil {
			err = validDisclosureList(items, capability)
		}
		meta.Capability = capability
		// A later page may fail after verified rows were collected. Preserve only
		// explicit bounded, source-masked rows, never unknown transport payloads.
		if err != nil && meta.QueryCoverage == "bounded" && len(items) > 0 && validDisclosureList(items, capability) == nil && allDisclosureRowsKnown(items) && !errors.Is(err, context.Canceled) && contracts.Kind(err) != contracts.Unsupported {
			if i == 0 || !partialValid {
				partialItems, partialMeta = cloneResearchItems(items), foundation.CloneSourceMeta(meta)
				partialValid = true
			}
		}
		// Adapters distinguish valid partial bodies from a partial query window.
		queryPartial := meta.QueryCoverage == "bounded" || meta.QueryCoverage == "unsupported"
		if err == nil && queryPartial {
			if i == 0 || partialItems == nil {
				partialItems, partialMeta = cloneResearchItems(items), foundation.CloneSourceMeta(meta)
				partialValid = true
			}
			err = &contracts.Error{Kind: contracts.NoData, SourceID: ids[i], Capability: capability, Cause: fmt.Errorf("query coverage is incomplete")}
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) && contracts.Kind(err) != contracts.Unsupported && (meta.ExecutionState == "" || meta.ExecutionState == "fetched") {
			attempts = append(attempts, sourceAttempt(meta, ids[i], capability, err))
		}
		if ctx.Err() != nil {
			return nil, foundation.SourceMeta{Observations: attempts}, ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return nil, foundation.SourceMeta{Observations: attempts}, err
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if meta.Source == "" {
			meta.Source = ids[i]
		}
		meta.Observations = attempts
		if i > 0 {
			meta.FallbackReason = joinFallbackReason("主内容源不支持该查询、覆盖不足或取数失败，已整份回退"+sourceLabel(ids[i])+"；未拼接不同来源正文/列表", meta.FallbackReason)
			for j := range items {
				items[j].Meta.FallbackReason = joinFallbackReason(meta.FallbackReason, items[j].Meta.FallbackReason)
			}
		}
		return items, meta, nil
	}
	if partialValid {
		partialMeta.Observations = attempts
		partialMeta.Partial = true
		reason := "未恢复查询覆盖，仅保留有界有效结果，不能据此排除遗漏"
		if hasFallback {
			reason = "备用" + reason
		} else {
			reason = "备用未配置，" + reason
		}
		partialMeta.FallbackReason = joinFallbackReason(partialMeta.FallbackReason, reason)
		return partialItems, partialMeta, nil
	}
	if len(failures) == 0 {
		return nil, foundation.SourceMeta{}, unsupportedMarket(capability)
	}
	return nil, foundation.SourceMeta{Observations: attempts}, fmt.Errorf("disclosure sources unavailable: %w", errors.Join(failures...))
}
func validDisclosureList(items []foundation.MarketResearchItem, capability string) error {
	// A validated empty result is successful, not proof that the company has no risk.
	for _, item := range items {
		if !item.Meta.FieldsKnown {
			continue
		}
		if strings.TrimSpace(item.Title) == "" || item.ID == "" || item.PublishedAt.IsZero() || item.URL == "" {
			return &contracts.Error{Kind: contracts.InvalidResponse, Capability: capability, Cause: fmt.Errorf("missing required disclosure identity/date/link")}
		}
	}
	return nil
}
func allDisclosureRowsKnown(items []foundation.MarketResearchItem) bool {
	for _, item := range items {
		if !item.Meta.FieldsKnown || item.Meta.Source == "" {
			return false
		}
	}
	return true
}
func cloneResearchItems(items []foundation.MarketResearchItem) []foundation.MarketResearchItem {
	value := append([]foundation.MarketResearchItem(nil), items...)
	for i := range value {
		value[i].Meta = foundation.CloneSourceMeta(value[i].Meta)
	}
	return value
}
