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

// Company keeps business and fundamentals independently routed. Partial financial
// snapshots are never hydrated from another provider/report period.
type CompanyConfig struct {
	Business, BusinessFallback                                             contracts.BusinessProvider
	Fundamentals, FundamentalsFallback                                     contracts.FundamentalsProvider
	BusinessID, BusinessFallbackID, FundamentalsID, FundamentalsFallbackID string
	Observe                                                                func(foundation.SourceObservation)
	Now                                                                    func() time.Time
}
type Company struct{ config CompanyConfig }

func NewCompany(config CompanyConfig) *Company {
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Company{config}
}

// SetObserver is bound once by the composition root, before requests begin.
func (s *Company) SetObserver(observe func(foundation.SourceObservation)) { s.config.Observe = observe }
func (s *Company) record(ctx context.Context, id, capability string, at time.Time, meta foundation.SourceMeta, err error) {
	if s.config.Observe == nil || errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) || contracts.Kind(err) == contracts.Unsupported {
		return
	}
	if meta.ExecutionState != "" && meta.ExecutionState != "fetched" {
		return
	}
	s.config.Observe(foundation.SourceObservation{SourceID: id, Capability: capability, AttemptAt: at, Meta: meta, Failed: err != nil})
}
func companyUnsupported(capability string) error {
	return &contracts.Error{Kind: contracts.Unsupported, Capability: capability}
}
func validBusiness(value foundation.StockBusinessProfile, symbol string) error {
	if value.Symbol != "" && value.Symbol != symbol {
		return &contracts.Error{Kind: contracts.InvalidResponse, Capability: "business", Cause: fmt.Errorf("company identity mismatch")}
	}
	if strings.TrimSpace(value.MainBusiness) == "" || strings.TrimSpace(value.Description) == "" {
		return &contracts.Error{Kind: contracts.NoData, Capability: "business", Cause: fmt.Errorf("main business or profile is missing")}
	}
	return nil
}
func (s *Company) StockBusinessProfile(ctx context.Context, symbol string) (foundation.StockBusinessProfile, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return foundation.StockBusinessProfile{}, err
	}
	ctx, cancel := runtime.Budget(ctx, 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return foundation.StockBusinessProfile{}, err
	}
	providers := []contracts.BusinessProvider{s.config.Business, s.config.BusinessFallback}
	ids := []string{s.config.BusinessID, s.config.BusinessFallbackID}
	var failures []error
	for i, provider := range providers {
		if provider == nil {
			continue
		}
		if ctx.Err() != nil {
			return foundation.StockBusinessProfile{}, ctx.Err()
		}
		child, stop := runtime.Budget(ctx, 6*time.Second)
		if i == 0 && providers[1] != nil {
			stop()
			child, stop = runtime.PrimaryBudget(ctx, 6*time.Second, 3*time.Second)
		}
		start := s.config.Now()
		value, loadErr := provider.StockBusinessProfile(child, normalized.Canonical)
		loadErr = marketContextResult(child, loadErr)
		stop()
		if loadErr == nil {
			loadErr = validBusiness(value, normalized.Canonical)
		}
		s.record(ctx, ids[i], "business", start, value.Meta, loadErr)
		if errors.Is(loadErr, context.Canceled) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return foundation.StockBusinessProfile{}, ctx.Err()
			}
			return foundation.StockBusinessProfile{}, loadErr
		}
		if loadErr != nil {
			failures = append(failures, loadErr)
			continue
		}
		value.Meta.Capability = "business"
		if i > 0 {
			value.Meta.FallbackReason = joinFallbackReason("主公司资料源不可用或覆盖不足，已整份回退"+sourceLabel(ids[i])+"公司资料", value.Meta.FallbackReason)
		}
		return value, nil
	}
	if len(failures) == 0 {
		return foundation.StockBusinessProfile{}, companyUnsupported("business")
	}
	return foundation.StockBusinessProfile{}, fmt.Errorf("company profile unavailable: %w", errors.Join(failures...))
}

var requiredFundamentalFields = []string{"revenue", "revenue_yoy", "net_profit", "net_profit_yoy", "deducted_net_profit", "deducted_net_profit_yoy", "eps", "roe", "gross_margin", "debt_ratio", "operating_cash_flow_per_share"}

func fundamentalMissing(value foundation.StockFundamentals) []string {
	var missing []string
	for _, field := range requiredFundamentalFields {
		if value.FieldAvailable(field) {
			continue
		}
		exempt := false
		for _, na := range value.NotApplicableFields {
			if na == field && field == "gross_margin" {
				exempt = true
			}
		}
		if !exempt {
			missing = append(missing, field)
		}
	}
	return missing
}
func validateFundamentalIdentity(value foundation.StockFundamentals, symbol string, now time.Time) error {
	if value.Symbol != "" && value.Symbol != symbol {
		return &contracts.Error{Kind: contracts.InvalidResponse, Capability: "fundamentals", Cause: fmt.Errorf("financial identity mismatch")}
	}
	report, err := time.ParseInLocation("2006-01-02", strings.Split(value.ReportDate, " ")[0], time.FixedZone("Asia/Shanghai", 8*3600))
	if err != nil || report.After(now) || now.Sub(report) > 550*24*time.Hour {
		return &contracts.Error{Kind: contracts.NoData, Capability: "fundamentals", Cause: fmt.Errorf("invalid or stale financial report period")}
	}
	if !value.PublishedAt.IsZero() && (value.PublishedAt.After(now) || value.PublishedAt.Before(report)) {
		return &contracts.Error{Kind: contracts.InvalidResponse, Capability: "fundamentals", Cause: fmt.Errorf("invalid disclosure date")}
	}
	if !value.FieldAvailable("revenue") && !value.FieldAvailable("net_profit") {
		return &contracts.Error{Kind: contracts.NoData, Capability: "fundamentals", Cause: fmt.Errorf("no core financial values")}
	}
	return nil
}
func (s *Company) StockFundamentals(ctx context.Context, symbol string) (foundation.StockFundamentals, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return foundation.StockFundamentals{}, err
	}
	ctx, cancel := runtime.Budget(ctx, 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return foundation.StockFundamentals{}, err
	}
	providers := []contracts.FundamentalsProvider{s.config.Fundamentals, s.config.FundamentalsFallback}
	ids := []string{s.config.FundamentalsID, s.config.FundamentalsFallbackID}
	var failures []error
	var primaryPartial, fallbackPartial *foundation.StockFundamentals
	for i, provider := range providers {
		if provider == nil {
			continue
		}
		if ctx.Err() != nil {
			return foundation.StockFundamentals{}, ctx.Err()
		}
		child, stop := runtime.Budget(ctx, 6*time.Second)
		if i == 0 && providers[1] != nil {
			stop()
			child, stop = runtime.PrimaryBudget(ctx, 6*time.Second, 3*time.Second)
		}
		start := s.config.Now()
		value, loadErr := provider.StockFundamentals(child, normalized.Canonical)
		loadErr = marketContextResult(child, loadErr)
		stop()
		if loadErr == nil {
			loadErr = validateFundamentalIdentity(value, normalized.Canonical, s.config.Now())
		}
		candidateValid := loadErr == nil
		missing := fundamentalMissing(value)
		value.Meta.Capability = "fundamentals"
		if loadErr == nil && len(missing) > 0 {
			value.Meta.Partial = true
			value.Meta.MissingIDs = append([]string(nil), missing...)
			value.Meta.FallbackReason = joinFallbackReason("财务指标部分覆盖："+strings.Join(missing, ","), value.Meta.FallbackReason)
			copy := value
			copy.Meta = foundation.CloneSourceMeta(value.Meta)
			if i == 0 {
				primaryPartial = &copy
			} else {
				fallbackPartial = &copy
			}
			loadErr = &contracts.Error{Kind: contracts.NoData, Capability: "fundamentals", Cause: fmt.Errorf("incomplete financial fields: %s", strings.Join(missing, ","))}
		}
		s.record(ctx, ids[i], "fundamentals", start, value.Meta, loadErr)
		if errors.Is(loadErr, context.Canceled) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return foundation.StockFundamentals{}, ctx.Err()
			}
			return foundation.StockFundamentals{}, loadErr
		}
		if loadErr != nil {
			failures = append(failures, loadErr)
			// Keep the primary unless a fallback is equally fresh and more complete.
			if i > 0 && candidateValid && primaryPartial != nil && len(missing) > 0 && validateFundamentalIdentity(value, normalized.Canonical, s.config.Now()) == nil && strings.Split(value.ReportDate, " ")[0] >= strings.Split(primaryPartial.ReportDate, " ")[0] && len(missing) < len(fundamentalMissing(*primaryPartial)) {
				value.Meta.FallbackReason = joinFallbackReason("主财务源覆盖不足，已整份回退"+sourceLabel(ids[i])+"部分财务；不跨源补字段", value.Meta.FallbackReason)
				return value, nil
			}
			continue
		}
		if i > 0 && primaryPartial != nil && strings.Split(value.ReportDate, " ")[0] < strings.Split(primaryPartial.ReportDate, " ")[0] {
			failures = append(failures, fmt.Errorf("fallback report is older than primary"))
			continue
		}
		if i > 0 {
			value.Meta.FallbackReason = joinFallbackReason("主财务源不可用或覆盖不足，已整份回退"+sourceLabel(ids[i])+"财务；不跨源补字段", value.Meta.FallbackReason)
		}
		return value, nil
	}
	if primaryPartial == nil && fallbackPartial != nil {
		value := *fallbackPartial
		value.Meta.FallbackReason = joinFallbackReason("主财务源不可用，备用仅部分覆盖；未知指标不评分", value.Meta.FallbackReason)
		return value, nil
	}
	if primaryPartial != nil {
		value := *primaryPartial
		value.Meta.FallbackReason = joinFallbackReason(value.Meta.FallbackReason, "备用未恢复同等或更新报告，仅保留主源有效字段，未知指标不评分")
		return value, nil
	}
	if len(failures) == 0 {
		return foundation.StockFundamentals{}, companyUnsupported("fundamentals")
	}
	return foundation.StockFundamentals{}, fmt.Errorf("financial snapshot unavailable: %w", errors.Join(failures...))
}
