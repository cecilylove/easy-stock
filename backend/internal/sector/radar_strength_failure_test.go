package sector

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

func TestLeaderNamesDoNotEraseStrengthAfterSourceFailure(t *testing.T) {
	now := time.Now()
	current := now
	theme := duanxianxia.Theme{Code: "801001", Name: "芯片", Rank: 1, Leaders: []duanxianxia.Leader{{Symbol: "600001.SH", Name: "芯片股"}}}
	fallback := &fakeRadarStrengthFallback{stocks: map[string][]foundation.BoardStock{"semiconductor": {{Symbol: "600001.SH", Name: "芯片股", Price: 10, ChangePercent: 6, FiveDayChangePercent: 8}}}}
	provider := NewRadarProvider(fakeRadarSource{}, fallback, nil, RadarProviderConfig{Now: func() time.Time { return current }})
	first := provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})
	fallback.err = errors.New("eastmoney unavailable")
	current = now.Add(10 * time.Minute)
	second := provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})
	if !first[theme.Code].dailyValid || !first[theme.Code].fiveDayValid || first[theme.Code].daily == 0 || second[theme.Code] != first[theme.Code] {
		t.Fatalf("name-only leaders replaced usable cache: first=%v second=%v", first, second)
	}
	uncached := NewRadarProvider(fakeRadarSource{}, fallback, nil, RadarProviderConfig{})
	if got := uncached.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme}); len(got) != 0 {
		t.Fatalf("name-only leaders marked as valid zero strength: %v", got)
	}
}

func TestPartialStrengthRefreshPreservesMissingWindowAndValidZero(t *testing.T) {
	now := time.Now()
	current := now
	theme := duanxianxia.Theme{Code: "801001", Name: "芯片", Rank: 1, Leaders: []duanxianxia.Leader{{Symbol: "600001.SH", Name: "芯片股"}}}
	fallback := &fakeRadarStrengthFallback{stocks: map[string][]foundation.BoardStock{"semiconductor": {{Symbol: "600001.SH", Price: 10, ChangePercent: 6, FiveDayChangePercent: 8}}}}
	quotes := &fakeRadarStrengthQuotes{quotes: map[string]foundation.Quote{}}
	provider := NewRadarProvider(fakeRadarSource{}, fallback, quotes, RadarProviderConfig{Now: func() time.Time { return current }})
	first := provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})[theme.Code]
	fallback.err = errors.New("constituent pool unavailable")
	quotes.quotes["600001.SH"] = foundation.Quote{Symbol: "600001.SH", Price: 9.5, ChangePercent: -5}
	current = now.Add(10 * time.Minute)
	second := provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})[theme.Code]
	if !second.dailyValid || second.daily != 0 || !second.fiveDayValid || second.fiveDay != first.fiveDay {
		t.Fatalf("zero daily score or old five-day sample lost: first=%+v second=%+v", first, second)
	}
}

func TestMissingStrengthWindowIsExcludedFromFusionWeight(t *testing.T) {
	provider := NewRadarProvider(nil, nil, nil, RadarProviderConfig{})
	theme := duanxianxia.Theme{Code: "801001", Name: "芯片", Rank: 1}
	snapshot := duanxianxia.Snapshot{TradeDate: "2026-09-30", Themes: []duanxianxia.Theme{theme}}
	missing := provider.buildKaipanlaRadarOverviews(snapshot, snapshot.Themes, nil, map[string]themeStrengthScore{}, 0)[0]
	dailyOnly := provider.buildKaipanlaRadarOverviews(snapshot, snapshot.Themes, nil, map[string]themeStrengthScore{theme.Code: {daily: 0, dailyValid: true}}, 0)[0]
	if dailyOnly.KaipanlaFiveDayScore != missing.KaipanlaFiveDayScore || dailyOnly.KaipanlaDailyScore >= missing.KaipanlaDailyScore {
		t.Fatalf("missing five-day value counted as zero or valid daily zero ignored: missing=%+v dailyOnly=%+v", missing, dailyOnly)
	}
}

func TestCancelledStrengthRefreshDoesNotStartTenMinuteCooldown(t *testing.T) {
	provider := NewRadarProvider(nil, nil, nil, RadarProviderConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider.realtimeStrengthScores(ctx, []duanxianxia.Theme{{Code: "801001", Name: "芯片"}})
	if !provider.strengthAttemptAt.IsZero() {
		t.Fatalf("caller cancellation started source cooldown: %v", provider.strengthAttemptAt)
	}
}
