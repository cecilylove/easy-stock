package service

import (
	"easy-stock/backend/internal/foundation"
	"testing"
	"time"
)

func TestLimitUpProviderPrefersKaipanlaAndFillsFromEastMoney(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	date := time.Date(2026, 8, 7, 0, 0, 0, 0, location)
	primary := []foundation.LimitUpEvent{{
		Symbol: "603629.SH", Name: "利通电子", Date: date, Streak: 3,
		Concepts: []string{"算力租赁"}, BoardType: "回封",
		Meta: foundation.SourceMeta{Source: "duanxianxia:kaipanla-limit-up"},
	}}
	fallback := []foundation.LimitUpEvent{
		{Symbol: "603629.SH", Name: "利通电子", Date: date, Streak: 2, Price: 18.8, TurnoverRate: 9.6, Industry: "消费电子", Meta: foundation.SourceMeta{Source: "eastmoney:limit-up-pool"}},
		{Symbol: "600001.SH", Name: "东财补位", Date: date, Streak: 1, Meta: foundation.SourceMeta{Source: "eastmoney:limit-up-pool"}},
	}
	merged := mergeLimitUpEvents(primary, fallback, nil)
	if len(merged) != 2 {
		t.Fatalf("merged=%+v", merged)
	}
	if merged[0].Streak != 3 || merged[0].Price != 18.8 || merged[0].TurnoverRate != 9.6 || merged[0].Industry != "消费电子" || merged[0].Meta.Source != "duanxianxia:kaipanla-limit-up" {
		t.Fatalf("primary event was not preserved and hydrated: %+v", merged[0])
	}
	if len(merged[0].Concepts) != 1 || merged[0].Concepts[0] != "算力租赁" || merged[1].Symbol != "600001.SH" {
		t.Fatalf("unexpected merge order/concepts: %+v", merged)
	}
}

func TestApplyKaipanlaThemeLeadersAttributesMatchingTradingDay(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	previousDate := time.Date(2026, 8, 6, 0, 0, 0, 0, location)
	currentDate := time.Date(2026, 8, 7, 0, 0, 0, 0, location)
	events := []foundation.LimitUpEvent{
		{Symbol: "600892.SH", Name: "大晟文化", Date: previousDate, Concepts: []string{"创投", "影视"}},
		{Symbol: "600892.SH", Name: "大晟文化", Date: currentDate, Concepts: []string{"创投"}},
	}
	snapshots := []foundation.ThemeSnapshot{{
		TradeDate: "2026-08-06",
		Themes: []foundation.ThemeSnapshotItem{{
			Code: "803023", Name: "AI应用", Rank: 3, LeadersLoaded: true,
			Leaders: []foundation.ThemeLeader{{Rank: 2, Role: "龙二", Symbol: "600892.SH", Name: "大晟文化"}},
		}},
	}}

	result := applyKaipanlaThemeLeaders(events, snapshots)
	previous := result[0]
	if previous.PrimaryTheme != "AI应用" || previous.ThemeSource != ThemeLeaderSource || previous.ThemeRank != 3 || previous.ThemeLeaderRole != "龙二" {
		t.Fatalf("Kaipanla theme leader attribution missing: %+v", previous)
	}
	if len(previous.Concepts) == 0 || previous.Concepts[0] != "AI应用" {
		t.Fatalf("authoritative theme was not added to concepts: %+v", previous.Concepts)
	}
	if result[1].PrimaryTheme != "" {
		t.Fatalf("theme attribution leaked across trading days: %+v", result[1])
	}
}
