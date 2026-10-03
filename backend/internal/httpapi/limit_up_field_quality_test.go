package httpapi

import (
	"easy-stock/backend/internal/foundation"
	"testing"
	"time"
)

func TestUnknownLimitUpStreakIsNotInventedFirstBoardOrEmotionInput(t *testing.T) {
	date := time.Date(2026, 9, 30, 0, 0, 0, 0, shanghaiLocation)
	event := foundation.LimitUpEvent{Symbol: "600001.SH", Name: "fixture", Date: date, Meta: foundation.SourceMeta{FieldsKnown: true, AvailableFields: []string{"amount", "open_count", "change_percent"}}}
	day := buildLimitUpDay("2026-09-30", []foundation.LimitUpEvent{event}, nil)
	if day.FirstBoardCount != 0 || len(day.Levels) != 1 || day.Levels[0].Level != 0 || day.Levels[0].Label != "板数未知" || !hasUnknownLadderStreak(day) {
		t.Fatalf("invented first board %+v", day)
	}
	if err := validateEmotionEventFields([]foundation.LimitUpEvent{event}); err == nil {
		t.Fatal("missing streak scored as known")
	}
	event.Streak = 1
	event.Meta.AvailableFields = append(event.Meta.AvailableFields, "streak")
	if err := validateEmotionEventFields([]foundation.LimitUpEvent{event}); err != nil {
		t.Fatalf("real zero amount/open count lost %v", err)
	}
	history := foundation.LimitUpHistory{Events: []foundation.LimitUpEvent{{Symbol: "600001.SH", Date: date, Meta: foundation.SourceMeta{FieldsKnown: true}}}, CoveredDates: []string{"2026-09-29", "2026-09-30"}}
	ladder, err := buildLimitUpLadderHistory(history, nil, date)
	if err != nil || ladder.ComparisonReady || len(ladder.Advance) != 0 || !ladder.Meta.Partial {
		t.Fatalf("unknown streak used in promotion %+v %v", ladder, err)
	}
}
