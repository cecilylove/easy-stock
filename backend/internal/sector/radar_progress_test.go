package sector

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type delayedRadarSource struct {
	fakeRadarSource
	release chan struct{}
}

func (s delayedRadarSource) Snapshot(ctx context.Context) (foundation.ThemeSnapshot, foundation.ThemeFetchMeta, error) {
	select {
	case <-s.release:
		return s.fakeRadarSource.Snapshot(ctx)
	case <-ctx.Done():
		return foundation.ThemeSnapshot{}, foundation.ThemeFetchMeta{}, ctx.Err()
	}
}

func TestProgressiveOverviewPublishesIndustryBeforeSlowMembership(t *testing.T) {
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	source := delayedRadarSource{fakeRadarSource: fakeRadarSource{snapshot: foundation.ThemeSnapshot{ID: "snapshot", TradeDate: "2026-09-17", FetchedAt: now, Themes: []foundation.ThemeSnapshotItem{{Code: "1", Name: "通信", Rank: 1, Leaders: []foundation.ThemeLeader{{Symbol: "000001.SZ", Name: "测试", Rank: 1}}}}}}, release: make(chan struct{})}
	provider := NewRadarProvider(source, fakeRadarFallback{}, nil, RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{{Code: "i1", Name: "通信", Score: 80, LeaderSymbol: "000001.SZ", LeaderName: "测试"}}}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan foundation.ThemeProgress, 8)
	go provider.ProgressiveOverviews(ctx, func(value foundation.ThemeProgress) { updates <- value })
	select {
	case first := <-updates:
		if len(first.Data) == 0 || first.Steps["industry"] != "ready" || first.Steps["kaipanla"] != "loading" {
			t.Fatalf("did not publish fast source: %+v", first)
		}
		if len(first.Data[0].LeaderStocks) != 1 || !first.Data[0].Provisional {
			t.Fatalf("missing preview membership: %+v", first.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("fast source blocked behind slow membership")
	}
	close(source.release)
	for {
		select {
		case update := <-updates:
			if !update.Refreshing {
				if update.Steps["kaipanla"] != "ready" {
					t.Fatal(update)
				}
				return
			}
		case <-time.After(time.Second):
			t.Fatal("refresh failed to reach terminal state")
		}
	}
}

type forbiddenRadarFallback struct{}

func (forbiddenRadarFallback) Build(context.Context, string) (foundation.SectorMap, error) {
	panic("leader preview must not load full constituents")
}

type forbiddenRadarQuotes struct{}

func (forbiddenRadarQuotes) Realtime(context.Context, []string) ([]foundation.Quote, error) {
	panic("leader preview must not fetch quotes")
}

func TestProgressiveCachedKaipanlaFailureDoesNotRenewObservation(t *testing.T) {
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	attempt := now.Add(-time.Minute)
	source := fakeRadarSource{
		snapshot: foundation.ThemeSnapshot{ID: "old", TradeDate: "2026-09-17", FetchedAt: attempt, Themes: []foundation.ThemeSnapshotItem{{Code: "1", Name: "通信"}}},
		meta:     foundation.ThemeFetchMeta{LastAttemptAt: attempt, RefreshError: "prior failure", FromCache: true},
	}
	provider := NewRadarProvider(source, fakeRadarFallback{}, nil, RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{{Code: "i1", Name: "通信"}}}})
	var observations []foundation.SourceObservation
	provider.ProgressiveOverviews(context.Background(), func(value foundation.ThemeProgress) {
		observations = append(observations, value.Observations...)
	})
	for _, observation := range observations {
		if observation.SourceID == "duanxianxia" || observation.Meta.Source == legacyThemeSnapshotSource {
			t.Fatalf("cached snapshot was reported as a fresh observation: %+v", observation)
		}
	}
}

func TestProgressiveKaipanlaPartialRefreshKeepsSuccessAndFailure(t *testing.T) {
	now := time.Now()
	source := fakeRadarSource{
		snapshot: foundation.ThemeSnapshot{ID: "fresh", TradeDate: now.Format("2006-01-02"), FetchedAt: now, Themes: []foundation.ThemeSnapshotItem{{Code: "1", Name: "通信"}}},
		meta:     foundation.ThemeFetchMeta{Refreshed: true, Attempted: true, LastAttemptAt: now.Add(-time.Second), RefreshError: "涨停池刷新失败"},
	}
	provider := NewRadarProvider(source, fakeRadarFallback{}, nil, RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{{Code: "i1", Name: "通信"}}}})
	var observations []foundation.SourceObservation
	provider.ProgressiveOverviews(context.Background(), func(value foundation.ThemeProgress) {
		observations = append(observations, value.Observations...)
	})
	var succeeded, failed bool
	for _, item := range observations {
		succeeded = succeeded || item.Meta.Source == legacyThemeSnapshotSource && !item.Failed
		failed = failed || item.SourceID == "duanxianxia" && item.Failed
	}
	if !succeeded || !failed {
		t.Fatalf("partial refresh lost one outcome: %+v", observations)
	}
	for _, item := range observations {
		if item.Failed && item.AttemptAt.Before(now) {
			t.Fatalf("partial failure timestamp must not precede the successful snapshot: %+v", observations)
		}
	}
}

func TestProgressiveKaipanlaPoolSuccessWithThemeFailure(t *testing.T) {
	now := time.Now()
	source := fakeRadarSource{
		snapshot: foundation.ThemeSnapshot{ID: "old", TradeDate: now.Format("2006-01-02"), FetchedAt: now.Add(-time.Minute), Themes: []foundation.ThemeSnapshotItem{{Code: "1", Name: "通信"}}},
		meta:     foundation.ThemeFetchMeta{FromCache: true, Attempted: true, LastAttemptAt: now.Add(-time.Second), RefreshError: "题材刷新失败", PoolRefreshed: true, PoolFetchedAt: now},
	}
	provider := NewRadarProvider(source, fakeRadarFallback{}, nil, RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{{Code: "i1", Name: "通信"}}}})
	var success, failure bool
	provider.ProgressiveOverviews(context.Background(), func(value foundation.ThemeProgress) {
		for _, item := range value.Observations {
			success = success || item.Meta.Source == legacyThemeSnapshotSource && !item.Failed
			failure = failure || item.SourceID == "duanxianxia" && item.Failed
		}
	})
	if !success || !failure {
		t.Fatalf("pool success and theme failure must both be observed: success=%t failure=%t", success, failure)
	}
}

func TestProgressiveKaipanlaAttemptWithCachedSnapshotRecordsFailure(t *testing.T) {
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	source := fakeRadarSource{
		snapshot: foundation.ThemeSnapshot{ID: "old", TradeDate: "2026-09-17", FetchedAt: now.Add(-time.Minute), Themes: []foundation.ThemeSnapshotItem{{Code: "1", Name: "通信"}}},
		meta:     foundation.ThemeFetchMeta{FromCache: true, Attempted: true, LastAttemptAt: now, RefreshError: "本轮抓取失败"},
	}
	provider := NewRadarProvider(source, fakeRadarFallback{}, nil, RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{{Code: "i1", Name: "通信"}}}})
	var failures int
	provider.ProgressiveOverviews(context.Background(), func(value foundation.ThemeProgress) {
		for _, item := range value.Observations {
			if item.SourceID == "duanxianxia" && item.Failed {
				failures++
			}
		}
	})
	if failures != 1 {
		t.Fatalf("cached snapshot masked a real failed attempt: %d", failures)
	}
}

func TestLeaderPreviewDoesNotWaitForRemoteData(t *testing.T) {
	source := fakeRadarSource{snapshot: foundation.ThemeSnapshot{ID: "s", TradeDate: "2026-09-17", Themes: []foundation.ThemeSnapshotItem{{Code: "1", Name: "通信", Leaders: []foundation.ThemeLeader{{Symbol: "000001.SZ", Name: "测试", Rank: 1}}}}}}
	provider := NewRadarProvider(source, forbiddenRadarFallback{}, forbiddenRadarQuotes{}, RadarProviderConfig{})
	result, err := provider.BuildLeaders(context.Background(), "kpl:1", "s")
	if err != nil || len(result.Groups[0].Nodes[0].Stocks) != 1 {
		t.Fatalf("preview: %+v %v", result, err)
	}
	_, err = provider.BuildLeaders(context.Background(), "kpl:1", "expired")
	if !errors.Is(err, ErrSnapshotExpired) {
		t.Fatalf("expected typed expired snapshot: %v", err)
	}
}
