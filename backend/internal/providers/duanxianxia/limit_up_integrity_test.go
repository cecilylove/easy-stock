package duanxianxia

import (
	"easy-stock/backend/internal/foundation"
	"testing"
	"time"
)

func TestPoolStrictNumericPresencePreservesZero(t *testing.T) {
	payload := []byte(`{"list":[["600001","测试",0,0,0,"09:30:00","主题","--",null,"--","回封",0],["600002","测试二","NaN",0,"--","09:31:00","主题","首板","Inf",0,"回封",null]]}`)
	events, err := parseLimitUpPool(payload, "2026-08-07", time.Now(), "fixture")
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if !events[0].Meta.FieldsKnown || events[0].Date.Format("2006-01-02") != events[0].Meta.TradeDate {
		t.Fatalf("strict date/meta missing: %+v", events[0])
	}
	for _, field := range []string{"change_percent", "open_count", "streak"} {
		if !foundation.LimitUpFieldAvailable(events[0], field) {
			t.Errorf("valid zero %s lost", field)
		}
	}
	for _, field := range []string{"price", "amount", "float_market_cap", "turnover_rate", "days", "count"} {
		if foundation.LimitUpFieldAvailable(events[0], field) {
			t.Errorf("missing %s falsely available", field)
		}
	}
	if events[0].Streak != 0 {
		t.Fatal("valid zero streak inferred as first board")
	}
	for _, field := range []string{"change_percent", "amount", "open_count"} {
		if foundation.LimitUpFieldAvailable(events[1], field) {
			t.Errorf("nonfinite/missing %s accepted", field)
		}
	}
	if events[1].Streak != 1 || !foundation.LimitUpFieldAvailable(events[1], "streak") || !foundation.LimitUpFieldAvailable(events[1], "float_market_cap") {
		t.Fatalf("documented streak derivation/zero lost: %+v", events[1])
	}
	events[0].Meta.AvailableFields[0] = "mutated"
	if events[1].Meta.AvailableFields[0] == "mutated" {
		t.Fatal("row metadata shares mask")
	}
}
func TestPoolStreakLabelNumbersKeepZeroPresenceAndRejectOverflow(t *testing.T) {
	for _, tc := range []struct {
		label string
		known bool
	}{{"0天0板", true}, {"999999999999999999999999天1板", false}, {"--", false}} {
		_, _, known := poolStreakNumbers(tc.label)
		if known != tc.known {
			t.Errorf("%q presence=%v", tc.label, known)
		}
	}
}
func TestPoolValidEmptyAndMalformedAreDistinct(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		wantErr       bool
	}{
		{"empty", `{"list":[]}`, false},
		{"null", `{"list":null}`, true},
		{"missing", `{}`, true},
		{"invalid row", `{"list":[["invalid"]]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events, err := parseLimitUpPool([]byte(tc.payload), "2026-08-07", time.Now(), "fixture")
			if (err != nil) != tc.wantErr || len(events) != 0 {
				t.Fatalf("events=%+v err=%v", events, err)
			}
		})
	}
}
