package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"errors"
	"testing"
	"time"
)

type fakeLabelBase struct {
	contracts.MarketOverviewProvider
	detail foundation.MarketBillboardDetail
}

func (s *fakeLabelBase) MarketBillboardDetail(context.Context, string, string, string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	return s.detail, s.detail.Meta, nil
}
func (s *fakeLabelBase) SupportsIndexSeries(id, period string) bool {
	return id == "custom" && period == "day"
}

type fakeSeatLabels struct {
	err   error
	state string
	calls int
}

func (s *fakeSeatLabels) Fetch(context.Context, string, string) (map[string]string, foundation.SourceMeta, error) {
	s.calls++
	return map[string]string{"机构专用": "机构"}, foundation.SourceMeta{Source: "ths:billboard-labels", FetchedAt: time.Now(), ExecutionState: s.state}, s.err
}
func TestExplicitBillboardLabelsFailOpenAndForwardSupport(t *testing.T) {
	base := &fakeLabelBase{detail: foundation.MarketBillboardDetail{BuySeats: []foundation.MarketBillboardSeat{{Name: "机构专用", BuyAmount: 10}}, Meta: foundation.SourceMeta{Source: "raw:seats"}}}
	labels := &fakeSeatLabels{state: "fetched"}
	decorated := WithBillboardLabels(base, labels, "ths")
	support := decorated.(interface{ SupportsIndexSeries(string, string) bool })
	if !support.SupportsIndexSeries("custom", "day") || support.SupportsIndexSeries("sse", "day") {
		t.Fatal("decorator lost underlying capability")
	}
	detail, meta, err := decorated.MarketBillboardDetail(context.Background(), "600001.SH", "2026-09-30", "reason")
	if err != nil || detail.BuySeats[0].SourceLabel != "机构" || len(meta.Observations) != 1 || base.detail.BuySeats[0].SourceLabel != "" {
		t.Fatalf("%+v %+v %v", detail, meta, err)
	}
	labels.err = errors.New("label failure")
	detail, meta, err = decorated.MarketBillboardDetail(context.Background(), "600001.SH", "2026-09-30", "reason")
	if err != nil || detail.BuySeats[0].BuyAmount != 10 || meta.FallbackReason == "" {
		t.Fatalf("labels broke raw facts %+v %v", detail, err)
	}
	labels.err = context.Canceled
	_, _, err = decorated.MarketBillboardDetail(context.Background(), "600001.SH", "2026-09-30", "reason")
	if err != nil {
		t.Fatal("an optional supplier cancellation destroyed active caller's raw detail")
	}
	labels.err = nil
	labels.state = "cache"
	_, meta, _ = decorated.MarketBillboardDetail(context.Background(), "600001.SH", "2026-09-30", "reason")
	if len(meta.Observations) != 0 {
		t.Fatal("cache became fresh attempt")
	}
	if WithBillboardLabels(base, nil, "ths") != base {
		t.Fatal("disabled route changed facade")
	}
}
