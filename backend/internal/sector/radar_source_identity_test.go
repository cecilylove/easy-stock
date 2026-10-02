package sector

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"strings"
	"testing"
	"time"
)

func TestReplacementThemeSourceSurvivesBusinessProjection(t *testing.T) {
	now := time.Now()
	snapshot := foundation.ThemeSnapshot{ID: "replacement-1", TradeDate: now.Format("2006-01-02"), FetchedAt: now, Meta: foundation.SourceMeta{Provider: "replacement", Source: "replacement:theme", SourceURL: "https://example.test/themes"}, Themes: []foundation.ThemeSnapshotItem{{Code: "native", Name: "通信", LeadersLoaded: true, Leaders: []foundation.ThemeLeader{{Symbol: "600001.SH", Name: "测试", Role: "龙一", Rank: 1}}}}}
	provider := NewRadarProvider(fakeRadarSource{snapshot: snapshot}, nil, nil, RadarProviderConfig{})
	result, err := provider.BuildSnapshot(context.Background(), "kpl:native", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	node := result.Groups[0].Nodes[0]
	if result.Meta.Source != "replacement:theme" || result.Meta.SourceURL != snapshot.Meta.SourceURL || node.BoardRef.Provider != "replacement" || node.Stocks[0].Meta.Source != "replacement:theme" {
		t.Fatalf("old attribution leaked: %+v", result)
	}
	observations := radarEventObservations(radarProgressEvent{step: "kaipanla", snapshot: snapshot, fetchMeta: foundation.ThemeFetchMeta{SourceID: "replacement", PoolSource: "replacement:limit-up", Refreshed: true, PoolRefreshed: true, PoolFetchedAt: now, Attempted: true, LastAttemptAt: now, RefreshError: "pool failed"}})
	if len(observations) != 3 || observations[0].Meta.Source != "replacement:theme" || observations[1].Meta.Source != "replacement:limit-up" || observations[2].SourceID != "replacement" {
		t.Fatalf("wrong health supplier: %+v", observations)
	}
}

func TestReplacementThemeNativeCodeDoesNotUseKaipanlaCrosswalk(t *testing.T) {
	now := time.Date(2026, 10, 2, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	bankID, _ := mappedFallbackThemeID("", "银行")
	var built []string
	bank := foundation.BoardStock{Symbol: "600000.SH", Name: "银行成分", Price: 10, ChangePercent: -3, FiveDayChangePercent: -4}
	fallback := fakeRadarFallback{builtThemeIDs: &built, sectorMaps: map[string]foundation.SectorMap{
		bankID: {Name: "银行", Groups: []foundation.SectorMapGroup{{Nodes: []foundation.SectorMapNode{{Stocks: []foundation.BoardStock{bank}}}}}},
	}}
	snapshot := foundation.ThemeSnapshot{ID: "replacement-bank", TradeDate: "2026-10-02", FetchedAt: now,
		Meta:   foundation.SourceMeta{Provider: "replacement", Source: "replacement:theme"},
		Themes: []foundation.ThemeSnapshotItem{{Code: "801001", Name: "银行", Rank: 1, Strength: 100}},
	}
	provider := NewRadarProvider(fakeRadarSource{snapshot: snapshot}, fallback, nil, RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: fakeIndustryMomentumSource{
		items: []foundation.MarketIndustryMomentum{{Code: "BK1036", Name: "半导体", Score: 80}},
		meta:  foundation.SourceMeta{Source: "test:industry", FetchedAt: now, TradeDate: snapshot.TradeDate},
	}})
	result, err := provider.BuildSnapshot(context.Background(), "kpl:801001", snapshot.ID)
	if err != nil || len(built) != 1 || built[0] != bankID || len(result.Groups) != 2 || result.Groups[1].Nodes[0].Stocks[0].Symbol != bank.Symbol {
		t.Fatalf("replacement native code selected wrong candidates: built=%v result=%+v err=%v", built, result, err)
	}
	for _, progressive := range []bool{false, true} {
		built = nil
		var items []foundation.ThemeOverview
		if progressive {
			provider.ProgressiveOverviews(context.Background(), func(progress foundation.ThemeProgress) { items = progress.Data })
		} else {
			items, _, err = provider.Overviews(context.Background())
			if err != nil {
				t.Fatal(err)
			}
		}
		if len(items) != 2 {
			t.Fatalf("lost source-exclusive themes (progressive=%v): %+v", progressive, items)
		}
		for _, item := range items {
			if strings.HasPrefix(item.Theme, "fusion:") {
				t.Fatalf("unrelated bank and semiconductor fused (progressive=%v): %+v", progressive, items)
			}
		}
		for _, id := range built {
			if id != bankID {
				t.Fatalf("wrong strength members (progressive=%v): %v", progressive, built)
			}
		}
	}
	// Explicit names may still use the shared topic crosswalk, even if their
	// supplier-native codes happen to identify a different Kaipanla topic.
	if score := radarIndustryMatchScore(foundation.ThemeOverview{Theme: "kpl:801001", Name: "通信", Source: "replacement:theme"}, foundation.ThemeOverview{Name: "通信技术"}); score < 70 {
		t.Fatalf("name crosswalk no longer works: score=%d", score)
	}
	if score := radarIndustryMatchScore(foundation.ThemeOverview{Theme: "kpl:801001", Name: "芯片"}, foundation.ThemeOverview{Name: "半导体"}); score < 70 {
		t.Fatalf("historical Kaipanla mapping changed: score=%d", score)
	}
}

func TestReplacementThemeStrengthDoesNotReuseHistoricalSameCodeCache(t *testing.T) {
	now := time.Now()
	bankID, _ := mappedFallbackThemeID("", "银行")
	fallback := &fakeRadarStrengthFallback{stocks: map[string][]foundation.BoardStock{
		"semiconductor": {{Symbol: "600001.SH", Price: 10, ChangePercent: 9, FiveDayChangePercent: 12}},
		bankID:          {{Symbol: "600000.SH", Price: 10, ChangePercent: -4, FiveDayChangePercent: -6}},
	}}
	provider := NewRadarProvider(nil, fallback, nil, RadarProviderConfig{Now: func() time.Time { return now }})
	themes := []foundation.ThemeSnapshotItem{{Code: "801001", Name: "银行"}}
	legacy := provider.realtimeStrengthScores(context.Background(), themes)["801001"]
	replacement := provider.realtimeStrengthScores(context.Background(), themes, "replacement:theme")["801001"]
	if fallback.calls != 2 || !replacement.dailyValid || !replacement.fiveDayValid || replacement.daily >= legacy.daily || replacement.fiveDay >= legacy.fiveDay {
		t.Fatalf("replacement inherited old native-code score: calls=%d legacy=%+v replacement=%+v", fallback.calls, legacy, replacement)
	}
	provider.realtimeStrengthScores(context.Background(), themes, "replacement:theme")
	if fallback.calls != 2 {
		t.Fatalf("same-source score no longer retains its refresh budget: calls=%d", fallback.calls)
	}
}
