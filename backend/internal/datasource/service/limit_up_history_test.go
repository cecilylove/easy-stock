package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
)

type emptyCoverageHistory struct {
	value                         foundation.LimitUpHistory
	err                           error
	ordinary, progressive, legacy int
}

func (p *emptyCoverageHistory) RecentLimitUps(context.Context, int) ([]foundation.LimitUpEvent, error) {
	p.legacy++
	return p.value.Events, p.err
}
func (p *emptyCoverageHistory) RecentLimitUpHistory(context.Context, int) (foundation.LimitUpHistory, error) {
	p.ordinary++
	return p.value, p.err
}
func (p *emptyCoverageHistory) ProgressiveRecentLimitUpHistory(_ context.Context, _ int, publish func(foundation.LimitUpHistory)) (foundation.LimitUpHistory, error) {
	p.progressive++
	if publish != nil {
		publish(p.value)
	}
	return p.value, p.err
}

func TestPrimaryOnlyMixedEmptyAndNonemptyHistoryKeepsAllDates(t *testing.T) {
	dates := foundation.LimitUpRequestedDates(time.Now(), 8)
	pools := make([]foundation.LimitUpPoolSnapshot, 0, len(dates))
	for i, date := range dates {
		pool := foundation.LimitUpPoolSnapshot{TradeDate: date}
		if i == 0 {
			parsed, _ := time.Parse("2006-01-02", date)
			pool.Events = []foundation.LimitUpEvent{{Symbol: "600001.SH", Date: parsed}}
		}
		pools = append(pools, pool)
	}
	provider := NewLimitUpProvider(&integrityPrimary{pools: pools}, nil)
	value, err := provider.RecentLimitUpHistory(context.Background(), 8)
	if err != nil || len(value.CoveredDates) != len(dates) || value.CoveredDates[len(value.CoveredDates)-1] != dates[len(dates)-1] {
		t.Fatalf("primary empty dates lost %+v %v", value, err)
	}
	value, err = provider.ProgressiveLimitUpHistory(context.Background(), 8, nil)
	if err != nil || len(value.CoveredDates) != len(dates) || value.CoveredDates[len(value.CoveredDates)-1] != dates[len(dates)-1] {
		t.Fatalf("progressive primary empty dates lost %+v %v", value, err)
	}
}
func TestCoveredDayCanRepairSupplierTimeoutButNotCallerCancellation(t *testing.T) {
	timeout := &contracts.Error{Kind: contracts.TimedOut, Cause: context.DeadlineExceeded}
	err := resolveLimitUpCoverage(nil, &foundation.LimitUpCoverageError{MissingDates: []string{"2026-09-30"}, Cause: timeout}, map[string]bool{"2026-09-30": true}, true)
	if err != nil {
		t.Fatalf("covered provider timeout not repaired %v", err)
	}
	err = resolveLimitUpCoverage(nil, &foundation.LimitUpCoverageError{MissingDates: []string{"2026-09-30"}, Cause: context.Canceled}, map[string]bool{"2026-09-30": true}, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation hidden")
	}
}

func TestAccessEmptyHistoryCoverageNoDuplicateFetch(t *testing.T) {
	p := &emptyCoverageHistory{value: foundation.LimitUpHistory{CoveredDates: []string{"2026-08-06", "2026-08-07"}}}
	a := NewAccess("history", registry.Capabilities{LimitUp: p})
	value, err := a.RecentLimitUpHistory(context.Background(), 2)
	if err != nil || len(value.CoveredDates) != 2 || p.ordinary != 1 || p.legacy != 0 {
		t.Fatalf("ordinary lost empty coverage: %+v %v %+v", value, err, p)
	}
	value, err = a.ProgressiveRecentLimitUpHistory(context.Background(), 2, func(v foundation.LimitUpHistory) { v.CoveredDates[0] = "mutated" })
	if err != nil || value.CoveredDates[0] != "2026-08-06" || p.progressive != 1 || p.ordinary != 1 || p.legacy != 0 {
		t.Fatalf("progressive cloned/refetched: %+v %v %+v", value, err, p)
	}
}
func TestAccessHistoryCanceledDoesNotFetch(t *testing.T) {
	p := &emptyCoverageHistory{}
	a := NewAccess("history", registry.Capabilities{LimitUp: p})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.RecentLimitUpHistory(ctx, 2); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := a.ProgressiveRecentLimitUpHistory(ctx, 2, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if p.ordinary != 0 || p.progressive != 0 || p.legacy != 0 {
		t.Fatal("canceled request fetched")
	}
}

func TestCombinedEmptyHistoryRetainsSuccessfulAndRecoveredDates(t *testing.T) {
	cause := errors.New("missing day")
	p := &emptyCoverageHistory{value: foundation.LimitUpHistory{RequestedDates: []string{"2026-08-06", "2026-08-07"}, CoveredDates: []string{"2026-08-06"}, MissingDates: []string{"2026-08-07"}}, err: &foundation.LimitUpCoverageError{RequestedDates: []string{"2026-08-06", "2026-08-07"}, CoveredDates: []string{"2026-08-06"}, MissingDates: []string{"2026-08-07"}, Cause: cause}}
	primary := &integrityPrimary{pools: []foundation.LimitUpPoolSnapshot{{TradeDate: "2026-08-07"}}}
	provider := NewLimitUpProvider(primary, p)
	value, err := provider.RecentLimitUpHistory(context.Background(), 2)
	if err != nil || len(value.Events) != 0 || len(value.CoveredDates) != 2 || len(value.MissingDates) != 0 {
		t.Fatalf("empty repair lost: %+v %v", value, err)
	}
	var last foundation.LimitUpHistory
	value, err = provider.ProgressiveLimitUpHistory(context.Background(), 2, func(v foundation.LimitUpHistory, _ string, _ error) { last = v })
	if err != nil || len(last.CoveredDates) != 2 || len(value.CoveredDates) != 2 || p.progressive != 1 || p.ordinary != 1 || p.legacy != 0 {
		t.Fatalf("progressive empty repair/refetch: %+v %v %+v", value, err, p)
	}
}
func TestCombinedPartialPoolCannotFillFailedEmptyDate(t *testing.T) {
	p := &emptyCoverageHistory{value: foundation.LimitUpHistory{RequestedDates: []string{"2026-08-06", "2026-08-07"}, CoveredDates: []string{"2026-08-06"}, MissingDates: []string{"2026-08-07"}}, err: &foundation.LimitUpCoverageError{RequestedDates: []string{"2026-08-06", "2026-08-07"}, CoveredDates: []string{"2026-08-06"}, MissingDates: []string{"2026-08-07"}}}
	value, err := NewLimitUpProvider(&integrityPrimary{pools: []foundation.LimitUpPoolSnapshot{{TradeDate: "2026-08-07", Events: []foundation.LimitUpEvent{integrityEvent()}, Meta: foundation.SourceMeta{Partial: true}}}}, p).RecentLimitUpHistory(context.Background(), 2)
	if err == nil || len(value.CoveredDates) != 1 || len(value.MissingDates) != 1 {
		t.Fatalf("partial pool invented complete empty coverage: %+v %v", value, err)
	}
}
