package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
)

func integrityEvent() foundation.LimitUpEvent {
	date := time.Date(2026, 8, 7, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	return foundation.LimitUpEvent{Symbol: "600001.SH", Date: date, Meta: foundation.SourceMeta{Source: "primary", TradeDate: "2026-08-07", FetchedAt: date.Add(time.Hour), FieldsKnown: true}}
}
func TestLimitUpPresencePreservesZeroAndRecordsHydration(t *testing.T) {
	primary := integrityEvent()
	primary.Meta.AvailableFields = []string{"amount", "open_count", "change_percent"}
	fallback := integrityEvent()
	fallback.Meta.Source, fallback.Meta.FetchedAt = "fallback", fallback.Date.Add(2*time.Hour)
	fallback.Meta.AvailableFields = []string{"amount", "open_count", "change_percent", "price", "turnover_rate"}
	fallback.Amount, fallback.OpenCount, fallback.ChangePercent, fallback.Price = 99, 4, 10, 12
	fallback.TurnoverRate = 0
	merged := fillLimitUpEvent(primary, fallback)
	if merged.Amount != 0 || merged.OpenCount != 0 || merged.ChangePercent != 0 {
		t.Fatalf("reported zero overwritten: %+v", merged)
	}
	if merged.Price != 12 || !foundation.LimitUpFieldAvailable(merged, "turnover_rate") {
		t.Fatalf("missing field not filled (including zero): %+v", merged)
	}
	if merged.Meta.FieldSources["price"] != "fallback" || merged.Meta.FieldSources["turnover_rate"] != "fallback" || !merged.Meta.FieldFetchedAt["price"].Equal(fallback.Meta.FetchedAt) {
		t.Fatalf("missing field provenance: %+v", merged.Meta)
	}
	if merged.Meta.Source != primary.Meta.Source || !merged.Meta.FetchedAt.Equal(primary.Meta.FetchedAt) {
		t.Fatal("original event source replaced")
	}
	if len(primary.Meta.FieldSources) != 0 || len(primary.Meta.AvailableFields) != 3 {
		t.Fatal("hydration mutated input")
	}
	fallback.Meta.TradeDate = "2026-08-06"
	if fillLimitUpEvent(primary, fallback).Price != 0 {
		t.Fatal("mismatched trade date hydrated")
	}
	fallback.Meta.TradeDate = primary.Meta.TradeDate
	fallback.Price = math.Inf(1)
	if fillLimitUpEvent(primary, fallback).Price != 0 {
		t.Fatal("infinite fallback accepted")
	}
}
func TestLimitUpCloneIsolatesMetadata(t *testing.T) {
	event := integrityEvent()
	event.Concepts = []string{"theme"}
	event.Meta.AvailableFields = []string{"price"}
	event.Meta.MissingIDs = []string{"2026-08-06"}
	event.Meta.FieldSources = map[string]string{"price": "fallback"}
	event.Meta.FieldFetchedAt = map[string]time.Time{"price": event.Date}
	event.Meta.MemberSet = &foundation.MemberSetMeta{Complete: true}
	event.Meta.Observations = []foundation.SourceObservation{{Meta: foundation.SourceMeta{AvailableFields: []string{"amount"}}}}
	cloned := cloneLimitUpEvent(event)
	cloned.Concepts[0], cloned.Meta.AvailableFields[0], cloned.Meta.MissingIDs[0] = "bad", "bad", "bad"
	cloned.Meta.FieldSources["price"], cloned.Meta.FieldFetchedAt["price"] = "bad", time.Time{}
	cloned.Meta.MemberSet.Complete = false
	cloned.Meta.Observations[0].Meta.AvailableFields[0] = "bad"
	if event.Concepts[0] != "theme" || event.Meta.AvailableFields[0] != "price" || event.Meta.MissingIDs[0] != "2026-08-06" || event.Meta.FieldSources["price"] != "fallback" || event.Meta.FieldFetchedAt["price"].IsZero() || !event.Meta.MemberSet.Complete || event.Meta.Observations[0].Meta.AvailableFields[0] != "amount" {
		t.Fatal("clone retained shared references")
	}
}

type integrityPrimary struct {
	pools []foundation.LimitUpPoolSnapshot
	err   error
	calls int
}

func (p *integrityPrimary) LimitUpPools(context.Context, int) ([]foundation.LimitUpPoolSnapshot, foundation.ThemeFetchMeta, error) {
	p.calls++
	return p.pools, foundation.ThemeFetchMeta{}, p.err
}
func (p *integrityPrimary) EarlyLimitUpPools(context.Context, int) ([]foundation.LimitUpPoolSnapshot, error) {
	return p.pools, p.err
}
func (p *integrityPrimary) CachedLimitUpPools(context.Context, int) ([]foundation.LimitUpPoolSnapshot, error) {
	return p.pools, p.err
}
func (p *integrityPrimary) Snapshots(context.Context, int) ([]foundation.ThemeSnapshot, foundation.ThemeFetchMeta, error) {
	return nil, foundation.ThemeFetchMeta{}, nil
}
func (p *integrityPrimary) LimitUpPoolStale(time.Time) bool { return false }

type integrityHistory struct {
	events                []foundation.LimitUpEvent
	err                   error
	ordinary, progressive int
}

func (p *integrityHistory) RecentLimitUps(context.Context, int) ([]foundation.LimitUpEvent, error) {
	p.ordinary++
	return p.events, p.err
}
func (p *integrityHistory) ProgressiveRecentLimitUps(_ context.Context, _ int, publish func([]foundation.LimitUpEvent)) ([]foundation.LimitUpEvent, error) {
	p.progressive++
	if publish != nil {
		publish(p.events)
	}
	return p.events, p.err
}

func TestLimitUpCoverageOnlyClearsActuallyRecoveredDays(t *testing.T) {
	event := integrityEvent()
	failure := errors.New("failed history day")
	partial := &foundation.LimitUpCoverageError{RequestedDates: []string{"2026-08-06", "2026-08-07"}, CoveredDates: []string{"2026-08-06"}, MissingDates: []string{"2026-08-07"}, Cause: failure}
	for _, tc := range []struct {
		name    string
		pool    foundation.LimitUpPoolSnapshot
		wantErr bool
	}{
		{"exact day", foundation.LimitUpPoolSnapshot{TradeDate: "2026-08-07", Events: []foundation.LimitUpEvent{event}}, false},
		{"valid empty", foundation.LimitUpPoolSnapshot{TradeDate: "2026-08-07"}, false},
		{"other day", foundation.LimitUpPoolSnapshot{TradeDate: "2026-08-06"}, true},
		{"event date mismatch", foundation.LimitUpPoolSnapshot{TradeDate: "2026-08-06", Events: []foundation.LimitUpEvent{event}}, true},
		{"partial pool", foundation.LimitUpPoolSnapshot{TradeDate: "2026-08-07", Meta: foundation.SourceMeta{Partial: true}, Events: []foundation.LimitUpEvent{event}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary := &integrityPrimary{pools: []foundation.LimitUpPoolSnapshot{tc.pool}}
			history := &integrityHistory{events: []foundation.LimitUpEvent{event}, err: partial}
			history.events[0].Meta.Partial, history.events[0].Meta.MissingIDs = true, []string{"2026-08-07"}
			provider := NewLimitUpProvider(primary, history)
			events, err := provider.RecentLimitUps(context.Background(), 2)
			if (err != nil) != tc.wantErr {
				t.Fatalf("events=%+v err=%v", events, err)
			}
			if tc.wantErr {
				var typed *foundation.LimitUpCoverageError
				if !errors.As(err, &typed) || !errors.Is(err, failure) || len(events) == 0 || !events[0].Meta.Partial {
					t.Fatalf("lost partial coverage: %+v %v", events, err)
				}
			} else if events[0].Meta.Partial {
				t.Fatal("recovered result still partial")
			}
		})
	}
	history := &integrityHistory{err: failure}
	provider := NewLimitUpProvider(&integrityPrimary{pools: []foundation.LimitUpPoolSnapshot{{TradeDate: "2026-08-07", Events: []foundation.LimitUpEvent{event}}}}, history)
	if events, err := provider.RecentLimitUps(context.Background(), 2); !errors.Is(err, failure) || len(events) == 0 || !events[0].Meta.Partial {
		t.Fatalf("generic error swallowed: %+v %v", events, err)
	}
}
func TestLimitUpRetainedOnlyDoesNotClaimCompleteHistory(t *testing.T) {
	provider := NewLimitUpProvider(&integrityPrimary{pools: []foundation.LimitUpPoolSnapshot{{TradeDate: "2026-08-07", Events: []foundation.LimitUpEvent{integrityEvent()}}}}, nil)
	events, err := provider.RecentLimitUps(context.Background(), 40)
	var typed *foundation.LimitUpCoverageError
	if !errors.As(err, &typed) || len(typed.MissingDates) == 0 || len(events) == 0 || !events[0].Meta.Partial {
		t.Fatalf("retained snapshot claimed complete history: %+v %v", events, err)
	}
}

func TestLimitUpCoverageReducesOnlyCoveredSubset(t *testing.T) {
	cause := errors.New("two missing days")
	err := resolveLimitUpCoverage(nil, &foundation.LimitUpCoverageError{RequestedDates: []string{"2026-08-06", "2026-08-07"}, MissingDates: []string{"2026-08-06", "2026-08-07"}, Cause: cause}, map[string]bool{"2026-08-07": true}, true)
	var typed *foundation.LimitUpCoverageError
	if !errors.As(err, &typed) || len(typed.MissingDates) != 1 || typed.MissingDates[0] != "2026-08-06" || len(typed.CoveredDates) != 1 || !errors.Is(err, cause) {
		t.Fatalf("incorrect residual coverage: %+v %v", typed, err)
	}
}

func TestLimitUpCanceledStopsBeforeSources(t *testing.T) {
	primary, history := &integrityPrimary{}, &integrityHistory{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewLimitUpProvider(primary, history).RecentLimitUps(ctx, 2)
	if !errors.Is(err, context.Canceled) || primary.calls != 0 || history.ordinary != 0 {
		t.Fatalf("did not stop: %v", err)
	}
}
func TestAccessForwardsProgressiveHistoryAndIsolatesPublish(t *testing.T) {
	event := integrityEvent()
	event.Meta.AvailableFields = []string{"price"}
	failure := &foundation.LimitUpCoverageError{MissingDates: []string{"2026-08-06"}, Cause: errors.New("partial")}
	history := &integrityHistory{events: []foundation.LimitUpEvent{event}, err: failure}
	access := NewAccess("history", registry.Capabilities{LimitUp: history})
	var provider contracts.ProgressiveRecentLimitUpProvider = access
	events, err := provider.ProgressiveRecentLimitUps(context.Background(), 2, func(items []foundation.LimitUpEvent) { items[0].Meta.AvailableFields[0] = "bad" })
	if history.progressive != 1 || history.ordinary != 0 || err != failure || events[0].Meta.AvailableFields[0] != "price" {
		t.Fatalf("lost progressive behavior: %+v %v", events, err)
	}
	disabled := NewAccess("disabled", registry.Capabilities{})
	_, err = disabled.ProgressiveRecentLimitUps(context.Background(), 2, nil)
	var typed *contracts.Error
	if !errors.As(err, &typed) || typed.Kind != contracts.Unsupported {
		t.Fatalf("disabled capability not unsupported: %v", err)
	}
}
func TestProgressiveLimitUpKeepsCoverageErrorThroughLaterStages(t *testing.T) {
	event := integrityEvent()
	history := &integrityHistory{events: []foundation.LimitUpEvent{event}, err: &foundation.LimitUpCoverageError{MissingDates: []string{"2026-08-06"}, Cause: errors.New("missing")}}
	primary := &integrityPrimary{pools: []foundation.LimitUpPoolSnapshot{{TradeDate: "2026-08-07", Events: []foundation.LimitUpEvent{event}}}}
	var last []foundation.LimitUpEvent
	var lastErr error
	NewLimitUpProvider(primary, history).ProgressiveLimitUps(context.Background(), 2, func(items []foundation.LimitUpEvent, _ string, err error) {
		last, lastErr = items, err
		if len(items) > 0 {
			items[0].Name = "mutated"
		}
	})
	var typed *foundation.LimitUpCoverageError
	if !errors.As(lastErr, &typed) || len(last) == 0 || !last[0].Meta.Partial || history.events[0].Name == "mutated" || primary.pools[0].Events[0].Name == "mutated" {
		t.Fatalf("partial lost/shared: %+v %v", last, lastErr)
	}
}
