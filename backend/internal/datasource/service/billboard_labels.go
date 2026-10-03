package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"errors"
	"strings"
	"time"
)

// BillboardLabels decorates the business facade with an explicitly registered
// optional supplier; raw billboard adapters never call another supplier.
type BillboardLabels struct {
	contracts.MarketOverviewProvider
	labels   contracts.BillboardLabelProvider
	sourceID string
}

func WithBillboardLabels(market contracts.MarketOverviewProvider, labels contracts.BillboardLabelProvider, sourceID string) contracts.MarketOverviewProvider {
	if labels == nil {
		return market
	}
	return &BillboardLabels{MarketOverviewProvider: market, labels: labels, sourceID: sourceID}
}
func (s *BillboardLabels) MarketBillboardDetail(ctx context.Context, symbol, date, reason string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	detail, meta, err := s.MarketOverviewProvider.MarketBillboardDetail(ctx, symbol, date, reason)
	if err != nil {
		return detail, meta, err
	}
	labelCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	labels, labelMeta, labelErr := s.labels.Fetch(labelCtx, symbol, date)
	if errors.Is(ctx.Err(), context.Canceled) {
		return detail, meta, context.Canceled
	}
	if !errors.Is(labelErr, context.Canceled) && (labelMeta.ExecutionState == "" || labelMeta.ExecutionState == "fetched") {
		meta.Observations = append(meta.Observations, sourceAttempt(labelMeta, s.sourceID, "billboard-labels", labelErr))
	}
	if labelErr != nil {
		meta.FallbackReason = joinFallbackReason(meta.FallbackReason, "平台席位标签未取得；原始买卖明细仍有效")
	} else {
		apply := func(seats []foundation.MarketBillboardSeat) []foundation.MarketBillboardSeat {
			copySeats := append([]foundation.MarketBillboardSeat(nil), seats...)
			for i := range copySeats {
				seat := &copySeats[i]
				label := labels[normalizedBillboardSeat(seat.Name)]
				if strings.TrimSpace(label) == "" {
					continue
				}
				seat.SourceLabel, seat.Source = label, s.sourceID
				seat.LabelConfidence = "high"
				seat.LabelNote = "平台当日公开页分类，不代表监管机构确认资金身份"
				if label == "机构" || strings.Contains(label, "机构专用") {
					seat.Institution = true
				}
			}
			return copySeats
		}
		detail.BuySeats, detail.SellSeats = apply(detail.BuySeats), apply(detail.SellSeats)
	}
	detail.Meta = meta
	return detail, meta, nil
}
func (s *BillboardLabels) SupportsIndexSeries(id, period string) bool {
	if provider, ok := s.MarketOverviewProvider.(interface{ SupportsIndexSeries(string, string) bool }); ok {
		return provider.SupportsIndexSeries(id, period)
	}
	return SupportsIndexSeries(id, period)
}
func normalizedBillboardSeat(value string) string {
	return foundation.NormalizeBillboardSeatName(value)
}
