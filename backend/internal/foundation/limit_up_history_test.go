package foundation

import (
	"errors"
	"testing"
	"time"
)

func TestLimitUpHistoryEmptyCoverageAndClone(t *testing.T) {
	value := StampLimitUpHistory(LimitUpHistory{CoveredDates: []string{"2026-08-06", "2026-08-07"}, Meta: SourceMeta{Source: "test"}})
	clone := CloneLimitUpHistory(value)
	clone.CoveredDates[0] = "mutated"
	clone.Meta.CoveredDates[0] = "mutated"
	if value.CoveredDates[0] != "2026-08-06" || value.Meta.CoveredDates[0] != "2026-08-06" || len(value.Events) != 0 {
		t.Fatalf("empty coverage lost/mutated: %+v", value)
	}
}

func TestLimitUpHistoryCompatibilityNeverInventsEmptyDays(t *testing.T) {
	if value := LimitUpHistoryFromEvents(nil, nil); len(value.CoveredDates) != 0 {
		t.Fatal("empty events invented coverage")
	}
	cause := errors.New("missing day")
	partial := &LimitUpCoverageError{RequestedDates: []string{"2026-08-06", "2026-08-07"}, CoveredDates: []string{"2026-08-06"}, MissingDates: []string{"2026-08-07"}, Cause: cause}
	value := LimitUpHistoryFromEvents(nil, partial)
	if len(value.CoveredDates) != 1 || value.CoveredDates[0] != "2026-08-06" || !value.Meta.Partial {
		t.Fatalf("typed empty-day coverage lost: %+v", value)
	}
	event := LimitUpEvent{Date: time.Date(2026, 8, 7, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)), Meta: SourceMeta{CoveredDates: []string{"2026-08-06", "2026-08-07"}}}
	value = LimitUpHistoryFromEvents([]LimitUpEvent{event}, nil)
	if len(value.CoveredDates) != 2 {
		t.Fatalf("event metadata coverage lost: %+v", value)
	}
}
