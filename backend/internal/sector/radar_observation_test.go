package sector

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

func TestPlainRadarObservationsPreservePartialFailureAndSkipCachedAttempts(t *testing.T) {
	now := time.Now()
	for _, cached := range []bool{false, true} {
		source := fakeRadarSource{
			snapshot: duanxianxia.Snapshot{ID: "snapshot", TradeDate: now.Format("2006-01-02"), FetchedAt: now, Themes: []duanxianxia.Theme{{Code: "1", Name: "通信"}}},
			meta:     duanxianxia.FetchMeta{Refreshed: !cached, Attempted: !cached, FromCache: cached, LastAttemptAt: now.Add(-time.Second), RefreshError: "涨停池刷新失败"},
		}
		provider := NewRadarProvider(source, fakeRadarFallback{}, nil, RadarProviderConfig{IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{{Code: "i1", Name: "通信"}}, meta: foundation.SourceMeta{Source: "tencent:industry-rank", FetchedAt: now}}})
		_, _, observations, err := provider.OverviewsWithObservations(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var success, failure, industry bool
		for _, event := range observations {
			success = success || event.Meta.Source == duanxianxia.SourceID
			failure = failure || event.SourceID == "duanxianxia" && event.Failed
			industry = industry || event.Meta.Source == "tencent:industry-rank"
		}
		if !industry || success != !cached || failure != !cached {
			t.Fatalf("cached=%t observations=%+v", cached, observations)
		}
	}
}

func TestPlainRadarKeepsKnownSourceOutcomesAfterDeadline(t *testing.T) {
	now := time.Now()
	ctx, cancel := context.WithDeadline(context.Background(), now.Add(-time.Second))
	defer cancel()
	provider := NewRadarProvider(fakeRadarSource{
		snapshot: duanxianxia.Snapshot{TradeDate: now.Format("2006-01-02"), FetchedAt: now},
		meta:     duanxianxia.FetchMeta{Refreshed: true, Attempted: true, LastAttemptAt: now, RefreshError: "涨停池刷新超时"},
	}, nil, nil, RadarProviderConfig{IndustryMomentum: fakeIndustryMomentumSource{
		meta: foundation.SourceMeta{Source: "tencent:industry-rank", FetchedAt: now},
	}})
	_, _, observations, _ := provider.OverviewsWithObservations(ctx)
	if len(observations) != 3 {
		t.Fatalf("deadline lost known supplier outcomes: %+v", observations)
	}
	if observations[0].Meta.Source != "tencent:industry-rank" || observations[1].Meta.Source != duanxianxia.SourceID || !observations[2].Failed {
		t.Fatalf("unexpected deadline observations: %+v", observations)
	}
}
