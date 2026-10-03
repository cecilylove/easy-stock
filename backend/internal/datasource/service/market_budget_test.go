package service_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/service"
	"easy-stock/backend/internal/foundation"
)

// Only local HTTP endpoints are used; this fixture implements the two explicit
// market fallback capabilities, not a generic policy/retry framework.
type budgetMarketProvider struct {
	url           string
	lateNil       bool
	calls         int
	deadlines     []time.Time
	remaining     []time.Duration
	configuredErr error
}

func (p *budgetMarketProvider) fetch(ctx context.Context) (foundation.SourceMeta, error) {
	p.calls++
	deadline, _ := ctx.Deadline()
	p.deadlines = append(p.deadlines, deadline)
	p.remaining = append(p.remaining, time.Until(deadline))
	if p.configuredErr != nil {
		return foundation.SourceMeta{}, p.configuredErr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return foundation.SourceMeta{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if p.lateNil {
		return foundation.SourceMeta{ExecutionState: "cache"}, nil
	}
	return foundation.SourceMeta{}, err
}

func (p *budgetMarketProvider) IndustryMomentum(ctx context.Context, _ int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	meta, err := p.fetch(ctx)
	return []foundation.MarketIndustryMomentum{{Code: "native-board"}}, meta, err
}

func (p *budgetMarketProvider) MarketFundFlows(ctx context.Context, _, _ string, _ int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	meta, err := p.fetch(ctx)
	return []foundation.MarketFundFlow{{Symbol: "000001.SZ"}}, meta, err
}

func budgetMarket(primary, backup *budgetMarketProvider) *service.Market {
	config := service.MarketConfig{Industry: primary, IndustrySourceID: "primary", FundFlow: primary, FundFlowSourceID: "primary"}
	if backup != nil {
		config.IndustryFallback, config.IndustryFallbackSourceID = backup, "backup"
		config.FundFlowFallback, config.FundFlowFallbackSourceID = backup, "backup"
	}
	return service.NewMarket(config)
}

func callBudgetMarket(market *service.Market, kind string, ctx context.Context) (foundation.SourceMeta, error) {
	if kind == "industry" {
		_, meta, err := market.IndustryMomentum(ctx, 1)
		return meta, err
	}
	_, meta, err := market.MarketFundFlows(ctx, "stock", "main_net", 1)
	return meta, err
}

func TestMarketBudgetSlowPrimaryStillRunsFallback(t *testing.T) {
	for _, kind := range []string{"industry", "fund-flow"} {
		for _, lateNil := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/deadline-error", true: "/late-nil-cached-result"}[lateNil], func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/primary" {
						<-r.Context().Done()
						return
					}
					w.WriteHeader(http.StatusOK)
				}))
				defer server.Close()
				primary := &budgetMarketProvider{url: server.URL + "/primary", lateNil: lateNil}
				backup := &budgetMarketProvider{url: server.URL + "/backup"}
				ctx, cancel := context.WithTimeout(context.Background(), 360*time.Millisecond)
				defer cancel()
				parentDeadline, _ := ctx.Deadline()
				meta, err := callBudgetMarket(budgetMarket(primary, backup), kind, ctx)
				if err != nil || meta.Source != "backup" || primary.calls != 1 || backup.calls != 1 || ctx.Err() != nil {
					t.Fatalf("slow primary consumed fallback budget: meta=%+v calls=%d/%d err=%v", meta, primary.calls, backup.calls, err)
				}
				if len(meta.Observations) != 2 || !meta.Observations[0].Failed || meta.Observations[1].Failed {
					t.Fatalf("late nil result reported primary success: %+v", meta.Observations)
				}
				if parentDeadline.Sub(primary.deadlines[0]) < 90*time.Millisecond || backup.remaining[0] < 50*time.Millisecond {
					t.Fatalf("no meaningful fallback reserve: primary=%v backup-remaining=%v", primary.deadlines[0], backup.remaining[0])
				}
				for _, deadline := range []time.Time{primary.deadlines[0], backup.deadlines[0]} {
					if deadline.After(parentDeadline) {
						t.Fatal("child budget extended parent")
					}
				}
			})
		}
	}
}

func TestMarketBudgetParentCancellationNeverRunsFallback(t *testing.T) {
	for _, kind := range []string{"industry", "fund-flow"} {
		t.Run(kind, func(t *testing.T) {
			arrived := make(chan struct{})
			var backupRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/primary" {
					close(arrived)
					<-r.Context().Done()
					return
				}
				backupRequests.Add(1)
			}))
			defer server.Close()
			primary, backup := &budgetMarketProvider{url: server.URL + "/primary"}, &budgetMarketProvider{url: server.URL + "/backup"}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := callBudgetMarket(budgetMarket(primary, backup), kind, ctx); result <- err }()
			<-arrived
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) || backup.calls != 0 || backupRequests.Load() != 0 {
					t.Fatalf("cancellation invoked backup: %d/%d %v", backup.calls, backupRequests.Load(), err)
				}
			case <-time.After(time.Second):
				t.Fatal("market ignored cancellation")
			}
		})
	}
}

func TestMarketBudgetBothSlowSourcesStayWithinParentDeadline(t *testing.T) {
	for _, kind := range []string{"industry", "fund-flow"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
			defer server.Close()
			primary := &budgetMarketProvider{url: server.URL + "/primary"}
			backup := &budgetMarketProvider{url: server.URL + "/backup", lateNil: true}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			deadline, _ := ctx.Deadline()
			meta, err := callBudgetMarket(budgetMarket(primary, backup), kind, ctx)
			if !errors.Is(err, context.DeadlineExceeded) || primary.calls != 1 || backup.calls != 1 || time.Now().After(deadline.Add(150*time.Millisecond)) {
				t.Fatalf("market chain escaped parent deadline: calls=%d/%d err=%v", primary.calls, backup.calls, err)
			}
			if len(meta.Observations) != 2 || !meta.Observations[0].Failed || !meta.Observations[1].Failed {
				t.Fatalf("expired fallback nil result reported success: %+v", meta.Observations)
			}
			if primary.deadlines[0].After(deadline) || backup.deadlines[0].After(deadline) {
				t.Fatal("one child exceeded parent budget")
			}
		})
	}
}

func TestMarketBudgetTypedCancellationAndExpiredFallbackOnly(t *testing.T) {
	for _, kind := range []string{"industry", "fund-flow"} {
		primary := &budgetMarketProvider{configuredErr: &contracts.Error{Kind: contracts.Canceled}}
		backup := &budgetMarketProvider{configuredErr: errors.New("should not run")}
		if meta, err := callBudgetMarket(budgetMarket(primary, backup), kind, context.Background()); !errors.Is(err, context.Canceled) || backup.calls != 0 || len(meta.Observations) != 0 {
			t.Fatalf("typed cancellation did not stop chain: %+v %v", meta, err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	for _, kind := range []string{"industry", "fund-flow"} {
		backup := &budgetMarketProvider{url: server.URL, lateNil: true}
		market := service.NewMarket(service.MarketConfig{IndustryFallback: backup, FundFlowFallback: backup})
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		meta, err := callBudgetMarket(market, kind, ctx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) || len(meta.Observations) != 1 || !meta.Observations[0].Failed {
			t.Fatalf("fallback-only late nil result reported success: %+v %v", meta, err)
		}
	}
}
